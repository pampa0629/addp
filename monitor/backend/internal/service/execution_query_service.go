package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
)

const maxExecutionTreeDepth = 8

// ExecutionQueryService 执行查询服务
type ExecutionQueryService struct {
	repo *commonExecution.TaskExecutionRepository
	now  func() time.Time
}

// NewExecutionQueryService 创建执行查询服务
func NewExecutionQueryService(repo *commonExecution.TaskExecutionRepository) *ExecutionQueryService {
	return &ExecutionQueryService{
		repo: repo,
		now:  time.Now,
	}
}

// ExecutionObservation is Monitor's safe projection of a shared execution.
// LeaseToken remains owner-internal and is never part of this DTO.
type ExecutionObservation struct {
	*commonExecution.TaskExecution
	QueueDurationMs int64      `json:"queue_duration_ms"`
	RunDurationMs   int64      `json:"run_duration_ms"`
	LeaseOwner      *string    `json:"lease_owner,omitempty"`
	LeaseExpiresAt  *time.Time `json:"lease_expires_at,omitempty"`
	LeaseState      string     `json:"lease_state"`
	RecoveryReason  string     `json:"recovery_reason,omitempty"`
}

// ListExecutionsRequest 查询请求
type ListExecutionsRequest struct {
	TenantID     int
	Module       string
	TaskType     string
	Source       string
	Status       string
	TriggerType  string
	SourceTaskID *string
	StartDate    *time.Time
	EndDate      *time.Time
	Page         int
	PageSize     int
}

// ListExecutionsResponse 查询响应
type ListExecutionsResponse struct {
	Executions []*ExecutionObservation `json:"executions"`
	Total      int64                   `json:"total"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"page_size"`
}

// ExecutionTreeNode 执行树节点
type ExecutionTreeNode struct {
	Execution *ExecutionObservation `json:"execution"`
	Children  []*ExecutionTreeNode  `json:"children"`
}

// ListExecutions 分页查询执行记录
func (s *ExecutionQueryService) ListExecutions(ctx context.Context, req *ListExecutionsRequest) (*ListExecutionsResponse, error) {
	if req.SourceTaskID != nil && (req.Module == "" || req.TaskType == "") {
		return nil, errors.New("module and task_type are required when source_task_id is provided")
	}

	filter := commonExecution.TaskExecutionFilter{
		TenantID:     req.TenantID,
		Module:       req.Module,
		TaskType:     req.TaskType,
		Source:       req.Source,
		Status:       req.Status,
		TriggerType:  req.TriggerType,
		SourceTaskID: req.SourceTaskID,
		StartDate:    req.StartDate,
		EndDate:      req.EndDate,
		Page:         req.Page,
		PageSize:     req.PageSize,
	}

	executions, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	observed := make([]*ExecutionObservation, 0, len(executions))
	for _, execution := range executions {
		observed = append(observed, observeExecution(execution, s.now().UTC()))
	}
	return &ListExecutionsResponse{
		Executions: observed,
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
	}, nil
}

// GetExecution 获取单条执行记录
func (s *ExecutionQueryService) GetExecution(ctx context.Context, id int64, tenantID int) (*ExecutionObservation, error) {
	execution, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	return observeExecution(execution, s.now().UTC()), nil
}

// GetExecutionByExecutionID 根据全局 execution_id 查询执行记录。
func (s *ExecutionQueryService) GetExecutionByExecutionID(ctx context.Context, executionID string, tenantID int) (*ExecutionObservation, error) {
	execution, err := s.repo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil {
		return nil, err
	}
	return observeExecution(execution, s.now().UTC()), nil
}

// GetExecutionTree 获取执行记录及其子执行树
func (s *ExecutionQueryService) GetExecutionTree(ctx context.Context, id int64, tenantID int) (*ExecutionTreeNode, error) {
	root, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	visited := map[string]struct{}{}
	return s.buildExecutionTree(ctx, root, tenantID, 0, visited, s.now().UTC())
}

