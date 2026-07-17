package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	WorkflowRunToolName = "workflow.run"
	toolApprovalTTL     = 15 * time.Minute
)

type ToolApprovalError struct {
	Code    string
	Message string
}

func (err *ToolApprovalError) Error() string {
	return err.Message
}

func approvalError(code, message string) error {
	return &ToolApprovalError{Code: code, Message: message}
}

type approvalExecution interface {
	prepareContentExecution(
		context.Context,
		string,
		map[string]interface{},
		map[string]interface{},
		uint,
		uint,
		string,
		int,
	) (*preparedContentExecution, error)
	persistPreparedContentExecution(
		context.Context,
		*commonExecution.TaskExecutionRepository,
		*preparedContentExecution,
	) error
	startPreparedContentExecution(*preparedContentExecution)
}

type ToolApprovalService struct {
	db       *gorm.DB
	executor approvalExecution
	now      func() time.Time
}

func NewToolApprovalService(db *gorm.DB, executor *DevExecutor) *ToolApprovalService {
	return &ToolApprovalService{db: db, executor: executor, now: time.Now}
}

func (service *ToolApprovalService) CreateWorkflowRunApproval(
	ctx context.Context,
	authContext commonAuth.AuthorizationContext,
	req models.CreateExecutionRequest,
) (*models.ToolApproval, error) {
	if err := validateDelegatedWorkflowRunContext(authContext); err != nil {
		return nil, err
	}
	if req.ApprovalID != "" || req.RequestFingerprint != "" {
		return nil, approvalError("approval_invalid_request", "首次 workflow.run 不得携带 approval_id 或 request_fingerprint")
	}
	if err := normalizeInitialWorkflowRunRequest(&req); err != nil {
		return nil, err
	}
	if _, err := service.executor.prepareContentExecution(
		ctx,
		req.DevType,
		req.Content,
		req.ExecutionConfig,
		reqTenantID(authContext),
		authContext.UserID,
		req.TriggerType,
		req.Timeout,
	); err != nil {
		return nil, approvalError("approval_invalid_request", err.Error())
	}

	payload, fingerprint, err := canonicalApprovalPayload(req)
	if err != nil {
		return nil, approvalError("approval_invalid_request", "无法规范化 workflow.run 请求")
	}
	now := service.now().UTC()
	approval := &models.ToolApproval{
		UserID:             authContext.UserID,
		TenantID:           reqTenantID(authContext),
		AgentRunID:         strings.TrimSpace(*authContext.AgentRunID),
		ToolCallID:         strings.TrimSpace(*authContext.ToolCallID),
		ToolName:           WorkflowRunToolName,
		RequestFingerprint: fingerprint,
		RequestPayload:     payload,
		RequestSummary:     workflowRunSummary(req),
		Status:             models.ToolApprovalStatusPending,
		RequestedAt:        now,
		ExpiresAt:          now.Add(toolApprovalTTL),
	}
	if err := service.db.WithContext(ctx).Create(approval).Error; err != nil {
		return nil, fmt.Errorf("create tool approval: %w", err)
	}
	return approval, nil
}

func (service *ToolApprovalService) GetApproval(
	ctx context.Context,
	authContext commonAuth.AuthorizationContext,
	approvalID string,
) (*models.ToolApproval, error) {
	if err := validateUserApprovalContext(authContext); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(approvalID)
	if err != nil {
		return nil, approvalError("approval_not_found", "审批不存在")
	}
	var approval models.ToolApproval
	if err := service.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND tenant_id = ?", id, authContext.UserID, reqTenantID(authContext)).
		First(&approval).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, approvalError("approval_not_found", "审批不存在")
		}
		return nil, fmt.Errorf("get tool approval: %w", err)
	}
	if err := service.expireIfNeeded(ctx, &approval); err != nil {
		return nil, fmt.Errorf("expire tool approval: %w", err)
	}
	return &approval, nil
}

