package service

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestExecutionTreeBudgetMarksOnlyOmittedAccessibleEvidence(t *testing.T) {
	db := newExecutionQueryServiceTestDB(t)
	query := NewExecutionQueryService(commonExecution.NewTaskExecutionRepository(db))
	root := "budget-root"
	insertExecutionQueryServiceTestRow(t, db, 1000, root, nil)
	for i := 1; i <= 201; i++ {
		insertExecutionQueryServiceTestRow(t, db, 1000+i, fmt.Sprintf("budget-child-%d", i), &root)
	}
	tree, err := query.GetExecutionTreeByExecutionID(context.Background(), root, 7)
	if err != nil || !tree.Truncated || len(tree.Children) != 199 {
		t.Fatalf("tree=%#v err=%v", tree, err)
	}
	depthRoot := "depth-root"
	parent := ""
	for i := 0; i <= 8; i++ {
		id := fmt.Sprintf("depth-%d", i)
		if i == 0 {
			id = depthRoot
			insertExecutionQueryServiceTestRow(t, db, 1500+i, id, nil)
		} else {
			insertExecutionQueryServiceTestRow(t, db, 1500+i, id, &parent)
		}
		parent = id
	}
	tree, err = query.GetExecutionTreeByExecutionID(context.Background(), depthRoot, 7)
	if err != nil {
		t.Fatal(err)
	}
	leaf := tree
	for len(leaf.Children) > 0 {
		leaf = leaf.Children[0]
	}
	if leaf.Truncated {
		t.Fatal("complete depth-boundary leaf marked as truncated")
	}
}

func TestExecutionTreeFiltersHiddenChildrenBeforeBudgetAndKeepsDirectParents(t *testing.T) {
	db := newExecutionQueryServiceTestDB(t)
	query := NewExecutionQueryService(commonExecution.NewTaskExecutionRepository(db))
	root := "scoped-budget-parent"
	insert := func(id int, tenant, actor int, identifier, module, taskType string, parent *string, taskID *string) {
		t.Helper()
		row := commonExecution.TaskExecution{ID: int64(id), TenantID: tenant, ExecutionID: identifier,
			Module: module, TaskType: taskType, Source: module, ParentExecutionID: parent, SourceTaskID: taskID,
			TriggeredBy: &actor, Status: commonExecution.ExecutionStatusSuccess, TriggerType: commonExecution.TriggerTypeManual,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	taskID := "scoped-budget-task"
	insert(98000, 7, 33, root, commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, nil, &taskID)
	// More hidden children than the entire tree budget must not displace a
	// later visible child or reveal their existence through truncation.
	for i := 0; i < 205; i++ {
		insert(98001+i, 7, 36, fmt.Sprintf("scoped-hidden-%03d", i), commonExecution.ModuleMeta, commonExecution.TaskTypeScan, &root, nil)
	}
	insert(98206, 8, 33, "scoped-other-tenant", commonExecution.ModuleMeta, commonExecution.TaskTypeScan, &root, nil)
	visible := "scoped-own-scan"
	insert(98207, 7, 33, visible, commonExecution.ModuleMeta, commonExecution.TaskTypeScan, &root, nil)
	hiddenParent := "scoped-hidden-000"
	insert(98208, 7, 33, "scoped-own-below-hidden", commonExecution.ModuleMeta, commonExecution.TaskTypeScan, &hiddenParent, nil)
	ctx := commonExecution.WithReadScopes(context.Background(), []commonExecution.ReadScope{
		{Module: commonExecution.ModuleTransfer, TenantID: 7, PrincipalID: 33, Grants: []commonExecution.ReadGrant{{TaskType: commonExecution.TaskTypeSync, TaskHistory: true}}},
		{Module: commonExecution.ModuleMeta, TenantID: 7, PrincipalID: 33, Grants: []commonExecution.ReadGrant{{TaskType: commonExecution.TaskTypeScan, OwnAdHoc: true}}},
	})
	for _, lookup := range []func() (*ExecutionTreeNode, error){
		func() (*ExecutionTreeNode, error) { return query.GetExecutionTreeByExecutionID(ctx, root, 7) },
		func() (*ExecutionTreeNode, error) { return query.GetExecutionTree(ctx, 98000, 7) },
	} {
		tree, err := lookup()
		if err != nil {
			t.Fatal(err)
		}
		if tree.Truncated || len(tree.Children) != 1 || tree.Children[0].Execution.ExecutionID != visible || tree.Children[0].Truncated {
			t.Fatalf("hidden executions consumed the visible budget: %#v", tree)
		}
		child := tree.Children[0]
		if child.Execution.ParentExecutionID == nil || *child.Execution.ParentExecutionID != root || len(child.Children) != 0 {
			t.Fatalf("direct scan lineage changed: %#v", child)
		}
	}
	// Removing the Meta Owner grant leaves a readable task parent, with no
	// child or truncation hint. An empty scope cannot read even that root.
	parentOnly := commonExecution.WithReadScopes(context.Background(), []commonExecution.ReadScope{
		{Module: commonExecution.ModuleTransfer, TenantID: 7, PrincipalID: 36, Grants: []commonExecution.ReadGrant{{TaskType: commonExecution.TaskTypeSync, TaskHistory: true}}},
	})
	tree, err := query.GetExecutionTreeByExecutionID(parentOnly, root, 7)
	if err != nil || len(tree.Children) != 0 || tree.Truncated {
		t.Fatalf("parent read leaked children: tree=%#v err=%v", tree, err)
	}
	if _, err := query.GetExecutionTreeByExecutionID(commonExecution.WithReadScopes(context.Background(), nil), root, 7); err == nil {
		t.Fatal("empty Owner scope exposed the root execution")
	}
}
