package repository

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/model/internal/migration"
	"github.com/addp/model/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openStandardReferenceGuardPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run model migrations: %v", err)
	}
	return db
}

func waitForStandardReferenceGuardLock(t *testing.T, db *gorm.DB) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		err := db.Raw(`SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%standard_reference_guards%'`).Scan(&count).Error
		if err != nil {
			t.Fatalf("inspect PostgreSQL lock wait: %v", err)
		}
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for standard reference guard row lock")
}

func TestPostgresStandardReferenceGuardSerializesWriterBeforeFreeze(t *testing.T) {
	db := openStandardReferenceGuardPostgres(t)
	tenantID := time.Now().UnixNano()
	resourceID := tenantID
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM model.entities WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM model.standard_reference_guards WHERE tenant_id = ?", tenantID).Error
	})

	writer := db.Begin()
	if writer.Error != nil {
		t.Fatalf("begin writer transaction: %v", writer.Error)
	}
	if err := LockStandardReferences(writer, tenantID, models.StandardReference{ResourceType: models.StandardResourceDomain, ResourceID: resourceID}); err != nil {
		_ = writer.Rollback().Error
		t.Fatalf("lock writer guard: %v", err)
	}

	result := make(chan struct {
		impact *models.StandardReferenceGuardResponse
		err    error
	}, 1)
	go func() {
		impact, err := NewStandardReferenceGuardRepository(db).SetState(tenantID, models.StandardResourceDomain, resourceID, models.StandardReferenceGuardFrozen)
		result <- struct {
			impact *models.StandardReferenceGuardResponse
			err    error
		}{impact: impact, err: err}
	}()
	waitForStandardReferenceGuardLock(t, db)

	entity := models.Entity{
		TenantID: tenantID, DomainID: &resourceID, Name: "Guard writer",
		Code: fmt.Sprintf("guard_%d", tenantID), Status: "draft", CreatedBy: 1,
	}
	if err := writer.Create(&entity).Error; err != nil {
		_ = writer.Rollback().Error
		t.Fatalf("create entity while holding guard: %v", err)
	}
	if err := writer.Commit().Error; err != nil {
		t.Fatalf("commit writer: %v", err)
	}

	freeze := <-result
	if freeze.err != nil {
		t.Fatalf("freeze after writer: %v", freeze.err)
	}
	if freeze.impact.ReferenceCount != 1 {
		t.Fatalf("freeze impact = %+v, want writer reference", freeze.impact)
	}
}

func TestPostgresStandardReferenceGuardRejectsWriterAfterFreeze(t *testing.T) {
	db := openStandardReferenceGuardPostgres(t)
	tenantID := time.Now().UnixNano()
	resourceID := tenantID + 1
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM model.entities WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM model.standard_reference_guards WHERE tenant_id = ?", tenantID).Error
	})
	if err := LockStandardReferences(db, tenantID, models.StandardReference{ResourceType: models.StandardResourceDomain, ResourceID: resourceID}); err != nil {
		t.Fatalf("create open guard: %v", err)
	}

	freeze := db.Begin()
	if freeze.Error != nil {
		t.Fatalf("begin freeze transaction: %v", freeze.Error)
	}
	guard, err := lockStandardReferenceGuard(freeze, tenantID, models.StandardResourceDomain, resourceID)
	if err != nil {
		_ = freeze.Rollback().Error
		t.Fatalf("lock freeze guard: %v", err)
	}
	if err := freeze.Model(&models.StandardReferenceGuard{}).Where("id = ?", guard.ID).
		Update("state", models.StandardReferenceGuardFrozen).Error; err != nil {
		_ = freeze.Rollback().Error
		t.Fatalf("freeze guard: %v", err)
	}

	writerResult := make(chan error, 1)
	go func() {
		writerResult <- db.Transaction(func(tx *gorm.DB) error {
			if err := LockStandardReferences(tx, tenantID, models.StandardReference{ResourceType: models.StandardResourceDomain, ResourceID: resourceID}); err != nil {
				return err
			}
			return tx.Create(&models.Entity{
				TenantID: tenantID, DomainID: &resourceID, Name: "Late writer",
				Code: fmt.Sprintf("late_%d", tenantID), Status: "draft", CreatedBy: 1,
			}).Error
		})
	}()
	waitForStandardReferenceGuardLock(t, db)
	if err := freeze.Commit().Error; err != nil {
		t.Fatalf("commit freeze: %v", err)
	}
	if err := <-writerResult; !errors.Is(err, ErrStandardReferenceFrozen) {
		t.Fatalf("late writer error = %v, want ErrStandardReferenceFrozen", err)
	}

	var count int64
	if err := db.Model(&models.Entity{}).Where("tenant_id = ? AND domain_id = ?", tenantID, resourceID).Count(&count).Error; err != nil {
		t.Fatalf("count late writer entities: %v", err)
	}
	if count != 0 {
		t.Fatalf("late writer entity count = %d, want 0", count)
	}
}
