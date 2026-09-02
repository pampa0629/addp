package repository

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/addp/workbench/internal/models"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestWorkbenchDataApplicationRepositoryAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("WORKBENCH_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set WORKBENCH_POSTGRES_TEST_DSN to addp_test or an isolated disposable database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP SCHEMA IF EXISTS workbench CASCADE").Error; err != nil {
		t.Fatalf("reset workbench schema: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA workbench").Error; err != nil {
		t.Fatalf("prepare workbench schema: %v", err)
	}
	if err := db.Exec("CREATE TABLE workbench.views (id uuid PRIMARY KEY)").Error; err != nil {
		t.Fatalf("prepare retired workbench view table: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	var retiredViewTable *string
	if err := db.Raw("SELECT to_regclass('workbench.views')::text").Scan(&retiredViewTable).Error; err != nil {
		t.Fatalf("inspect retired workbench view table: %v", err)
	}
	if retiredViewTable != nil {
		t.Fatalf("retired workbench view table still exists: %q", *retiredViewTable)
	}
	applications := NewDataApplicationRepository(db)
	application := postgresDataApplication(7, 11)
	if err := applications.Create(application); err != nil {
		t.Fatalf("Create(data application) error = %v", err)
	}
	if _, err := applications.Get(7, 12, application.ID); !errors.Is(err, ErrDataApplicationNotFound) {
		t.Fatalf("cross-owner data application Get() error = %v", err)
	}
	revision, err := applications.Publish(7, 11, application.ID, 1, 11)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if revision.RevisionNumber != 1 || revision.Name != application.Name {
		t.Fatalf("published revision = %#v", revision)
	}
	var catalogChanges []models.CatalogResourceChangeRow
	if err := db.Where("tenant_id = ? AND source_identity = ?", 7, application.ID).Order("id ASC").Find(&catalogChanges).Error; err != nil {
		t.Fatalf("list published application catalog changes: %v", err)
	}
	if len(catalogChanges) != 1 || catalogChanges[0].Operation != "upsert" || catalogChanges[0].Snapshot["name"] != "Postgres application" || catalogChanges[0].Snapshot["publication_status"] != models.PublicationStatusPublished {
		t.Fatalf("published application catalog changes = %#v", catalogChanges)
	}
	application.Name = "Edited draft"
	application.DraftContentHash = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := applications.Update(application, 2); err != nil {
		t.Fatalf("Update(data application) error = %v", err)
	}
	runtime, err := applications.GetRuntime(7, 11, application.ID)
	if err != nil {
		t.Fatalf("GetRuntime() error = %v", err)
	}
	if runtime.Name != "Postgres application" || runtime.ContentHash == application.DraftContentHash {
		t.Fatalf("runtime revision changed with draft = %#v", runtime)
	}
	if err := db.Where("tenant_id = ? AND source_identity = ?", 7, application.ID).Order("id ASC").Find(&catalogChanges).Error; err != nil {
		t.Fatalf("list draft-edited application catalog changes: %v", err)
	}
	if len(catalogChanges) != 1 {
		t.Fatalf("draft edit emitted catalog changes = %#v", catalogChanges)
	}
	if err := applications.Delete(7, 11, application.ID, 3); !errors.Is(err, ErrDataApplicationAlreadyPublished) {
		t.Fatalf("published application Delete() error = %v", err)
	}
	if err := applications.Offline(7, 11, application.ID, 3); err != nil {
		t.Fatalf("Offline() error = %v", err)
	}
	if err := db.Where("tenant_id = ? AND source_identity = ?", 7, application.ID).Order("id ASC").Find(&catalogChanges).Error; err != nil {
		t.Fatalf("list offline application catalog changes: %v", err)
	}
	if len(catalogChanges) != 2 || catalogChanges[1].Snapshot["name"] != "Postgres application" || catalogChanges[1].Snapshot["publication_status"] != models.PublicationStatusOffline {
		t.Fatalf("offline application catalog changes = %#v", catalogChanges)
	}
	if _, err := applications.GetRuntime(7, 11, application.ID); !errors.Is(err, ErrDataApplicationNotPublished) {
		t.Fatalf("offline GetRuntime() error = %v", err)
	}
	if err := applications.Offline(7, 11, application.ID, 4); !errors.Is(err, ErrDataApplicationNotPublished) {
		t.Fatalf("repeated Offline() error = %v", err)
	}

	accessRules := NewResourceAccessRuleRepository(db)
	rule := models.ResourceAccessRule{
		TenantID: 7, ResourceType: models.ResourceTypeDataApplication, ResourceID: application.ID,
		SubjectType: models.ResourceAccessSubjectUser, SubjectID: 91,
		Permission: models.DataApplicationExecutePermission, Effect: models.ResourceAccessEffectAllow,
		SourceModule: models.ResourceAccessSourceAsset, SourceIdentity: "73",
	}
	createdRule, err := accessRules.FulfillAssetGrant(rule)
	if err != nil {
		t.Fatalf("FulfillAssetGrant() error = %v", err)
	}
	idempotentRule, err := accessRules.FulfillAssetGrant(rule)
	if err != nil || idempotentRule.ID != createdRule.ID {
		t.Fatalf("idempotent FulfillAssetGrant() = %#v, %v", idempotentRule, err)
	}
	concurrentRule := rule
	concurrentRule.SourceIdentity = "74"
	concurrentRule.SubjectID = 93
	const concurrentRequests = 12
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(concurrentRequests + 2)
	start := make(chan struct{})
	readyToCreate := make(chan struct{}, concurrentRequests)
	releaseCreate := make(chan struct{})
	results := make(chan *models.ResourceAccessRule, concurrentRequests)
	resultErrors := make(chan error, concurrentRequests)
	const concurrentCreateBarrier = "test:resource-access-rule-concurrent-create"
	if err := db.Callback().Create().Before("gorm:create").Register(concurrentCreateBarrier, func(tx *gorm.DB) {
		ruleToCreate, ok := tx.Statement.Dest.(*models.ResourceAccessRule)
		if !ok || ruleToCreate.SourceIdentity != concurrentRule.SourceIdentity {
			return
		}
		readyToCreate <- struct{}{}
		<-releaseCreate
	}); err != nil {
		t.Fatalf("register concurrent create barrier: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(concurrentCreateBarrier)
	})
	var wait sync.WaitGroup
	for range concurrentRequests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, fulfillErr := accessRules.FulfillAssetGrant(concurrentRule)
			results <- result
			resultErrors <- fulfillErr
		}()
	}
	close(start)
	for range concurrentRequests {
		<-readyToCreate
	}
	close(releaseCreate)
	wait.Wait()
	close(results)
	close(resultErrors)
	for fulfillErr := range resultErrors {
		if fulfillErr != nil {
			t.Fatalf("concurrent FulfillAssetGrant() error = %v", fulfillErr)
		}
	}
	var concurrentRuleID string
	for result := range results {
		if result == nil || result.ID == "" {
			t.Fatalf("concurrent FulfillAssetGrant() result = %#v", result)
		}
		if concurrentRuleID == "" {
			concurrentRuleID = result.ID
		} else if result.ID != concurrentRuleID {
			t.Fatalf("concurrent FulfillAssetGrant() IDs = %q and %q", concurrentRuleID, result.ID)
		}
	}
	allowed, err := accessRules.CanExecuteDataApplication(7, 91, application.ID, time.Now().UTC())
	if err != nil || !allowed {
		t.Fatalf("CanExecuteDataApplication() = %v, %v", allowed, err)
	}
	conflict := rule
	conflict.SubjectID = 92
	if _, err := accessRules.FulfillAssetGrant(conflict); !errors.Is(err, ErrResourceGrantConflict) {
		t.Fatalf("conflicting FulfillAssetGrant() error = %v", err)
	}
	if _, err := accessRules.RevokeAssetGrant(rule, time.Now().UTC()); err != nil {
		t.Fatalf("RevokeAssetGrant() error = %v", err)
	}
	allowed, err = accessRules.CanExecuteDataApplication(7, 91, application.ID, time.Now().UTC())
	if err != nil || allowed {
		t.Fatalf("revoked CanExecuteDataApplication() = %v, %v", allowed, err)
	}
}

func postgresDataApplication(tenantID, ownerUserID int64) *models.DataApplication {
	return &models.DataApplication{
		ID: uuid.NewString(), TenantID: tenantID, OwnerUserID: ownerUserID,
		Name: "Postgres application", Description: "",
		DraftSnapshot:     datatypes.JSON(`{"schema_version":"addp.workbench_data_application/v1","page":{"id":"69e435ef-5f56-456e-b495-791b42e74247","title":"Page","placements":[]},"components":[],"parameters":[],"parameter_bindings":[]}`),
		DraftContentHash:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PublicationStatus: models.PublicationStatusUnpublished, CurrentRevisionHash: "", Version: 1,
	}
}
