package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresFulfillmentProtectsCurationAndSyncCheckpoint(t *testing.T) {
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable Catalog PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	entry, component := createEditableCatalogEntry(t, tx, 7)
	service := NewEntryService(tx, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	curated := curateCompleteEntry(t, service, entry, component)
	check := models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID,
		RequestBinding: json.RawMessage(`{"decision_id":"` + uuid.NewString() + `"}`)}
	if err := tx.Create(&check).Error; err != nil {
		t.Fatal(err)
	}
	name, description := "Better name", "Better business explanation"
	inputs := make([]ResponsibilityInput, 0, len(curated.Responsibilities))
	for _, row := range curated.Responsibilities {
		inputs = append(inputs, ResponsibilityInput{Role: row.Role, SubjectType: row.SubjectType, SubjectID: row.SubjectID})
	}
	updated, err := service.Update(context.Background(), 7, entry.ID, UpdateEntryInput{
		Version: curated.Version, BusinessName: &name, BusinessDescription: &description,
		GovernanceStatus: models.GovernanceStatusCurated, Visibility: models.VisibilityTenant,
		Domains: []DomainLinkInput{{ID: 10, Role: models.SemanticRolePrimary}}, GlossaryIDs: []int64{20},
		Responsibilities: inputs,
	}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("ordinary curation prose blocked: %v", err)
	}
	for _, before := range curated.Responsibilities {
		found := false
		for _, after := range updated.Responsibilities {
			found = found || before.ID == after.ID
		}
		if !found {
			t.Fatal("unchanged responsibility identity replaced")
		}
	}
	checkpoint := models.SourceCheckpoint{TenantID: 7, SourceModule: models.SourceModuleMeta,
		FeedName: metaDataItemFeedName, Cursor: "before", UpdatedAt: time.Now()}
	if err := tx.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	source := &fakeMetaChangeSource{responses: map[string]*commonClient.MetaDataItemChangesResponse{
		"before": changeBatch("after", false, commonClient.MetaDataItemChange{
			ChangeID: "pending-source-change", Operation: "missing", SourceIdentity: curated.Source.SourceIdentity,
			SourceVersion: "00000000000000000002", ObservedAt: time.Now(),
			Snapshot: map[string]interface{}{"name": "orders"},
		}),
	}}
	syncService := NewSourceSyncService(tx, source)
	err = syncService.SyncTenant(context.Background(), 7)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "catalog_fulfillment_unresolved" {
		t.Fatalf("sync escaped pending guard: %v", err)
	}
	if err := tx.First(&checkpoint, "id = ?", checkpoint.ID).Error; err != nil || checkpoint.Cursor != "before" {
		t.Fatalf("rejected sync consumed checkpoint: %+v err=%v", checkpoint, err)
	}
	var binding models.SourceBinding
	if err := tx.First(&binding, "id = ?", curated.Source.ID).Error; err != nil || binding.SourceStatus != models.SourceStatusActive || binding.SourceVersion != "00000000000000000001" {
		t.Fatalf("rejected sync changed source: %+v err=%v", binding, err)
	}
	// Synthetic reconciliation fixture only; production must verify System's
	// exact durable outcome before marking this coordination as resolved.
	if err := tx.Model(&check).Update("resolved_at", gorm.Expr("clock_timestamp()")).Error; err != nil {
		t.Fatal(err)
	}
	if err := syncService.SyncTenant(context.Background(), 7); err != nil {
		t.Fatalf("reconciled sync did not recover: %v", err)
	}
	if err := tx.First(&checkpoint, "id = ?", checkpoint.ID).Error; err != nil || checkpoint.Cursor != "after" {
		t.Fatalf("recovered checkpoint: %+v err=%v", checkpoint, err)
	}
}