// GetExecutionTreeByExecutionID 根据全局 execution_id 获取执行记录及其子执行树。
func (s *ExecutionQueryService) GetExecutionTreeByExecutionID(ctx context.Context, executionID string, tenantID int) (*ExecutionTreeNode, error) {
	root, err := s.repo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil {
		return nil, err
	}
	visited := map[string]struct{}{}
	return s.buildExecutionTree(ctx, root, tenantID, 0, visited, s.now().UTC())
}

func (s *ExecutionQueryService) buildExecutionTree(
	ctx context.Context,
	exec *commonExecution.TaskExecution,
	tenantID int,
	depth int,
	visited map[string]struct{},
	observedAt time.Time,
) (*ExecutionTreeNode, error) {
	if exec == nil {
		return nil, errors.New("execution is nil")
	}
	if _, exists := visited[exec.ExecutionID]; exists {
		return nil, fmt.Errorf("execution tree contains cycle at %s", exec.ExecutionID)
	}
	visited[exec.ExecutionID] = struct{}{}

	node := &ExecutionTreeNode{
		Execution: observeExecution(exec, observedAt),
		Children:  []*ExecutionTreeNode{},
	}
	if depth >= maxExecutionTreeDepth {
		return node, nil
	}

	children, err := s.repo.ListChildrenByParentExecutionID(ctx, exec.ExecutionID, tenantID)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		childNode, err := s.buildExecutionTree(ctx, child, tenantID, depth+1, visited, observedAt)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, childNode)
	}
	return node, nil
}

func observeExecution(execution *commonExecution.TaskExecution, now time.Time) *ExecutionObservation {
	if execution == nil {
		return nil
	}
	queueEnd := now
	if execution.StartedAt != nil {
		queueEnd = execution.StartedAt.UTC()
	} else if execution.CompletedAt != nil {
		queueEnd = execution.CompletedAt.UTC()
	}
	queueDuration := nonNegativeMilliseconds(queueEnd.Sub(execution.CreatedAt.UTC()))

	runDuration := int64(0)
	if execution.ExecutionTimeMs != nil {
		runDuration = *execution.ExecutionTimeMs
	} else if execution.StartedAt != nil {
		runEnd := now
		if execution.CompletedAt != nil {
			runEnd = execution.CompletedAt.UTC()
		}
		runDuration = nonNegativeMilliseconds(runEnd.Sub(execution.StartedAt.UTC()))
	}

	leaseState := "none"
	if execution.Status == commonExecution.ExecutionStatusRunning && execution.ExecutionBoundary == commonExecution.ExecutionBoundaryBounded {
		leaseState = "missing"
		if execution.LeaseExpiresAt != nil {
			leaseState = "active"
			if execution.LeaseExpiresAt.Before(now) {
				leaseState = "expired"
			}
		}
	}

	return &ExecutionObservation{
		TaskExecution: execution, QueueDurationMs: queueDuration, RunDurationMs: runDuration,
		LeaseOwner: execution.LeaseOwner, LeaseExpiresAt: execution.LeaseExpiresAt,
		LeaseState: leaseState, RecoveryReason: executionRecoveryReason(execution),
	}
}

func nonNegativeMilliseconds(duration time.Duration) int64 {
	if duration < 0 {
		return 0
	}
	return duration.Milliseconds()
}

func executionRecoveryReason(execution *commonExecution.TaskExecution) string {
	if execution == nil {
		return ""
	}
	if reason, ok := execution.Metadata["recovery_reason"].(string); ok && strings.TrimSpace(reason) != "" {
		return strings.TrimSpace(reason)
	}
	if code, ok := execution.ErrorDetails["code"].(string); ok && strings.Contains(code, "lease_expired") {
		return code
	}
	if execution.CurrentStep != nil && strings.Contains(strings.ToLower(*execution.CurrentStep), "lease expired") {
		return "lease_expired"
	}
	return ""
}
