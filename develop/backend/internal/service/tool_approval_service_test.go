package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	commonAuth "github.com/addp/common/authorization"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeApprovalExecutor struct {
	prepareCount int
	persistCount int
	startCount   int
}

func (executor *fakeApprovalExecutor) prepareContentExecution(
	_ context.Context,
	devType string,
	content map[string]interface{},
	executionConfig map[string]interface{},
	tenantID uint,
	userID uint,
	triggerType string,
	_ int,
) (*preparedContentExecution, error) {
	executor.prepareCount++
	now := time.Now().UTC()
	triggeredBy := int(userID)
	return &preparedContentExecution{
		execution: &commonExecution.TaskExecution{
			TenantID:        int(tenantID),
			ExecutionID:     uuid.NewString(),
			Module:          commonExecution.ModuleDevelop,
			TaskType:        devType,
			Source:          commonExecution.ModuleDevelop,
			Status:          commonExecution.ExecutionStatusPending,
			TriggerType:     triggerType,
			TriggeredBy:     &triggeredBy,
			ExecutionConfig: commonModels.JSONMap{"content": content, "execution_config": executionConfig},
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		devTask:  &models.DevTask{DevType: devType},
		tenantID: int(tenantID),
	}, nil
}

func (executor *fakeApprovalExecutor) persistPreparedContentExecution(
	ctx context.Context,
	repo *commonExecution.TaskExecutionRepository,
	prepared *preparedContentExecution,
) error {
	executor.persistCount++
	return repo.Create(ctx, prepared.execution)
}

func (executor *fakeApprovalExecutor) startPreparedContentExecution(_ *preparedContentExecution) {
	executor.startCount++
}

func TestToolApprovalRequiresOwnerDecisionAndConsumesOnce(t *testing.T) {
	db := newToolApprovalTestDB(t)
	executor := &fakeApprovalExecutor{}
	service := &ToolApprovalService{db: db, executor: executor, now: func() time.Time {
		return time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	}}
	runID := uuid.NewString()
	initialContext := delegatedApprovalContext(3, 5, runID, "call-initial")

	approval, err := service.CreateWorkflowRunApproval(
		context.Background(),
		initialContext,
		workflowRunRequest(),
	)
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}
	if approval.Status != models.ToolApprovalStatusPending {
		t.Fatalf("status = %s, want pending", approval.Status)
	}
	if executor.persistCount != 0 || executor.startCount != 0 {
		t.Fatalf("initial request executed: persist=%d start=%d", executor.persistCount, executor.startCount)
	}

	_, err = service.ConsumeWorkflowRunApproval(
		context.Background(),
		delegatedApprovalContext(3, 5, runID, "call-before-approval"),
		approval.ID.String(),
		approval.RequestFingerprint,
	)
	assertApprovalErrorCode(t, err, "approval_not_approved")

	decided, err := service.DecideApproval(
		context.Background(),
		userApprovalContext(3, 5),
		approval.ID.String(),
		models.ToolApprovalStatusApproved,
	)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if decided.Status != models.ToolApprovalStatusApproved {
		t.Fatalf("status = %s, want approved", decided.Status)
	}

	executionID, err := service.ConsumeWorkflowRunApproval(
		context.Background(),
		delegatedApprovalContext(3, 5, runID, "call-consume"),
		approval.ID.String(),
		approval.RequestFingerprint,
	)
	if err != nil {
		t.Fatalf("consume approval: %v", err)
	}
	if executionID == "" || executor.persistCount != 1 || executor.startCount != 1 {
		t.Fatalf("execution=%q persist=%d start=%d", executionID, executor.persistCount, executor.startCount)
	}

	_, err = service.ConsumeWorkflowRunApproval(
		context.Background(),
		delegatedApprovalContext(3, 5, runID, "call-repeat"),
		approval.ID.String(),
		approval.RequestFingerprint,
	)
	assertApprovalErrorCode(t, err, "approval_already_consumed")
	if executor.persistCount != 1 || executor.startCount != 1 {
		t.Fatalf("approval executed more than once: persist=%d start=%d", executor.persistCount, executor.startCount)
	}

	service.now = func() time.Time { return approval.ExpiresAt.Add(time.Second) }
	_, err = service.DecideApproval(
		context.Background(),
		userApprovalContext(3, 5),
		approval.ID.String(),
		models.ToolApprovalStatusRejected,
	)
	assertApprovalErrorCode(t, err, "approval_already_consumed")
	var terminal models.ToolApproval
	if err := db.First(&terminal, "id = ?", approval.ID).Error; err != nil {
		t.Fatalf("load terminal approval: %v", err)
	}
	if terminal.Status != models.ToolApprovalStatusConsumed {
		t.Fatalf("terminal status regressed to %s", terminal.Status)
	}
}

