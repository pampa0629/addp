package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestCatalogFulfillmentChecksAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable Catalog PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS catalog CASCADE").Error; err != nil {
			t.Error(err)
		}
		var n int64
		if err := db.Raw("SELECT count(*) FROM pg_namespace WHERE nspname = 'catalog'").Scan(&n).Error; err != nil || n != 0 {
			t.Errorf("Catalog residual=%d err=%v", n, err)
		}
	})
	if err := db.Exec("DROP SCHEMA IF EXISTS catalog CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	newEntry := func(tenant int64) (models.Entry, models.SourceBinding) {
		t.Helper()
		name, description, established := "Fixture", "Business prose", true
		entry := models.Entry{ID: uuid.New(), TenantID: tenant, EntryType: models.EntryTypeDataItem,
			EntryStatus: models.EntryStatusActive, GovernanceStatus: models.GovernanceStatusCurated,
			BusinessName: &name, BusinessDescription: &description, Visibility: models.VisibilityInventory,
			BusinessResponsibilityEstablished: &established, Version: 1}
		if err := db.Create(&entry).Error; err != nil {
			t.Fatal(err)
		}
		binding := models.SourceBinding{ID: uuid.New(), TenantID: tenant, CatalogEntryID: entry.ID,
			SourceModule: models.SourceModuleMeta, SourceType: models.SourceTypeDataItem, SourceIdentity: uuid.NewString(),
			SourceStatus: models.SourceStatusActive, SourceVersion: "00000000000000000001", IsCurrent: true,
			BoundAt: time.Now(), ObservedAt: time.Now(), ObservedSnapshot: commonModels.JSONMap{"name": "fixture"}}
		if err := db.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
		return entry, binding
	}
	entry, binding := newEntry(7)
	other, _ := newEntry(7)
	responsibility := models.Responsibility{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID,
		Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 91,
		Status: models.ResponsibilityStatusActive, ObservedSnapshot: commonModels.JSONMap{}, VerifiedAt: time.Now()}
	if err := db.Create(&responsibility).Error; err != nil {
		t.Fatal(err)
	}
	check := models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID,
		RequestBinding: json.RawMessage(`{"decision_id":"` + uuid.NewString() + `","requirement_version":3,"recipient_id":9007199254740993}`)}
	// Committed before any sending. A later process/transaction rollback does
	// not release this persistent guard as it would release an ordinary lock.
	if err := db.Transaction(func(tx *gorm.DB) error { return tx.Create(&check).Error }); err != nil {
		t.Fatal(err)
	}
	legacyResolved := check
	legacyResolved.RequestID = uuid.New()
	if err := db.Create(&legacyResolved).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&legacyResolved).Update("resolved_at", gorm.Expr("clock_timestamp()")).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&legacyResolved, "request_id=?", legacyResolved.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	// Reconstruct the prior schema, then upgrade without assigning a terminal
	// issuance marker to either old pending or old resolved history.
	if err := db.Exec("ALTER TABLE catalog.fulfillment_checks DROP COLUMN grant_reconciled_at CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("repeat migration erased/blocked pending history: %v", err)
	}
	var restored models.FulfillmentCheck
	if err := db.First(&restored, "request_id = ?", check.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	var migratedResolved models.FulfillmentCheck
	if err := db.First(&migratedResolved, "request_id=?", legacyResolved.RequestID).Error; err != nil || restored.GrantReconciledAt != nil || migratedResolved.GrantReconciledAt != nil || migratedResolved.ResolvedAt == nil || !migratedResolved.ResolvedAt.Equal(*legacyResolved.ResolvedAt) {
		t.Fatalf("upgrade invented completion or changed reconciliation: %+v %v", migratedResolved, err)
	}
	var exact struct {
		RecipientID int64 `json:"recipient_id"`
	}
	if err := json.Unmarshal(restored.RequestBinding, &exact); err != nil || exact.RecipientID != 9007199254740993 {
		t.Fatalf("request binding lost exact identity: %s err=%v", restored.RequestBinding, err)
	}
	aborted := errors.New("simulated interrupted local transaction")
	if err := db.Transaction(func(tx *gorm.DB) error {
		var locked models.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", entry.ID).Error; err != nil {
			return err
		}
		return aborted
	}); !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	var pending int64
	if err := db.Model(&models.FulfillmentCheck{}).Where("request_id = ? AND resolved_at IS NULL", check.RequestID).Count(&pending).Error; err != nil || pending != 1 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	for _, statement := range []struct {
		name, sql string
		id        uuid.UUID
	}{
		{"deprecate", "UPDATE catalog.entries SET governance_status = 'deprecated' WHERE id = ?", entry.ID},
		{"withdraw curation", "UPDATE catalog.entries SET governance_status = 'discovered' WHERE id = ?", entry.ID},
		{"source missing", "UPDATE catalog.source_bindings SET source_status = 'missing' WHERE id = ?", binding.ID},
		{"source rebind", "UPDATE catalog.source_bindings SET is_current = FALSE WHERE id = ?", binding.ID},
		{"source observation", "UPDATE catalog.source_bindings SET source_version = '00000000000000000002' WHERE id = ?", binding.ID},
		{"responsibility transfer", "UPDATE catalog.responsibilities SET subject_id = 92 WHERE id = ?", responsibility.ID},
		{"background invalidation", "UPDATE catalog.responsibilities SET status = 'needs_transfer' WHERE id = ?", responsibility.ID},
		{"responsibility replacement", "DELETE FROM catalog.responsibilities WHERE id = ?", responsibility.ID},
	} {
		t.Run(statement.name, func(t *testing.T) {
			err := db.Exec(statement.sql, statement.id).Error
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.ConstraintName != "catalog_fulfillment_unresolved" {
				t.Fatalf("guard result=%v", err)
			}
		})
	}
	if err := db.Model(&entry).Update("business_description", "Changed description").Error; err != nil {
		t.Fatalf("prose was blocked: %v", err)
	}
	if err := db.Model(&other).Update("governance_status", models.GovernanceStatusDeprecated).Error; err != nil {
		t.Fatalf("unrelated entry blocked: %v", err)
	}
	if err := db.Model(&responsibility).Update("verified_at", time.Now()).Error; err != nil {
		t.Fatalf("unchanged responsibility refresh blocked: %v", err)
	}
	for _, statement := range []string{
		"UPDATE catalog.fulfillment_checks SET request_binding = '{\"changed\":true}'::jsonb WHERE request_id = ?",
		"DELETE FROM catalog.fulfillment_checks WHERE request_id = ?",
	} {
		if err := db.Exec(statement, check.RequestID).Error; err == nil {
			t.Fatal("pending binding/history altered")
		}
	}
	if err := db.Exec("TRUNCATE catalog.fulfillment_checks").Error; err == nil {
		t.Fatal("coordination history truncated")
	}
	// Local resolution is a fixture here, NOT proof of a real System receipt.
	if err := db.Model(&models.FulfillmentCheck{}).Where("request_id = ?", check.RequestID).Update("grant_reconciled_at", gorm.Expr("clock_timestamp()")).Error; err == nil {
		t.Fatal("issuance marker preceded acceptance reconciliation")
	}
	if err := db.Model(&models.FulfillmentCheck{}).Where("request_id = ?", check.RequestID).Update("resolved_at", gorm.Expr("clock_timestamp()")).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entry).Update("governance_status", models.GovernanceStatusDeprecated).Error; err != nil {
		t.Fatalf("resolved guard not released: %v", err)
	}
	if err := db.Model(&models.FulfillmentCheck{}).Where("request_id = ?", check.RequestID).Update("grant_reconciled_at", gorm.Expr("resolved_at - INTERVAL '1 microsecond'")).Error; err == nil {
		t.Fatal("issuance marker backdated before reconciliation")
	}
	if err := db.Model(&models.FulfillmentCheck{}).Where("request_id = ?", check.RequestID).Update("grant_reconciled_at", gorm.Expr("clock_timestamp()")).Error; err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{nil, gorm.Expr("clock_timestamp()"), gorm.Expr("'infinity'::timestamptz")} {
		if err := db.Model(&models.FulfillmentCheck{}).Where("request_id = ?", check.RequestID).Update("grant_reconciled_at", value).Error; err == nil {
			t.Fatal("issuance completion rewritten or reopened")
		}
	}
	if err := db.Model(&models.FulfillmentCheck{}).Where("request_id = ?", check.RequestID).Update("resolved_at", nil).Error; err == nil {
		t.Fatal("resolved coordination reopened")
	}
	late := models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, RequestBinding: check.RequestBinding}
	if err := db.Create(&late).Error; err == nil {
		t.Fatal("deprecated entry prepared a new request")
	}
	crossTenant := models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 8, CatalogEntryID: other.ID, RequestBinding: check.RequestBinding}
	if err := db.Create(&crossTenant).Error; err == nil {
		t.Fatal("cross-tenant coordination accepted")
	}
	t.Run("responsibility is independent of completed curation", func(t *testing.T) {
		candidate, _ := newEntry(7)
		if err := db.Model(&candidate).Update("governance_status", models.GovernanceStatusDiscovered).Error; err != nil {
			t.Fatal(err)
		}
		owner := models.Responsibility{ID: uuid.New(), TenantID: 7, CatalogEntryID: candidate.ID,
			Role: models.ResponsibilityRoleBusinessOwner, SubjectType: models.ResponsibilitySubjectUser, SubjectID: 91,
			Status: models.ResponsibilityStatusActive, ObservedSnapshot: commonModels.JSONMap{}, VerifiedAt: time.Now()}
		if err := db.Create(&owner).Error; err != nil {
			t.Fatal(err)
		}
		row := models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 7, CatalogEntryID: candidate.ID, RequestBinding: check.RequestBinding}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("completed curation was imposed as a new qualification: %v", err)
		}
	})
	t.Run("prepare and basis change serialize on the entry", func(t *testing.T) {
		candidate, _ := newEntry(7)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ready, release := make(chan struct{}), make(chan struct{})
		prepared := make(chan error, 1)
		go func() {
			prepared <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				row := models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 7, CatalogEntryID: candidate.ID,
					RequestBinding: check.RequestBinding}
				if err := tx.Create(&row).Error; err != nil {
					close(ready)
					return err
				}
				close(ready)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		<-ready
		changed := make(chan error, 1)
		go func() {
			changed <- db.WithContext(ctx).Model(&candidate).Update("governance_status", models.GovernanceStatusDeprecated).Error
		}()
		close(release)
		if err := <-prepared; err != nil {
			t.Fatal(err)
		}
		var pgErr *pgconn.PgError
		if err := <-changed; !errors.As(err, &pgErr) || pgErr.ConstraintName != "catalog_fulfillment_unresolved" {
			t.Fatalf("concurrent basis change escaped pending guard: %v", err)
		}
		// Opposite order: a committed deprecation prevents a later preparation.
		first, _ := newEntry(7)
		if err := db.Model(&first).Update("governance_status", models.GovernanceStatusDeprecated).Error; err != nil {
			t.Fatal(err)
		}
		row := models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 7, CatalogEntryID: first.ID, RequestBinding: check.RequestBinding}
		if err := db.Create(&row).Error; err == nil {
			t.Fatal("preparation escaped an earlier basis change")
		}
	})
}
