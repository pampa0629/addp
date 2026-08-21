package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetExecutionTreeByExecutionID(t *testing.T) {
	db := newExecutionQueryServiceTestDB(t)
	repo := commonExecution.NewTaskExecutionRepository(db)
	service := NewExecutionQueryService(repo)

	insertExecutionQueryServiceTestRow(t, db, 1, "root-exec", nil)
	parentID := "root-exec"
	insertExecutionQueryServiceTestRow(t, db, 2, "child-exec", &parentID)

	tree, err := service.GetExecutionTreeByExecutionID(context.Background(), "root-exec", 7)
	if err != nil {
		t.Fatalf("GetExecutionTreeByExecutionID() error = %v", err)
	}
	if tree.Execution.ExecutionID != "root-exec" {
		t.Fatalf("root execution_id = %s, want root-exec", tree.Execution.ExecutionID)
	}
	if len(tree.Children) != 1 {
		t.Fatalf("children len = %d, want 1", len(tree.Children))
	}
	if tree.Children[0].Execution.ExecutionID != "child-exec" {
		t.Fatalf("child execution_id = %s, want child-exec", tree.Children[0].Execution.ExecutionID)
	}
}

func TestObserveExecutionProjectsDurationsAndSafeLeaseFacts(t *testing.T) {
	createdAt := time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)
	startedAt := createdAt.Add(12 * time.Second)
	expiresAt := startedAt.Add(30 * time.Second)
	owner := "meta-worker-1"
	token := "secret-lease-token"
	currentStep := "worker lease expired; retry pending"
	execution := &commonExecution.TaskExecution{
		ExecutionID: "observed", Status: commonExecution.ExecutionStatusRunning,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		CreatedAt:         createdAt, StartedAt: &startedAt, LeaseOwner: &owner,
		LeaseToken: &token, LeaseExpiresAt: &expiresAt, Attempt: 2, CurrentStep: &currentStep,
	}
	observed := observeExecution(execution, startedAt.Add(35*time.Second))
	if observed.QueueDurationMs != 12000 || observed.RunDurationMs != 35000 {
		t.Fatalf("durations = queue %d run %d", observed.QueueDurationMs, observed.RunDurationMs)
	}
	if observed.LeaseState != "expired" || observed.LeaseOwner == nil || *observed.LeaseOwner != owner {
		t.Fatalf("lease observation = %#v", observed)
	}
	if observed.RecoveryReason != "lease_expired" {
		t.Fatalf("recovery reason = %q", observed.RecoveryReason)
	}
	payload, err := json.Marshal(observed)
	if err != nil {
		t.Fatalf("marshal observation: %v", err)
	}
	if strings.Contains(string(payload), token) || strings.Contains(string(payload), "lease_token") {
		t.Fatalf("safe execution observation exposed lease token: %s", payload)
	}
}

func newExecutionQueryServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	return db
}

func insertExecutionQueryServiceTestRow(t *testing.T, db *gorm.DB, id int, executionID string, parentExecutionID *string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO common.task_executions (
		id, tenant_id, execution_id, module, task_type, source, parent_execution_id,
		status, progress, trigger_type, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, 7, executionID, commonExecution.ModuleOrchestrator, commonExecution.TaskTypeOrchestration,
		commonExecution.ModuleOrchestrator, parentExecutionID, commonExecution.ExecutionStatusSuccess,
		100, commonExecution.TriggerTypeManual, "2026-01-01 10:00:00", "2026-01-01 10:00:00",
	).Error; err != nil {
		t.Fatalf("insert task execution: %v", err)
	}
}
