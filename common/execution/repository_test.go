package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
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
	insertTaskExecutionRepositoryTestRowWithSourceTask(t, db, 4, 7, "transfer-other", ModuleTransfer, TaskTypeSync, ptrString("42"), ExecutionStatusSuccess, 480, "2026-01-01 10:03:00")

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

func TestGetByExecutionIDScopesTenant(t *testing.T) {
	db := newTaskExecutionRepositoryTestDB(t)
	repo := NewTaskExecutionRepository(db)
	insertTaskExecutionRepositoryTestRowWithSourceTask(t, db, 1, 7, "tenant-7-execution", ModuleQuality, TaskTypeQualityCheck, nil, ExecutionStatusSuccess, 10, "2026-01-01 10:00:00")

	if _, err := repo.GetByExecutionID(context.Background(), "tenant-7-execution", 8); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("cross-tenant execution lookup error = %v, want ErrNotFound", err)
	}
	item, err := repo.GetByExecutionID(context.Background(), "tenant-7-execution", 7)
	if err != nil {
		t.Fatalf("same-tenant execution lookup: %v", err)
	}
	if item.Module != ModuleQuality || item.TaskType != TaskTypeQualityCheck {
		t.Fatalf("execution identity = module %q task_type %q", item.Module, item.TaskType)
	}
}

func TestListUsesStableCreatedAtAndIDOrder(t *testing.T) {
	db := newTaskExecutionRepositoryTestDB(t)
	repo := NewTaskExecutionRepository(db)
	insertTaskExecutionRepositoryTestRowWithSourceTask(t, db, 1, 7, "older", ModuleQuality, TaskTypeQualityCheck, nil, ExecutionStatusSuccess, 10, "2026-01-01 10:00:00")
	insertTaskExecutionRepositoryTestRowWithSourceTask(t, db, 2, 7, "same-time-low", ModuleQuality, TaskTypeQualityCheck, nil, ExecutionStatusSuccess, 20, "2026-01-01 10:01:00")
	insertTaskExecutionRepositoryTestRowWithSourceTask(t, db, 3, 7, "same-time-high", ModuleQuality, TaskTypeQualityCheck, nil, ExecutionStatusFailed, 30, "2026-01-01 10:01:00")

	items, total, err := repo.List(context.Background(), TaskExecutionFilter{TenantID: 7, Module: ModuleQuality, TaskType: TaskTypeQualityCheck, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("total/items = %d/%d, want 3/3", total, len(items))
	}
	if items[0].ExecutionID != "same-time-high" || items[1].ExecutionID != "same-time-low" || items[2].ExecutionID != "older" {
		t.Fatalf("execution order = [%s, %s, %s], want stable id tie-break", items[0].ExecutionID, items[1].ExecutionID, items[2].ExecutionID)
	}
}

func TestUpdateFieldsReturnsNotFoundWhenExecutionDoesNotMatchTenant(t *testing.T) {
	db := newTaskExecutionRepositoryTestDB(t)
	repo := NewTaskExecutionRepository(db)

	insertTaskExecutionRepositoryTestRow(t, db, 1, 7, "run-1", nil, "2026-01-01 10:00:00")

	err := repo.UpdateFields(context.Background(), "run-1", 8, map[string]interface{}{
		"status": ExecutionStatusSuccess,
	})
	if !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("UpdateFields error = %v, want ErrNotFound", err)
	}
}

func TestStartExecutionAtomicallySetsRunningAndStartedAt(t *testing.T) {
	db := newTaskExecutionRepositoryTestDB(t)
	repo := NewTaskExecutionRepository(db)
	createdAt := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	exec := &TaskExecution{
		TenantID: 7, ExecutionID: "pending-1", Module: ModuleMeta, TaskType: TaskTypeScan,
		Source: ModuleMeta, Status: ExecutionStatusPending, TriggerType: TriggerTypeManual,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := repo.Create(context.Background(), exec); err != nil {
		t.Fatalf("create pending execution: %v", err)
	}
	if exec.StartedAt != nil {
		t.Fatalf("pending started_at = %v, want nil", exec.StartedAt)
	}

	startedAt := createdAt.Add(30 * time.Second)
	if err := repo.StartExecution(context.Background(), exec.ExecutionID, exec.TenantID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	stored, err := repo.GetByExecutionID(context.Background(), exec.ExecutionID, exec.TenantID)
	if err != nil {
		t.Fatalf("load started execution: %v", err)
	}
	if stored.Status != ExecutionStatusRunning || stored.StartedAt == nil || !stored.StartedAt.Equal(startedAt) {
		t.Fatalf("started execution status=%s started_at=%v, want running %v", stored.Status, stored.StartedAt, startedAt)
	}
	if err := repo.StartExecution(context.Background(), exec.ExecutionID, exec.TenantID, startedAt.Add(time.Second)); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("second StartExecution error = %v, want ErrConflict", err)
	}
}

func TestGetRunningExecutionsWithZeroTenantReturnsAllTenants(t *testing.T) {
	db := newTaskExecutionRepositoryTestDB(t)
	repo := NewTaskExecutionRepository(db)
	createdAt := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for _, exec := range []*TaskExecution{
		{TenantID: 7, ExecutionID: "pending-7", Module: ModuleMeta, TaskType: TaskTypeScan, Source: ModuleMeta, Status: ExecutionStatusPending, TriggerType: TriggerTypeManual, CreatedAt: createdAt, UpdatedAt: createdAt},
		{TenantID: 8, ExecutionID: "running-8", Module: ModuleMeta, TaskType: TaskTypeScan, Source: ModuleMeta, Status: ExecutionStatusRunning, TriggerType: TriggerTypeManual, CreatedAt: createdAt.Add(time.Minute), UpdatedAt: createdAt.Add(time.Minute)},
		{TenantID: 9, ExecutionID: "success-9", Module: ModuleMeta, TaskType: TaskTypeScan, Source: ModuleMeta, Status: ExecutionStatusSuccess, TriggerType: TriggerTypeManual, CreatedAt: createdAt.Add(2 * time.Minute), UpdatedAt: createdAt.Add(2 * time.Minute)},
	} {
		if err := repo.Create(context.Background(), exec); err != nil {
			t.Fatalf("create %s: %v", exec.ExecutionID, err)
		}
	}

	executions, err := repo.GetRunningExecutions(context.Background(), 0)
	if err != nil {
		t.Fatalf("GetRunningExecutions() error = %v", err)
	}
	if len(executions) != 2 || executions[0].ExecutionID != "running-8" || executions[1].ExecutionID != "pending-7" {
		t.Fatalf("executions = %#v", executions)
	}

	tenantExecutions, err := repo.GetRunningExecutions(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetRunningExecutions(tenant 7) error = %v", err)
	}
	if len(tenantExecutions) != 1 || tenantExecutions[0].ExecutionID != "pending-7" {
		t.Fatalf("tenant 7 executions = %#v", tenantExecutions)
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
		actor_principal_id INTEGER,
		actor_tenant_membership_id INTEGER,
		issued_authorization_version INTEGER,
		execution_authorization_id INTEGER,
		authorization_effects TEXT,
		authorization_expires_at DATETIME,
		execution_config JSON,
		error_details JSON,
		metadata JSON,
		execution_time_ms INTEGER,
		rows_affected INTEGER,
		records_read INTEGER,
		records_written INTEGER,
		bytes_read INTEGER,
		bytes_written INTEGER,
		lease_owner TEXT,
		lease_expires_at DATETIME,
		attempt INTEGER NOT NULL DEFAULT 0,
		max_attempts INTEGER NOT NULL DEFAULT 3,
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
