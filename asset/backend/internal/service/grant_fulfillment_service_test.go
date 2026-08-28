package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/asset/internal/models"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeGrantCatalog struct {
	resolution commonClient.CatalogReferenceResolution
}

func (catalog fakeGrantCatalog) ResolveReferences(_ context.Context, _ uint, ids []uuid.UUID) ([]commonClient.CatalogReferenceResolution, error) {
	resolution := catalog.resolution
	resolution.ID = ids[0]
	return []commonClient.CatalogReferenceResolution{resolution}, nil
}

type fakeGrantOwner struct {
	fulfilled int
	revoked   int
	failure   error
}

type recordNotFoundCaptureLogger struct {
	recordNotFoundCount int
}

func (capture *recordNotFoundCaptureLogger) LogMode(logger.LogLevel) logger.Interface      { return capture }
func (capture *recordNotFoundCaptureLogger) Info(context.Context, string, ...interface{})  {}
func (capture *recordNotFoundCaptureLogger) Warn(context.Context, string, ...interface{})  {}
func (capture *recordNotFoundCaptureLogger) Error(context.Context, string, ...interface{}) {}
func (capture *recordNotFoundCaptureLogger) Trace(_ context.Context, _ time.Time, _ func() (string, int64), err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		capture.recordNotFoundCount++
	}
}

func (owner *fakeGrantOwner) FulfillAssetGrant(_ context.Context, _ uint, _ int64, _ commonClient.WorkbenchAssetResourceGrantRequest) (*commonClient.WorkbenchAssetResourceGrantResponse, error) {
	owner.fulfilled++
	return &commonClient.WorkbenchAssetResourceGrantResponse{}, owner.failure
}

func (owner *fakeGrantOwner) RevokeAssetGrant(_ context.Context, _ uint, _ int64, _ commonClient.WorkbenchAssetResourceGrantRequest) (*commonClient.WorkbenchAssetResourceGrantResponse, error) {
	owner.revoked++
	return &commonClient.WorkbenchAssetResourceGrantResponse{}, owner.failure
}

