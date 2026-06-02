package service

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

func TestCreateAutoRunsSubmitsUnscannedEngines(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewObjectCapabilities("s3"))
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	capJSON := commonModels.JSONString(capabilities)
	tenantID := uint(1)
	engineSvc := NewEngineService(db, "", "", nil)
	engineSvc.cacheEngine(&commonModels.Engine{
		ID:           9,
		TenantID:     &tenantID,
		Name:         "Business MinIO",
		EngineType:   "s3",
		IsActive:     true,
		Capabilities: &capJSON,
	})

	repo := NewScanService(db, engineSvc)
	taskSvc := NewScanTaskService(db, repo, engineSvc, nil)

	runs, err := taskSvc.CreateAutoRuns(context.Background(), tenantID, 7, "token")
	if err != nil {
		t.Fatalf("CreateAutoRuns() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len = %d, want 1", len(runs))
	}
	if runs[0].ExecutionID == "" || runs[0].Status != commonModels.ExecutionStatusPending {
		t.Fatalf("run = %#v", runs[0])
	}
	if got := jsonMapUint(runs[0].ExecutionConfig, "engine_id"); got != 9 {
		t.Fatalf("execution engine_id = %d, want 9", got)
	}
}

func createTaskExecutionTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			execution_id TEXT NOT NULL UNIQUE,
			module TEXT NOT NULL,
			task_type TEXT NOT NULL,
			source_task_id INTEGER,
			source_task_name TEXT,
			parent_execution_id TEXT,
			status TEXT NOT NULL,
			progress INTEGER DEFAULT 0,
			current_step TEXT,
			trigger_type TEXT NOT NULL,
			triggered_by INTEGER,
			execution_config JSON,
			error_details JSON,
			metadata JSON,
			execution_time_ms INTEGER,
			rows_affected INTEGER,
			records_read INTEGER,
			records_written INTEGER,
			bytes_read INTEGER,
			bytes_written INTEGER,
			started_at DATETIME,
			completed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
}

func jsonMapUint(m commonModels.JSONMap, key string) uint {
	switch value := m[key].(type) {
	case uint:
		return value
	case int:
		if value > 0 {
			return uint(value)
		}
	case int64:
		if value > 0 {
			return uint(value)
		}
	case float64:
		if value > 0 {
			return uint(value)
		}
	}
	return 0
}