func (service *ToolApprovalService) DecideApproval(
	ctx context.Context,
	authContext commonAuth.AuthorizationContext,
	approvalID string,
	decision string,
) (*models.ToolApproval, error) {
	if err := validateUserApprovalContext(authContext); err != nil {
		return nil, err
	}
	if decision != models.ToolApprovalStatusApproved && decision != models.ToolApprovalStatusRejected {
		return nil, approvalError("approval_invalid_decision", "decision 只允许 approved 或 rejected")
	}
	id, err := uuid.Parse(approvalID)
	if err != nil {
		return nil, approvalError("approval_not_found", "审批不存在")
	}

	var decided models.ToolApproval
	var transitionErr error
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ? AND tenant_id = ?", id, authContext.UserID, reqTenantID(authContext)).
			First(&decided).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return approvalError("approval_not_found", "审批不存在")
			}
			return err
		}
		now := service.now().UTC()
		if decided.Status != models.ToolApprovalStatusPending {
			return approvalStateError(decided.Status)
		}
		if !decided.ExpiresAt.After(now) {
			decided.Status = models.ToolApprovalStatusExpired
			if err := tx.Save(&decided).Error; err != nil {
				return err
			}
			transitionErr = approvalError("approval_expired", "审批已过期")
			return nil
		}
		decided.Status = decision
		decided.DecidedAt = &now
		decided.DecidedByUserID = &authContext.UserID
		return tx.Save(&decided).Error
	})
	if err != nil {
		return nil, err
	}
	if transitionErr != nil {
		return nil, transitionErr
	}
	return &decided, nil
}

func (service *ToolApprovalService) ConsumeWorkflowRunApproval(
	ctx context.Context,
	authContext commonAuth.AuthorizationContext,
	approvalID string,
	requestFingerprint string,
) (string, error) {
	if err := validateDelegatedWorkflowRunContext(authContext); err != nil {
		return "", err
	}
	id, err := uuid.Parse(approvalID)
	if err != nil {
		return "", approvalError("approval_not_found", "审批不存在")
	}
	requestFingerprint = strings.ToLower(strings.TrimSpace(requestFingerprint))
	if len(requestFingerprint) != 64 {
		return "", approvalError("approval_request_mismatch", "审批请求指纹不匹配")
	}

	var snapshot models.ToolApproval
	if err := service.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND tenant_id = ?", id, authContext.UserID, reqTenantID(authContext)).
		First(&snapshot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", approvalError("approval_not_found", "审批不存在")
		}
		return "", fmt.Errorf("get tool approval: %w", err)
	}
	if snapshot.AgentRunID != strings.TrimSpace(*authContext.AgentRunID) {
		return "", approvalError("approval_forbidden", "审批不属于当前 AgentRun")
	}
	if snapshot.RequestFingerprint != requestFingerprint {
		return "", approvalError("approval_request_mismatch", "审批请求指纹不匹配")
	}
	req, err := requestFromApproval(snapshot.RequestPayload)
	if err != nil {
		return "", approvalError("approval_invalid_request", "审批中的 workflow.run 请求无效")
	}
	prepared, err := service.executor.prepareContentExecution(
		ctx,
		req.DevType,
		req.Content,
		req.ExecutionConfig,
		snapshot.TenantID,
		snapshot.UserID,
		req.TriggerType,
		req.Timeout,
	)
	if err != nil {
		return "", approvalError("approval_invalid_request", err.Error())
	}

	var transitionErr error
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var approval models.ToolApproval
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ? AND tenant_id = ?", id, authContext.UserID, reqTenantID(authContext)).
			First(&approval).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return approvalError("approval_not_found", "审批不存在")
			}
			return err
		}
		now := service.now().UTC()
		if approval.Status != models.ToolApprovalStatusPending && approval.Status != models.ToolApprovalStatusApproved {
			return approvalStateError(approval.Status)
		}
		if !approval.ExpiresAt.After(now) {
			approval.Status = models.ToolApprovalStatusExpired
			if err := tx.Save(&approval).Error; err != nil {
				return err
			}
			transitionErr = approvalError("approval_expired", "审批已过期")
			return nil
		}
		if approval.AgentRunID != strings.TrimSpace(*authContext.AgentRunID) {
			return approvalError("approval_forbidden", "审批不属于当前 AgentRun")
		}
		if approval.RequestFingerprint != requestFingerprint {
			return approvalError("approval_request_mismatch", "审批请求指纹不匹配")
		}
		if approval.Status == models.ToolApprovalStatusPending {
			return approvalStateError(approval.Status)
		}
		txRepo := commonExecution.NewTaskExecutionRepository(tx)
		if err := service.executor.persistPreparedContentExecution(ctx, txRepo, prepared); err != nil {
			return err
		}
		approval.Status = models.ToolApprovalStatusConsumed
		approval.ConsumedAt = &now
		approval.ExecutionID = &prepared.execution.ExecutionID
		return tx.Save(&approval).Error
	})
	if err != nil {
		return "", err
	}
	if transitionErr != nil {
		return "", transitionErr
	}
	service.executor.startPreparedContentExecution(prepared)
	return prepared.execution.ExecutionID, nil
}

