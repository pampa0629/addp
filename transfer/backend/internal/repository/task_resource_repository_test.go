package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskResourceRepositorySoftDeletesTaskPrivateStateAndCancelsActiveExecutions(t *testing.T) {
	db := newTaskResourceRepositoryTestDB(t)
	now := time.Date(2026, 7, 24, 2, 0, 0, 0, time.UTC)
	seedTaskResourceFixture(t, db, 7, 11, models.TaskStatusIdle, models.TaskDesiredStateStopped, now.Add(-time.Minute), "")
	seedTaskResourceExecution(t, db, 7, 11, "pending", commonExecution.ExecutionStatusPending, nil)
	startedAt := now.Add(-2 * time.Minute)
	seedTaskResourceExecution(t, db, 7, 11, "running", commonExecution.ExecutionStatusRunning, &startedAt)
	seedTaskResourceExecution(t, db, 7, 11, "success", commonExecution.ExecutionStatusSuccess, nil)

	stats, err := NewTaskResourceRepository(db).DeleteTaskAndPrivateState(
		context.Background(), 7, 11, false, TaskDefinitionDeleteSoft, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats != (TaskPrivateStateDeleteStats{DeadLetters: 1, SyncStates: 1, RuntimeLeases: 1, SchemaChangeRequests: 1, CaptureResources: 1, CancelledExecutions: 2}) {
		t.Fatalf("delete stats = %#v", stats)
	}
	assertTaskResourceCounts(t, db, 11, 0, 0, 0, 0, 3)
	assertTaskDefinitionCounts(t, db, 11, 0, 1)

	pending := loadTaskResourceExecution(t, db, "pending")
	running := loadTaskResourceExecution(t, db, "running")
	success := loadTaskResourceExecution(t, db, "success")
	for _, execution := range []commonExecution.TaskExecution{pending, running} {
		if execution.Status != commonExecution.ExecutionStatusCancelled || execution.CompletedAt == nil || execution.Metadata["stop_reason"] != "cleanup" {
			t.Fatalf("cancelled execution = %#v", execution)
		}
	}
	if pending.ExecutionTimeMs != nil {
		t.Fatalf("pending execution duration = %v, want nil", *pending.ExecutionTimeMs)
	}
	if running.ExecutionTimeMs == nil || *running.ExecutionTimeMs != 120000 {
		t.Fatalf("running execution duration = %v, want 120000", running.ExecutionTimeMs)
	}
	if success.Status != commonExecution.ExecutionStatusSuccess || success.CompletedAt != nil {
		t.Fatalf("terminal execution changed = %#v", success)
	}
}

func TestTaskResourceRepositoryPhysicallyDeletesTaskDefinition(t *testing.T) {
	db := newTaskResourceRepositoryTestDB(t)
	now := time.Date(2026, 7, 24, 2, 30, 0, 0, time.UTC)
	seedTaskResourceFixture(t, db, 7, 11, models.TaskStatusIdle, models.TaskDesiredStateStopped, now.Add(-time.Minute), "")

	if _, err := NewTaskResourceRepository(db).DeleteTaskAndPrivateState(
		context.Background(), 7, 11, false, TaskDefinitionDeletePhysical, now,
	); err != nil {
		t.Fatal(err)
	}
	assertTaskDefinitionCounts(t, db, 11, 0, 0)
}

func TestTaskResourceRepositoryRuntimeGuardsRollBackAllFacts(t *testing.T) {
	now := time.Date(2026, 7, 24, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		status       models.TaskStatus
		desiredState models.TaskDesiredState
		leaseUntil   time.Time
		leaseOwner   string
		continuous   bool
	}{
		{name: "desired state running", status: models.TaskStatusIdle, desiredState: models.TaskDesiredStateRunning, leaseUntil: now.Add(-time.Minute), continuous: true},
		{name: "active lease", status: models.TaskStatusIdle, desiredState: models.TaskDesiredStateStopped, leaseUntil: now.Add(time.Minute), leaseOwner: "worker-1", continuous: true},
		{name: "running bounded task", status: models.TaskStatusRunning, desiredState: models.TaskDesiredStateStopped, leaseUntil: now.Add(-time.Minute), continuous: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTaskResourceRepositoryTestDB(t)
			seedTaskResourceFixture(t, db, 7, 11, test.status, test.desiredState, test.leaseUntil, test.leaseOwner)
			seedTaskResourceExecution(t, db, 7, 11, "pending", commonExecution.ExecutionStatusPending, nil)

			stats, err := NewTaskResourceRepository(db).DeleteTaskAndPrivateState(
				context.Background(), 7, 11, test.continuous, TaskDefinitionDeleteSoft, now,
			)
			if !errors.Is(err, ErrTaskDeletionRuntimeActive) {
				t.Fatalf("delete error = %v, want ErrTaskDeletionRuntimeActive", err)
			}
			if stats != (TaskPrivateStateDeleteStats{}) {
				t.Fatalf("delete stats = %#v, want zero", stats)
			}
			assertTaskDefinitionCounts(t, db, 11, 1, 1)
			assertTaskResourceCounts(t, db, 11, 1, 1, 1, 1, 1)
			if execution := loadTaskResourceExecution(t, db, "pending"); execution.Status != commonExecution.ExecutionStatusPending || execution.CompletedAt != nil {
				t.Fatalf("guard changed execution = %#v", execution)
			}
		})
	}
}

func newTaskResourceRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:task_resource_repository_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	for _, schema := range []string{"transfer", "common"} {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS " + schema).Error; err != nil {
			t.Fatal(err)
		}
	}
	statements := []string{
		`CREATE TABLE transfer.transfer_tasks (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, config JSON,
			status TEXT NOT NULL, desired_state TEXT NOT NULL, deleted_at DATETIME
		)`,
		`CREATE TABLE transfer.dead_letters (identity TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, task_id INTEGER NOT NULL)`,
		`CREATE TABLE transfer.sync_states (id INTEGER PRIMARY KEY, task_id INTEGER NOT NULL)`,
		`CREATE TABLE transfer.runtime_leases (
			id INTEGER PRIMARY KEY, task_id INTEGER NOT NULL, owner_instance_id TEXT NOT NULL,
			lease_until DATETIME NOT NULL
		)`,
		`CREATE TABLE transfer.capture_resources (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, task_id INTEGER NOT NULL)`,
		`CREATE TABLE transfer.schema_change_requests (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, task_id INTEGER NOT NULL)`,
		`CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, execution_id TEXT NOT NULL,
			module TEXT NOT NULL, task_type TEXT NOT NULL, source_task_id TEXT, status TEXT NOT NULL,
			metadata JSON, started_at DATETIME, completed_at DATETIME, execution_time_ms INTEGER,
			created_at DATETIME, updated_at DATETIME
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	addTaskExecutionAuthorizationColumns(t, db)
	return db
}

func seedTaskResourceFixture(
	t *testing.T,
	db *gorm.DB,
	tenantID, taskID uint,
	status models.TaskStatus,
	desiredState models.TaskDesiredState,
	leaseUntil time.Time,
	leaseOwner string,
) {
	t.Helper()
	statements := []struct {
		query string
		args  []interface{}
	}{
		{`INSERT INTO transfer.transfer_tasks (id, tenant_id, config, status, desired_state) VALUES (?, ?, '{}', ?, ?)`, []interface{}{taskID, tenantID, status, desiredState}},
		{`INSERT INTO transfer.dead_letters (identity, tenant_id, task_id) VALUES (?, ?, ?)`, []interface{}{fmt.Sprintf("dead-letter-%d", taskID), tenantID, taskID}},
		{`INSERT INTO transfer.sync_states (id, task_id) VALUES (?, ?)`, []interface{}{taskID, taskID}},
		{`INSERT INTO transfer.runtime_leases (id, task_id, owner_instance_id, lease_until) VALUES (?, ?, ?, ?)`, []interface{}{taskID, taskID, leaseOwner, leaseUntil}},
		{`INSERT INTO transfer.capture_resources (id, tenant_id, task_id) VALUES (?, ?, ?)`, []interface{}{taskID, tenantID, taskID}},
		{`INSERT INTO transfer.schema_change_requests (id, tenant_id, task_id) VALUES (?, ?, ?)`, []interface{}{taskID, tenantID, taskID}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func seedTaskResourceExecution(t *testing.T, db *gorm.DB, tenantID, taskID uint, executionID, status string, startedAt *time.Time) {
	t.Helper()
	now := time.Date(2026, 7, 24, 1, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO common.task_executions
		(tenant_id, execution_id, module, task_type, source_task_id, status, metadata, started_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, '{"existing":"value"}', ?, ?, ?)`,
		tenantID, executionID, commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, fmt.Sprint(taskID), status, startedAt, now, now,
	).Error; err != nil {
		t.Fatal(err)
	}
}

func loadTaskResourceExecution(t *testing.T, db *gorm.DB, executionID string) commonExecution.TaskExecution {
	t.Helper()
	var execution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", executionID).First(&execution).Error; err != nil {
		t.Fatal(err)
	}
	return execution
}

func assertTaskDefinitionCounts(t *testing.T, db *gorm.DB, taskID uint, active, unscoped int64) {
	t.Helper()
	var activeCount, unscopedCount int64
	if err := db.Raw("SELECT COUNT(*) FROM transfer.transfer_tasks WHERE id = ? AND deleted_at IS NULL", taskID).Scan(&activeCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM transfer.transfer_tasks WHERE id = ?", taskID).Scan(&unscopedCount).Error; err != nil {
		t.Fatal(err)
	}
	if activeCount != active || unscopedCount != unscoped {
		t.Fatalf("task counts active=%d unscoped=%d, want %d/%d", activeCount, unscopedCount, active, unscoped)
	}
}

func assertTaskResourceCounts(t *testing.T, db *gorm.DB, taskID uint, deadLetters, syncStates, runtimeLeases, captureResources, executions int64) {
	t.Helper()
	tables := []struct {
		name string
		want int64
	}{
		{name: "transfer.dead_letters", want: deadLetters},
		{name: "transfer.sync_states", want: syncStates},
		{name: "transfer.runtime_leases", want: runtimeLeases},
		{name: "transfer.schema_change_requests", want: captureResources},
		{name: "transfer.capture_resources", want: captureResources},
		{name: "common.task_executions", want: executions},
	}
	for _, table := range tables {
		var count int64
		if table.name == "common.task_executions" {
			if err := db.Raw("SELECT COUNT(*) FROM common.task_executions WHERE source_task_id = ?", fmt.Sprint(taskID)).Scan(&count).Error; err != nil {
				t.Fatal(err)
			}
		} else if err := db.Raw("SELECT COUNT(*) FROM "+table.name+" WHERE task_id = ?", taskID).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != table.want {
			t.Fatalf("%s count = %d, want %d", table.name, count, table.want)
		}
	}
}
