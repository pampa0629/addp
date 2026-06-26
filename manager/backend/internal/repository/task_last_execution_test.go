package repository

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskLastExecutionUpdatesAreTenantScoped(t *testing.T) {
	db := newTaskLastExecutionRepositoryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 24, 9, 30, 0, 0, time.UTC)

	tests := []struct {
		name        string
		table       string
		update      func() error
		wantTenant7 string
	}{
		{
			name:  "tile cache",
			table: "manager.vector_tile_cache_tasks",
			update: func() error {
				return NewTileCacheRepository(db).UpdateTaskLastExecution(ctx, 1, 7, "exec-tenant-7", "running", now)
			},
			wantTenant7: "exec-tenant-7",
		},
		{
			name:  "quick view optimization",
			table: "manager.vector_quick_view_target_tasks",
			update: func() error {
				return NewQuickViewOptimizationRepository(db).UpdateTaskLastExecution(ctx, 1, 7, "exec-tenant-7", "running", now)
			},
			wantTenant7: "exec-tenant-7",
		},
		{
			name:  "raster COG",
			table: "manager.raster_cog_tasks",
			update: func() error {
				return NewRasterCOGRepository(db).UpdateTaskLastExecution(ctx, 1, 7, "exec-tenant-7", "running", now)
			},
			wantTenant7: "exec-tenant-7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insertTaskLastExecutionRow(t, db, tt.table, 1, 7, "")
			insertTaskLastExecutionRow(t, db, tt.table, 1, 8, "exec-tenant-8")

			if err := tt.update(); err != nil {
				t.Fatalf("update task last execution: %v", err)
			}

			if got := taskLastExecutionID(t, db, tt.table, 1, 7); got != tt.wantTenant7 {
				t.Fatalf("tenant 7 last_execution_id = %q, want %q", got, tt.wantTenant7)
			}
			if got := taskLastExecutionID(t, db, tt.table, 1, 8); got != "exec-tenant-8" {
				t.Fatalf("tenant 8 last_execution_id = %q, want unchanged exec-tenant-8", got)
			}
		})
	}
}

func newTaskLastExecutionRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:task_last_execution?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	for _, table := range []string{
		"vector_tile_cache_tasks",
		"vector_quick_view_target_tasks",
		"raster_cog_tasks",
	} {
		if err := db.Exec(`CREATE TABLE manager.` + table + ` (
			id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			last_execution_id TEXT,
			last_execution_status TEXT,
			last_run_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`).Error; err != nil {
			t.Fatalf("create %s table: %v", table, err)
		}
	}
	return db
}

func insertTaskLastExecutionRow(t *testing.T, db *gorm.DB, table string, id uint, tenantID uint, executionID string) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO `+table+` (id, tenant_id, last_execution_id, last_execution_status) VALUES (?, ?, ?, ?)`,
		id,
		tenantID,
		executionID,
		"old",
	).Error; err != nil {
		t.Fatalf("insert %s row: %v", table, err)
	}
}

func taskLastExecutionID(t *testing.T, db *gorm.DB, table string, id uint, tenantID uint) string {
	t.Helper()
	var value string
	if err := db.Raw(
		`SELECT COALESCE(last_execution_id, '') FROM `+table+` WHERE id = ? AND tenant_id = ?`,
		id,
		tenantID,
	).Scan(&value).Error; err != nil {
		t.Fatalf("select %s last_execution_id: %v", table, err)
	}
	return value
}