func TestToolApprovalRejectsFingerprintAndTenantMismatch(t *testing.T) {
	db := newToolApprovalTestDB(t)
	executor := &fakeApprovalExecutor{}
	service := &ToolApprovalService{db: db, executor: executor, now: time.Now}
	runID := uuid.NewString()
	approval, err := service.CreateWorkflowRunApproval(
		context.Background(),
		delegatedApprovalContext(3, 5, runID, "call-initial"),
		workflowRunRequest(),
	)
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}

	_, err = service.GetApproval(context.Background(), userApprovalContext(3, 6), approval.ID.String())
	assertApprovalErrorCode(t, err, "approval_not_found")

	_, err = service.ConsumeWorkflowRunApproval(
		context.Background(),
		delegatedApprovalContext(3, 5, runID, "call-consume"),
		approval.ID.String(),
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	assertApprovalErrorCode(t, err, "approval_request_mismatch")
	if executor.persistCount != 0 || executor.startCount != 0 {
		t.Fatal("mismatched request must not create execution")
	}
}

func TestToolApprovalExpiresBeforeDecision(t *testing.T) {
	db := newToolApprovalTestDB(t)
	executor := &fakeApprovalExecutor{}
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	service := &ToolApprovalService{db: db, executor: executor, now: func() time.Time { return now }}
	approval, err := service.CreateWorkflowRunApproval(
		context.Background(),
		delegatedApprovalContext(3, 5, uuid.NewString(), "call-initial"),
		workflowRunRequest(),
	)
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}
	now = now.Add(toolApprovalTTL + time.Second)

	_, err = service.DecideApproval(
		context.Background(),
		userApprovalContext(3, 5),
		approval.ID.String(),
		models.ToolApprovalStatusApproved,
	)
	assertApprovalErrorCode(t, err, "approval_expired")

	var stored models.ToolApproval
	if err := db.First(&stored, "id = ?", approval.ID).Error; err != nil {
		t.Fatalf("load expired approval: %v", err)
	}
	if stored.Status != models.ToolApprovalStatusExpired {
		t.Fatalf("status = %s, want expired", stored.Status)
	}
}

func assertApprovalErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	var approvalErr *ToolApprovalError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("error = %v, want ToolApprovalError %s", err, want)
	}
	if approvalErr.Code != want {
		t.Fatalf("code = %s, want %s", approvalErr.Code, want)
	}
}

func delegatedApprovalContext(userID, tenantID uint, runID, callID string) commonAuth.AuthContext {
	tenantIDText := strconv.FormatUint(uint64(tenantID), 10)
	return commonAuth.AuthContext{
		Principal: commonAuth.AuthPrincipal{Type: "user", ID: strconv.FormatUint(uint64(userID), 10)},
		Context:   commonAuth.AuthSessionContext{Type: "tenant", TenantID: &tenantIDText},
		Token:     commonAuth.TokenFacts{Type: "delegated_access_token"},
		Delegation: &commonAuth.DelegationFacts{
			AgentRunID: runID,
			ToolCallID: callID,
		},
	}
}

func userApprovalContext(userID, tenantID uint) commonAuth.AuthContext {
	tenantIDText := strconv.FormatUint(uint64(tenantID), 10)
	return commonAuth.AuthContext{
		Principal: commonAuth.AuthPrincipal{Type: "user", ID: strconv.FormatUint(uint64(userID), 10)},
		Context:   commonAuth.AuthSessionContext{Type: "tenant", TenantID: &tenantIDText},
		Token:     commonAuth.TokenFacts{Type: "first_party_access_token"},
	}
}

func workflowRunRequest() models.CreateExecutionRequest {
	return models.CreateExecutionRequest{
		DevType:     "workflow",
		TriggerType: "manual",
		Content: map[string]interface{}{
			"workflow_definition": map[string]interface{}{
				"tasks": []interface{}{
					map[string]interface{}{
						"id":         "load-data",
						"operator":   "load",
						"params":     map[string]interface{}{},
						"depends_on": []interface{}{},
					},
				},
			},
			"inputs": map[string]interface{}{},
		},
		ExecutionConfig: map[string]interface{}{"engine_id": float64(20)},
		Timeout:         300,
	}
}

func newToolApprovalTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	for _, schema := range []string{"develop", "common"} {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS " + schema).Error; err != nil {
			t.Fatalf("attach %s schema: %v", schema, err)
		}
	}
	statements := []string{
		`CREATE TABLE develop.tool_approvals (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			agent_run_id TEXT NOT NULL,
			tool_call_id TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			request_fingerprint TEXT NOT NULL,
			request_payload JSON NOT NULL,
			request_summary JSON NOT NULL,
			status TEXT NOT NULL,
			requested_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL,
			decided_at DATETIME,
			decided_by_user_id INTEGER,
			consumed_at DATETIME,
			execution_id TEXT
		)`,
		`CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			execution_id TEXT NOT NULL UNIQUE,
			module TEXT NOT NULL,
			task_type TEXT NOT NULL,
			source TEXT NOT NULL,
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
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create approval test table: %v", err)
		}
	}
	return db
}
