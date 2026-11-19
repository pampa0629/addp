package repository

import (
	"os"
	"testing"

	"github.com/addp/develop/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSharedEditorLayoutMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS develop").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.DevTask{}, &models.CatalogResourceChangeRow{}); err != nil {
		t.Fatal(err)
	}
	if err := migrateCatalogDevTaskChanges(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("DELETE FROM develop.data_migrations WHERE version = 2026111901").Error; err != nil {
		t.Fatal(err)
	}
	var taskID uint
	if err := tx.Raw(`INSERT INTO develop.dev_tasks (tenant_id,name,dev_type,content,editor_layout,updated_at)
		VALUES (707, 'editor-layout-migration-fixture', 'workflow', '{}'::jsonb,
		'{"nodes":{"n1":{"x":120,"y":240}},"viewport":{"zoom":1.3}}'::jsonb, TIMESTAMPTZ '2025-01-01 00:00:00+00')
		RETURNING id`).Scan(&taskID).Error; err != nil || taskID == 0 {
		t.Fatalf("seed workflow layout: id=%d err=%v", taskID, err)
	}
	for range 2 {
		if err := migrateSharedEditorLayout(tx); err != nil {
			t.Fatal(err)
		}
	}
	var positionsPreserved bool
	if err := tx.Raw(`SELECT editor_layout = '{"nodes":{"n1":{"x":120,"y":240}}}'::jsonb
		AND updated_at = TIMESTAMPTZ '2025-01-01 00:00:00+00' FROM develop.dev_tasks WHERE id = ?`, taskID).
		Scan(&positionsPreserved).Error; err != nil || !positionsPreserved {
		t.Fatalf("migration must preserve node positions and task time: result=%t err=%v", positionsPreserved, err)
	}
}