func normalizeInitialWorkflowRunRequest(req *models.CreateExecutionRequest) error {
	if req.DevType != "workflow" {
		return approvalError("approval_invalid_request", "委托 workflow.run 只允许 dev_type=workflow")
	}
	if req.TriggerType == "" {
		req.TriggerType = "manual"
	}
	if req.TriggerType != "manual" {
		return approvalError("approval_invalid_request", "委托 workflow.run 只允许 trigger_type=manual")
	}
	if req.Content == nil || req.ExecutionConfig == nil {
		return approvalError("approval_invalid_request", "workflow.run 必须提供 content 和 execution_config")
	}
	if req.Timeout <= 0 {
		req.Timeout = 300
	}
	return nil
}

func canonicalApprovalPayload(req models.CreateExecutionRequest) (models.DevTaskContent, string, error) {
	payload := models.DevTaskContent{
		"dev_type":         req.DevType,
		"trigger_type":     req.TriggerType,
		"content":          req.Content,
		"execution_config": req.ExecutionConfig,
		"timeout":          req.Timeout,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(encoded)
	return payload, hex.EncodeToString(digest[:]), nil
}

func requestFromApproval(payload models.DevTaskContent) (models.CreateExecutionRequest, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return models.CreateExecutionRequest{}, err
	}
	var req models.CreateExecutionRequest
	if err := json.Unmarshal(encoded, &req); err != nil {
		return models.CreateExecutionRequest{}, err
	}
	return req, normalizeInitialWorkflowRunRequest(&req)
}

func workflowRunSummary(req models.CreateExecutionRequest) models.DevTaskContent {
	summary := models.DevTaskContent{
		"dev_type":     req.DevType,
		"trigger_type": req.TriggerType,
		"timeout":      req.Timeout,
	}
	if engineID := workflowEngineIDFromExecutionConfig(req.ExecutionConfig); engineID > 0 {
		summary["workflow_engine_id"] = engineID
	}
	workflow, _ := req.Content["workflow_definition"].(map[string]interface{})
	if tasks, ok := workflow["tasks"].([]interface{}); ok {
		summary["task_count"] = len(tasks)
	} else if tasks, ok := workflow["tasks"].([]map[string]interface{}); ok {
		summary["task_count"] = len(tasks)
	}
	return summary
}

func validateDelegatedWorkflowRunContext(authContext commonAuth.AuthorizationContext) error {
	if authContext.AuthType != commonAuth.AuthTypeDelegatedAccessToken {
		return approvalError("approval_forbidden", "workflow.run 审批执行只接受受委托访问令牌")
	}
	if authContext.TenantID == nil || *authContext.TenantID == 0 || authContext.UserID == 0 {
		return approvalError("approval_forbidden", "审批身份缺少用户或租户")
	}
	if authContext.AgentRunID == nil || strings.TrimSpace(*authContext.AgentRunID) == "" ||
		authContext.ToolCallID == nil || strings.TrimSpace(*authContext.ToolCallID) == "" {
		return approvalError("approval_forbidden", "审批身份缺少 AgentRun 或 ToolCall 绑定")
	}
	return nil
}

func validateUserApprovalContext(authContext commonAuth.AuthorizationContext) error {
	if authContext.AuthType != "first_party_access_token" && authContext.AuthType != "oauth_access_token" {
		return approvalError("approval_forbidden", "审批只接受用户访问令牌")
	}
	if authContext.TenantID == nil || *authContext.TenantID == 0 || authContext.UserID == 0 {
		return approvalError("approval_forbidden", "审批身份缺少用户或租户")
	}
	return nil
}

func reqTenantID(authContext commonAuth.AuthorizationContext) uint {
	if authContext.TenantID == nil {
		return 0
	}
	return *authContext.TenantID
}

func approvalStateError(status string) error {
	switch status {
	case models.ToolApprovalStatusConsumed:
		return approvalError("approval_already_consumed", "审批已消费")
	case models.ToolApprovalStatusRejected:
		return approvalError("approval_rejected", "审批已拒绝")
	case models.ToolApprovalStatusExpired:
		return approvalError("approval_expired", "审批已过期")
	case models.ToolApprovalStatusPending:
		return approvalError("approval_not_approved", "审批尚未批准")
	default:
		return approvalError("approval_not_approved", "审批状态不允许执行")
	}
}

func (service *ToolApprovalService) expireIfNeeded(ctx context.Context, approval *models.ToolApproval) error {
	if approval.Status != models.ToolApprovalStatusPending && approval.Status != models.ToolApprovalStatusApproved {
		return nil
	}
	if approval.ExpiresAt.After(service.now().UTC()) {
		return nil
	}
	approval.Status = models.ToolApprovalStatusExpired
	return service.db.WithContext(ctx).Save(approval).Error
}
