package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const TransferStartTool = "transfer.task.start"

type RunApprovalError struct{ Code string }

func (e *RunApprovalError) Error() string { return e.Code }
func runApprovalError(code string) error  { return &RunApprovalError{Code: code} }

// runIdentity is a current identity binding, never a source of new permission.
func runIdentity(a authorization.AuthContext, delegated bool, permissions ...string) error {
	if authorization.ValidateAuthContext(a) != nil || a.Principal.Type != "user" || a.Context.Type != "tenant" || a.Context.TenantID == nil || a.Context.TenantMembershipID == nil {
		return runApprovalError("approval_forbidden")
	}
	if !authorization.HasContextPermissions(a, permissions...) {
		return runApprovalError("approval_forbidden")
	}
	if delegated {
		if a.Token.Type != "delegated_access_token" || a.Delegation == nil || a.Delegation.AgentRunID == "" || a.Delegation.ToolCallID == "" || len(a.Client.Audiences) != 1 || a.Client.Audiences[0] != "transfer" || len(a.Client.Scopes) != 1 || a.Client.Scopes[0] != TransferStartTool {
			return runApprovalError("approval_forbidden")
		}
	} else if a.Token.Type != "first_party_access_token" && a.Token.Type != "oauth_access_token" {
		return runApprovalError("approval_forbidden")
	}
	return nil
}

// runTaskSnapshot includes all saved execution inputs, but not progress or the
// previous execution. A change must invalidate approval even if still supported.
func runTaskSnapshot(task *models.TransferTask) (models.JSONMap, string, error) {
	if task == nil {
		return nil, "", runApprovalError("approval_request_mismatch")
	}
	snapshot := models.JSONMap{"task_id": task.ID, "name": task.Name, "task_type": task.TaskType, "config": task.Config, "batch_size": task.BatchSize, "schedule": task.Schedule, "enabled": task.Enabled, "auto_scan_metadata": task.AutoScanMetadata, "apply_identity": task.ApplyIdentity}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(body)
	return snapshot, hex.EncodeToString(sum[:]), nil
}

func validateRunTask(task *models.TransferTask) error {
	if task == nil || task.TaskType != "sync" || task.Schedule != "" || task.Enabled || task.AutoScanMetadata || task.Status != models.TaskStatusIdle {
		return runApprovalError("approval_invalid_request")
	}
	config := task.Config
	runtime, _ := config["runtime"].(map[string]interface{})
	load, _ := config["load"].(map[string]interface{})
	source, _ := config["source"].(map[string]interface{})
	target, _ := config["target"].(map[string]interface{})
	policy, _ := target["policy"].(map[string]interface{})
	if runtime["boundary"] != "bounded" || load["mode"] != "snapshot" || source["data_type"] != "table" || source["representation"] != "native" || target["data_type"] != "table" || target["representation"] != "native" || (policy["apply_mode"] != "replace" && policy["apply_mode"] != "append") {
		return runApprovalError("approval_invalid_request")
	}
	choices, err := models.SourceProtectionChoices(config)
	if err != nil || len(choices) > 0 {
		return runApprovalError("approval_invalid_request")
	}
	return validateNewTaskConfig(config, task.BatchSize)
}

func validateRunApprovalTask(task *models.TransferTask, approval *models.RunApproval) error {
	if task.ID != approval.TaskID || task.TenantID != approval.TenantID {
		return runApprovalError("approval_request_mismatch")
	}
	if err := validateRunTask(task); err != nil {
		return err
	}
	_, digest, err := runTaskSnapshot(task)
	if err != nil {
		return err
	}
	if digest != approval.RequestFingerprint {
		return runApprovalError("approval_request_mismatch")
	}
	return nil
}

func (s *TaskService) CreateRunApproval(ctx context.Context, a authorization.AuthContext, taskID uint) (*models.RunApproval, error) {
	if err := runIdentity(a, true, "transfer.task.execute", "transfer.task.read"); err != nil {
		return nil, err
	}
	tenant, _ := strconv.ParseUint(*a.Context.TenantID, 10, 64)
	task, err := s.GetTask(ctx, taskID, uint(tenant))
	if err != nil {
		return nil, err
	}
	if err = validateRunTask(task); err != nil {
		return nil, err
	}
	snapshot, digest, err := runTaskSnapshot(task)
	if err != nil {
		return nil, err
	}
	// Summary is the full user-reviewable execution input, without owner-only ID.
	delete(snapshot, "apply_identity")
	// Do not place arbitrarily large configurations into an Agent interaction.
	body, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	if len(body) > 32*1024 {
		return nil, runApprovalError("approval_invalid_request")
	}
	now := time.Now().UTC()
	approval := &models.RunApproval{ID: uuid.New(), TenantID: uint(tenant), TaskID: taskID, PrincipalID: a.Principal.ID, MembershipID: *a.Context.TenantMembershipID, AuthorizationVersion: a.Authorization.AuthorizationVersion, AgentRunID: a.Delegation.AgentRunID, RequestFingerprint: digest, RequestSummary: snapshot, Status: "pending", RequestedAt: now, ExpiresAt: now.Add(15 * time.Minute)}
	if err = s.db.WithContext(ctx).Create(approval).Error; err != nil {
		return nil, err
	}
	return approval, nil
}

func (s *TaskService) readRunApproval(ctx context.Context, a authorization.AuthContext, id string) (*models.RunApproval, error) {
	key, err := uuid.Parse(id)
	if err != nil {
		return nil, runApprovalError("approval_not_found")
	}
	var item models.RunApproval
	err = s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND principal_id = ?", key, *a.Context.TenantID, a.Principal.ID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, runApprovalError("approval_not_found")
	}
	if err != nil {
		return nil, err
	}
	if item.MembershipID != *a.Context.TenantMembershipID || item.AuthorizationVersion != a.Authorization.AuthorizationVersion {
		return nil, runApprovalError("approval_forbidden")
	}
	return &item, nil
}

