package repository

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLogRepositoryApplyFiltersIncludesResourceIdentity(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "",
		PreferSimpleProtocol: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open dry run db: %v", err)
	}

	repo := NewLogRepository(nil)
	tx := repo.applyFilters(db.Model(&models.AuditLog{}), &models.AuditLogFilters{
		ResourcePath: " cleanup.execute.",
		EntityType:   "cleanup",
		EntityID:     "cleanup-task-1",
	}).Find(&[]models.AuditLog{})

	sql := tx.Statement.SQL.String()
	for _, fragment := range []string{
		"resource_path LIKE",
		"entity_type =",
		"entity_id =",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("sql %q does not contain %q", sql, fragment)
		}
	}

	wantVars := []interface{}{"cleanup.execute.%", "cleanup", "cleanup-task-1"}
	if !reflect.DeepEqual(tx.Statement.Vars, wantVars) {
		t.Fatalf("vars = %#v, want %#v", tx.Statement.Vars, wantVars)
	}
}

func TestLogRepositoryListFiltersResourceIdentity(t *testing.T) {
	db := newLogRepositoryTestDB(t)
	repo := NewLogRepository(db)

	insertAuditLog(t, db, 1, "cleanup.execute.created", "cleanup", "cleanup-task-1")
	insertAuditLog(t, db, 2, "cleanup.completed", "cleanup", "cleanup-task-1")
	insertAuditLog(t, db, 3, "cleanup.execute.created", "cleanup", "cleanup-task-2")
	insertAuditLog(t, db, 4, "/api/v1/system/login", "", "")

	logs, total, err := repo.List(0, 20, &models.AuditLogFilters{
		EntityType: "cleanup",
		EntityID:   "cleanup-task-1",
	})
	if err != nil {
		t.Fatalf("list by entity: %v", err)
	}
	if total != 2 || len(logs) != 2 {
		t.Fatalf("entity filter total=%d len=%d, want 2/2", total, len(logs))
	}
	for _, log := range logs {
		if log.EntityID != "cleanup-task-1" {
			t.Fatalf("entity filter returned entity_id=%q", log.EntityID)
		}
	}

	logs, total, err = repo.List(0, 20, &models.AuditLogFilters{
		ResourcePath: "cleanup.execute",
	})
	if err != nil {
		t.Fatalf("list by resource path: %v", err)
	}
	if total != 2 || len(logs) != 2 {
		t.Fatalf("resource_path filter total=%d len=%d, want 2/2", total, len(logs))
	}
	for _, log := range logs {
		if !strings.HasPrefix(log.ResourcePath, "cleanup.execute") {
			t.Fatalf("resource_path filter returned %q", log.ResourcePath)
		}
	}
}

func TestLogRepositoryGetLogsBeforeDateReturnsEarliestBatch(t *testing.T) {
	db := newLogRepositoryTestDB(t)
	repo := NewLogRepository(db)

	insertAuditLog(t, db, 1, "cleanup.execute.created", "cleanup", "cleanup-task-1")
	insertAuditLog(t, db, 2, "cleanup.execute.created", "cleanup", "cleanup-task-2")
	insertAuditLog(t, db, 3, "cleanup.execute.created", "cleanup", "cleanup-task-3")

	cutoff := time.Date(2026, 6, 18, 10, 4, 0, 0, time.UTC)
	logs, err := repo.GetLogsBeforeDate(cutoff, 2)
	if err != nil {
		t.Fatalf("GetLogsBeforeDate() error = %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("len(logs) = %d, want 2", len(logs))
	}
	if logs[0].ID != 1 || logs[1].ID != 2 {
		t.Fatalf("log IDs = [%d %d], want [1 2]", logs[0].ID, logs[1].ID)
	}
}

func newLogRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate audit_logs: %v", err)
	}
	return db
}

func insertAuditLog(t *testing.T, db *gorm.DB, id uint, resourcePath string, entityType string, entityID string) {
	t.Helper()
	log := &models.AuditLog{
		ID:           id,
		CreatedAt:    time.Date(2026, 6, 18, 10, int(id), 0, 0, time.UTC),
		HTTPMethod:   "SYSTEM",
		ResourcePath: resourcePath,
		HTTPStatus:   200,
		DurationMs:   0,
		EntityType:   entityType,
		EntityID:     entityID,
		IPAddress:    "127.0.0.1",
		LogLevel:     "INFO",
		RequestID:    "request-id",
		ModuleName:   "system",
	}
	if err := db.Create(log).Error; err != nil {
		t.Fatalf("insert audit log: %v", err)
	}
}
