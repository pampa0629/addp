package execution

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListChildrenByParentExecutionID(t *testing.T) {
	db := newTaskExecutionRepositoryTestDB(t)
	repo := NewTaskExecutionRepository(db)

	insertTaskExecutionRepositoryTestRow(t, db, 1, 7, "root", nil, "2026-01-01 10:00:00")
	insertTaskExecutionRepositoryTestRow(t, db, 2, 7, "child-b", ptrString("root"), "2026-01-01 10:02:00")
	insertTaskExecutionRepositoryTestRow(t, db, 3, 7, "child-a", ptrString("root"), "2026-01-01 10:01:00")
	insertTaskExecutionRepositoryTestRow(t, db, 4, 8, "child-other-tenant", ptrString("root"), "2026-01-01 10:00:30")

	children, err := repo.ListChildrenByParentExecutionID(context.Background(), "root", 7)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("children len = %d, want 2", len(children))
	}
	if children[0].ExecutionID != "child-a" || children[1].ExecutionID != "child-b" {
		t.Fatalf("children order = [%s, %s], want [child-a, child-b]", children[0].ExecutionID, children[1].ExecutionID)
	}
}

func TestGetStatisticsFiltersBySourceTaskID(t *testing.T) {
	db := newTaskExecutionRepositoryTestDB(t)
	repo := NewTaskExecutionRepository(db)

	insertTaskExecutionRepositoryTestRowWithSourceTask(t, db, 1, 7, "query-1", ModuleDevelop, TaskTypeQuery, ptrString("42"), ExecutionStatusSuccess, 120, "2026-01-01 10:00:00")
	insertTaskExecutionRepositoryTestRowWithSourceTask(t, db, 2, 7, "query-2", ModuleDevelop, TaskTypeQuery, ptrString("42"), ExecutionStatusFailed, 240, "2026-01-01 10:01:00")
	insertTaskExecutionRepositoryTestRowWithSourceTask(t, db, 3, 7, "query-other", ModuleDevelop, TaskTypeQuery, ptrString("99"), ExecutionStatusSuccess, 360, "2026-01-01 10:02:00")
	insertTaskExecutionRepositoryTestRowWithSourceTask(t, db, 4, 7, "transfer-other", ModuleTransfer, TaskTypeImport, ptrString("42"), ExecutionStatusSuccess, 480, "2026-01-01 10:03:00")

	stats, err := repo.GetStatistics(context.Background(), TaskExecutionFilter{
		TenantID:     7,
		Module:       ModuleDevelop,
		TaskType:     TaskTypeQuery,
		SourceTaskID: ptrString("42"),
	})
	if err != nil {
		t.Fatalf("get statistics: %v", err)
	}
	if stats.Total != 2 {
		t.Fatalf("total = %d, want 2", stats.Total)
	}
	if stats.SuccessCount != 1 || stats.FailedCount != 1 {
		t.Fatalf("status counts success=%d failed=%d, want 1/1", stats.SuccessCount, stats.FailedCount)
	}
	if stats.AvgExecutionTimeMs != 180 {
		t.Fatalf("avg execution time = %v, want 180", stats.AvgExecutionTimeMs)
	}
}

func newTaskExecutionRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
		id INTEGER PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		execution_id TEXT NOT NULL,
		module TEXT NOT NULL,
		task_type TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		source_task_id TEXT,
		source_task_name TEXT,
		parent_execution_id TEXT,
		status TEXT NOT NULL,
		progress INTEGER,
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
	)`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
	return db
}

func insertTaskExecutionRepositoryTestRow(t *testing.T, db *gorm.DB, id int, tenantID int, executionID string, parentExecutionID *string, createdAt string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO common.task_executions (
		id, tenant_id, execution_id, module, task_type, source, parent_execution_id,
		status, progress, trigger_type, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, executionID, ModuleOrchestrator, TaskTypeOrchestration, ModuleOrchestrator, parentExecutionID,
		ExecutionStatusSuccess, 100, TriggerTypeManual, createdAt, createdAt,
	).Error; err != nil {
		t.Fatalf("insert task execution: %v", err)
	}
}

func insertTaskExecutionRepositoryTestRowWithSourceTask(t *testing.T, db *gorm.DB, id int, tenantID int, executionID string, module string, taskType string, sourceTaskID *string, status string, executionTimeMs int, createdAt string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO common.task_executions (
		id, tenant_id, execution_id, module, task_type, source, source_task_id,
		status, progress, trigger_type, execution_time_ms, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, executionID, module, taskType, module, sourceTaskID,
		status, 100, TriggerTypeManual, executionTimeMs, createdAt, createdAt,
	).Error; err != nil {
		t.Fatalf("insert task execution: %v", err)
	}
}

func ptrString(value string) *string {
	return &value
}
