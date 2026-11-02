package service

import (
	"context"
	"errors"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"testing"
	"time"
)

func runApprovalAuth() (authorization.AuthContext, authorization.AuthContext) {
	user := authtest.NewTenantUserAuthContext("7", "42", []string{"transfer.task.execute", "transfer.task.read"})
	delegated := authorization.CloneAuthContext(user)
	delegated.Token.Type = "delegated_access_token"
	delegated.Client.Audiences = []string{"transfer"}
	delegated.Client.ScopeMode = "restricted"
	delegated.Client.Scopes = []string{TransferStartTool}
	delegated.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *delegated.Client.ClientID, AgentRunID: uuid.NewString(), ToolCallID: uuid.NewString()}
	return user, delegated
}

func seedRunApprovalTask(t *testing.T, db *gorm.DB) (*TaskService, *models.TransferTask) {
	t.Helper()
	if db.Dialector.Name() == "sqlite" {
		// SQLite's attached schemas do not support GORM's qualified index DDL.
		if err := db.Exec(`CREATE TABLE transfer.run_approvals (
		 id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, task_id INTEGER NOT NULL,
		 principal_id TEXT NOT NULL, membership_id TEXT NOT NULL, authorization_version TEXT NOT NULL,
		 agent_run_id TEXT NOT NULL, request_fingerprint TEXT NOT NULL, request_summary JSON NOT NULL,
		 status TEXT NOT NULL, requested_at DATETIME NOT NULL, expires_at DATETIME NOT NULL,
		 decided_at DATETIME, consumed_at DATETIME, execution_id TEXT)`).Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.AutoMigrate(&models.RunApproval{}); err != nil {
		t.Fatal(err)
	}
	cfg := validOverridableTargetTransferTaskConfig()
	delete(cfg["target"].(map[string]interface{}), "override_policy")
	task := &models.TransferTask{TenantID: 7, Name: "approved-table-sync", TaskType: "sync", Config: cfg, BatchSize: 100, Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(db, nil, nil)
	svc.SetExecutionService(NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db)))
	return svc, task
}

func requireApprovalError(t *testing.T, err error, code string) {
	t.Helper()
	var failure *RunApprovalError
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestRunApprovalRequiresUserClickAndConsumesOnce(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	svc, task := seedRunApprovalTask(t, db)
	user, delegated := runApprovalAuth()
	ctx := commonExecution.WithUserOrigin(context.Background(), delegated)
	approval, err := svc.CreateRunApproval(ctx, delegated, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := models.StartTaskRequest{ApprovalID: approval.ID.String(), RequestFingerprint: approval.RequestFingerprint}
	_, err = svc.StartApprovedTask(ctx, delegated, task.ID, req)
	requireApprovalError(t, err, "approval_not_approved")
	var n int64
	db.Model(&commonExecution.TaskExecution{}).Count(&n)
	if n != 0 {
		t.Fatal("request created execution before user click")
	}
	decision := models.RunApprovalDecision{Decision: "approved", RequestFingerprint: approval.RequestFingerprint}
	_, err = svc.DecideRunApproval(ctx, delegated, approval.ID.String(), decision)
	requireApprovalError(t, err, "approval_forbidden")
	if _, err = svc.DecideRunApproval(ctx, user, approval.ID.String(), decision); err != nil {
		t.Fatal(err)
	}
	// The decision records only approval; the Tool must consume it separately.
	db.Model(&commonExecution.TaskExecution{}).Count(&n)
	if n != 0 {
		t.Fatal("decision started execution")
	}
	execution, err := svc.StartApprovedTask(ctx, delegated, task.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := svc.GetRunApproval(ctx, user, approval.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "consumed" || stored.ExecutionID == nil || *stored.ExecutionID != execution.ExecutionID {
		t.Fatalf("approval not bound to execution: %#v", stored)
	}
	_, err = svc.StartApprovedTask(ctx, delegated, task.ID, req)
	requireApprovalError(t, err, "approval_already_consumed")
	db.Model(&commonExecution.TaskExecution{}).Count(&n)
	if n != 1 {
		t.Fatalf("executions=%d", n)
	}
}

func TestRunApprovalRejectsDriftReplayAndExpiration(t *testing.T) {
	for _, kind := range []string{"task_drift", "membership", "authorization_version", "agent_run", "rejected", "expired", "fingerprint"} {
		t.Run(kind, func(t *testing.T) {
			db := newTransferTaskServiceTestDB(t)
			svc, task := seedRunApprovalTask(t, db)
			user, delegated := runApprovalAuth()
			ctx := commonExecution.WithUserOrigin(context.Background(), delegated)
			approval, err := svc.CreateRunApproval(ctx, delegated, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			decision := "approved"
			if kind == "rejected" {
				decision = "rejected"
			}
			if _, err = svc.DecideRunApproval(ctx, user, approval.ID.String(), models.RunApprovalDecision{Decision: decision, RequestFingerprint: approval.RequestFingerprint}); err != nil {
				t.Fatal(err)
			}
			req := models.StartTaskRequest{ApprovalID: approval.ID.String(), RequestFingerprint: approval.RequestFingerprint}
			code := "approval_forbidden"
			switch kind {
			case "task_drift":
				db.Model(task).Update("batch_size", 101)
				code = "approval_request_mismatch"
			case "membership":
				value := "999"
				delegated.Context.TenantMembershipID = &value
			case "authorization_version":
				delegated.Authorization.AuthorizationVersion = "999"
			case "agent_run":
				delegated.Delegation.AgentRunID = uuid.NewString()
			case "rejected":
				code = "approval_rejected"
			case "expired":
				db.Model(approval).Update("expires_at", time.Now().Add(-time.Minute))
				code = "approval_expired"
			case "fingerprint":
				req.RequestFingerprint = "wrong"
				code = "approval_request_mismatch"
			}
			_, err = svc.StartApprovedTask(ctx, delegated, task.ID, req)
			requireApprovalError(t, err, code)
			var n int64
			db.Model(&commonExecution.TaskExecution{}).Count(&n)
			if n != 0 {
				t.Fatalf("rejected run created %d executions", n)
			}
		})
	}
}

func TestRunApprovalGuardRollsBackWithFailedExecutionClaim(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	svc, task := seedRunApprovalTask(t, db)
	user, delegated := runApprovalAuth()
	ctx := commonExecution.WithUserOrigin(context.Background(), delegated)
	approval, err := svc.CreateRunApproval(ctx, delegated, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.DecideRunApproval(ctx, user, approval.ID.String(), models.RunApprovalDecision{Decision: "approved", RequestFingerprint: approval.RequestFingerprint}); err != nil {
		t.Fatal(err)
	}
	// Force the execution insert to fail after the approval guard has updated it.
	if err = db.Exec("CREATE TRIGGER common.reject_test_execution BEFORE INSERT ON task_executions BEGIN SELECT RAISE(ABORT, 'test insert rejected'); END").Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.StartApprovedTask(ctx, delegated, task.ID, models.StartTaskRequest{ApprovalID: approval.ID.String(), RequestFingerprint: approval.RequestFingerprint})
	if err == nil {
		t.Fatal("expected failed execution insert")
	}
	stored, err := svc.GetRunApproval(ctx, user, approval.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "approved" || stored.ExecutionID != nil {
		t.Fatalf("approval consumed without execution: %#v", stored)
	}
}