func (s *TaskService) GetRunApproval(ctx context.Context, a authorization.AuthContext, id string) (*models.RunApproval, error) {
	if err := runIdentity(a, false, "transfer.task.read"); err != nil {
		return nil, err
	}
	item, err := s.readRunApproval(ctx, a, id)
	if err != nil {
		return nil, err
	}
	// Read-time expiry is projected, without a background writer racing consumption.
	if (item.Status == "pending" || item.Status == "approved") && !item.ExpiresAt.After(time.Now().UTC()) {
		item.Status = "expired"
	}
	return item, nil
}

func checkRunApprovalIdentity(item *models.RunApproval, a authorization.AuthContext) error {
	if item.PrincipalID != a.Principal.ID || item.TenantID == 0 || strconv.FormatUint(uint64(item.TenantID), 10) != *a.Context.TenantID || item.MembershipID != *a.Context.TenantMembershipID || item.AuthorizationVersion != a.Authorization.AuthorizationVersion {
		return runApprovalError("approval_forbidden")
	}
	if a.Token.Type == "delegated_access_token" && item.AgentRunID != a.Delegation.AgentRunID {
		return runApprovalError("approval_forbidden")
	}
	return nil
}

func approvalState(item *models.RunApproval, now time.Time, required string) error {
	if item.Status == "consumed" {
		return runApprovalError("approval_already_consumed")
	}
	if item.Status == "rejected" {
		return runApprovalError("approval_rejected")
	}
	if item.Status == "expired" || !item.ExpiresAt.After(now) {
		return runApprovalError("approval_expired")
	}
	if item.Status != required {
		return runApprovalError("approval_not_approved")
	}
	return nil
}

func approvalClock(tx *gorm.DB) (time.Time, error) {
	if tx.Dialector.Name() != "postgres" {
		return time.Now().UTC(), nil
	}
	var now time.Time
	err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now, err
}

func (s *TaskService) DecideRunApproval(ctx context.Context, a authorization.AuthContext, id string, req models.RunApprovalDecision) (*models.RunApproval, error) {
	if err := runIdentity(a, false, "transfer.task.execute"); err != nil {
		return nil, err
	}
	if req.Decision != "approved" && req.Decision != "rejected" {
		return nil, runApprovalError("approval_invalid_decision")
	}
	snapshot, err := s.readRunApproval(ctx, a, id)
	if err != nil {
		return nil, err
	}
	var item models.RunApproval
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// All mutations lock task before approval, matching execution claim order.
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", snapshot.TaskID, snapshot.TenantID).First(&task).Error; err != nil {
			return commonAPI.ErrNotFound
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", snapshot.ID).Error; err != nil {
			return err
		}
		if err := checkRunApprovalIdentity(&item, a); err != nil {
			return err
		}
		if req.RequestFingerprint != item.RequestFingerprint {
			return runApprovalError("approval_request_mismatch")
		}
		now, err := approvalClock(tx)
		if err != nil {
			return err
		}
		if err = approvalState(&item, now, "pending"); err != nil {
			return err
		}
		// Rejection remains possible if the task has changed.
		if req.Decision == "approved" {
			if err = validateRunApprovalTask(&task, &item); err != nil {
				return err
			}
		}
		item.Status = req.Decision
		item.DecidedAt = &now
		return tx.Save(&item).Error
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *TaskService) StartApprovedTask(ctx context.Context, a authorization.AuthContext, taskID uint, req models.StartTaskRequest) (*models.TaskExecution, error) {
	if err := runIdentity(a, true, "transfer.task.execute", "transfer.task.read"); err != nil {
		return nil, err
	}
	item, err := s.readRunApproval(ctx, a, req.ApprovalID)
	if err != nil {
		return nil, err
	}
	// Identity precedes state to avoid leaking a different AgentRun's approval.
	if err = checkRunApprovalIdentity(item, a); err != nil {
		return nil, err
	}
	if item.TaskID != taskID || item.RequestFingerprint != req.RequestFingerprint {
		return nil, runApprovalError("approval_request_mismatch")
	}
	if err = approvalState(item, time.Now().UTC(), "approved"); err != nil {
		return nil, err
	}
	principal, _ := strconv.ParseUint(a.Principal.ID, 10, 64)
	return s.startTaskWithContext(ctx, taskID, item.TenantID, uint(principal), commonExecution.TriggerTypeManual, commonExecution.ModuleTransfer, nil, nil, item)
}

func consumeRunApproval(expected *models.RunApproval, executionID string) func(*gorm.DB, *models.TransferTask) error {
	return func(tx *gorm.DB, task *models.TransferTask) error {
		var item models.RunApproval
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", expected.ID).Error; err != nil {
			return err
		}
		if item.TenantID != expected.TenantID || item.PrincipalID != expected.PrincipalID || item.MembershipID != expected.MembershipID || item.AuthorizationVersion != expected.AuthorizationVersion || item.AgentRunID != expected.AgentRunID {
			return runApprovalError("approval_forbidden")
		}
		if item.RequestFingerprint != expected.RequestFingerprint {
			return runApprovalError("approval_request_mismatch")
		}
		now, err := approvalClock(tx)
		if err != nil {
			return err
		}
		if err = approvalState(&item, now, "approved"); err != nil {
			return err
		}
		if err = validateRunApprovalTask(task, &item); err != nil {
			return err
		}
		item.Status = "consumed"
		item.ConsumedAt = &now
		item.ExecutionID = &executionID
		return tx.Save(&item).Error
	}
}