func TestGrantFulfillmentReconcilesOwnerRuleAndRevocation(t *testing.T) {
	db := openGrantFulfillmentTestDB(t)
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	authorization, applicationID := seedPendingApplicationAuthorization(t, db, now)
	resourceID := uuid.New()
	owner := &fakeGrantOwner{}
	reconciler := &GrantFulfillmentService{
		db: db,
		catalog: fakeGrantCatalog{resolution: commonClient.CatalogReferenceResolution{
			ID: uuid.New(), Found: true, Selectable: true, Publishable: true,
			EntryType: "data_application", SourceModule: "workbench", SourceType: "data_application", SourceIdentity: resourceID.String(),
		}},
		owner: owner,
		now:   func() time.Time { return now },
	}

	processed, err := reconciler.ReconcileOnce(context.Background())
	if err != nil || !processed || owner.fulfilled != 1 {
		t.Fatalf("fulfill processed=%v owner=%#v err=%v", processed, owner, err)
	}
	if err := db.First(&authorization, authorization.ID).Error; err != nil {
		t.Fatal(err)
	}
	if authorization.Status != models.AuthorizationStatusEffective || authorization.TargetResourceID != resourceID.String() || authorization.FulfilledAt == nil {
		t.Fatalf("effective authorization = %#v", authorization)
	}

	if err := NewApplicationService(db, NewAuthorizationService(db)).RevokeByApplication(7, 22, applicationID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := db.Model(&models.Authorization{}).Where("id = ?", authorization.ID).Update("next_attempt_at", now).Error; err != nil {
		t.Fatal(err)
	}
	processed, err = reconciler.ReconcileOnce(context.Background())
	if err != nil || !processed || owner.revoked != 1 {
		t.Fatalf("revoke processed=%v owner=%#v err=%v", processed, owner, err)
	}
	if err := db.First(&authorization, authorization.ID).Error; err != nil {
		t.Fatal(err)
	}
	if authorization.Status != models.AuthorizationStatusRevoked || authorization.RevokedAt == nil {
		t.Fatalf("revoked authorization = %#v", authorization)
	}
}

func TestGrantFulfillmentRetainsPendingStateForRetry(t *testing.T) {
	db := openGrantFulfillmentTestDB(t)
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	authorization, _ := seedPendingApplicationAuthorization(t, db, now)
	resourceID := uuid.New()
	owner := &fakeGrantOwner{failure: errors.New("owner unavailable")}
	reconciler := &GrantFulfillmentService{
		db: db,
		catalog: fakeGrantCatalog{resolution: commonClient.CatalogReferenceResolution{
			ID: uuid.New(), Found: true, Selectable: true, Publishable: true,
			EntryType: "data_application", SourceModule: "workbench", SourceType: "data_application", SourceIdentity: resourceID.String(),
		}},
		owner: owner, now: func() time.Time { return now },
	}
	if processed, err := reconciler.ReconcileOnce(context.Background()); !processed || err == nil {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	if err := db.First(&authorization, authorization.ID).Error; err != nil {
		t.Fatal(err)
	}
	if authorization.Status != models.AuthorizationStatusPending || authorization.FulfillmentAttempt != 1 || authorization.FulfillmentLastError != "owner unavailable" || authorization.NextAttemptAt == nil {
		t.Fatalf("retry authorization = %#v", authorization)
	}
}

func TestGrantFulfillmentEmptyQueueDoesNotEmitRecordNotFound(t *testing.T) {
	db := openGrantFulfillmentTestDB(t)
	capture := &recordNotFoundCaptureLogger{}
	db = db.Session(&gorm.Session{Logger: capture})
	reconciler := &GrantFulfillmentService{db: db, now: func() time.Time {
		return time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	}}

	processed, err := reconciler.ReconcileOnce(context.Background())
	if err != nil || processed {
		t.Fatalf("empty queue processed=%v err=%v", processed, err)
	}
	if capture.recordNotFoundCount != 0 {
		t.Fatalf("empty queue emitted %d record-not-found traces", capture.recordNotFoundCount)
	}
}

func seedPendingApplicationAuthorization(t *testing.T, db *gorm.DB, now time.Time) (models.Authorization, int64) {
	t.Helper()
	typeDefinition := models.TypeDefinition{TenantID: 0, Name: "Data application", Code: "application", Enabled: true}
	if err := db.Create(&typeDefinition).Error; err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{TenantID: 7, Name: "Orders app", TypeID: typeDefinition.ID, Status: "published", OwnerID: 11, CreatedBy: 11}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	component := models.AssetComponent{TenantID: 7, AssetID: asset.ID, CatalogEntryID: uuid.New(), Role: models.AssetComponentRolePrimary}
	if err := db.Create(&component).Error; err != nil {
		t.Fatal(err)
	}
	application := models.Application{TenantID: 7, AssetID: asset.ID, ApplicantID: 91, Status: "approved"}
	if err := db.Create(&application).Error; err != nil {
		t.Fatal(err)
	}
	expiresAt := now.Add(time.Hour)
	authorization := models.Authorization{
		TenantID: 7, AssetID: asset.ID, ApplicationID: &application.ID, UserID: 91,
		Status: models.AuthorizationStatusPending, ExpiresAt: &expiresAt, NextAttemptAt: &now,
	}
	if err := db.Create(&authorization).Error; err != nil {
		t.Fatal(err)
	}
	return authorization, application.ID
}

func openGrantFulfillmentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS asset").Error; err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE asset.type_definitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, code TEXT NOT NULL,
			icon_url TEXT, description TEXT, enabled BOOLEAN, sort_order INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.assets (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT,
			type_id INTEGER NOT NULL, category_id INTEGER, tags TEXT, status TEXT, owner_id INTEGER, version INTEGER,
			published_at DATETIME, created_by INTEGER, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.asset_components (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			catalog_entry_id TEXT NOT NULL, role TEXT NOT NULL, sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.applications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			applicant_id INTEGER NOT NULL, reason TEXT, duration_day INTEGER, status TEXT, reviewer_id INTEGER,
			review_note TEXT, reviewed_at DATETIME, expires_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.authorizations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			application_id INTEGER NOT NULL, user_id INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
			target_module TEXT NOT NULL DEFAULT '', target_resource_type TEXT NOT NULL DEFAULT '', target_resource_id TEXT NOT NULL DEFAULT '',
			expires_at DATETIME, fulfillment_attempt INTEGER NOT NULL DEFAULT 0, fulfillment_last_error TEXT NOT NULL DEFAULT '',
			next_attempt_at DATETIME, fulfilled_at DATETIME, revoked_at DATETIME, revoked_by INTEGER,
			created_at DATETIME, updated_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
