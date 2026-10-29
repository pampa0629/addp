package service

import (
	"context"
	"errors"
	"fmt"
	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
)

const maxExecutionTreeDepth = 8

func (s *ExecutionQueryService) GetExecutionEvents(ctx context.Context, executionID string, tenantID int, after int64, limit int) (*commonExecution.EventPage, error) {
	return s.repo.ListEvents(ctx, executionID, tenantID, after, limit)
}

// ExecutionQueryService 执行查询服务
type ExecutionQueryService struct {
	repo         *commonExecution.TaskExecutionRepository
	now          func() time.Time
	readResolver commonclient.ExecutionReadResolver
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
	*commonExecution.Observation
	CleanupAdmission *commonExecution.CleanupAdmissionView `json:"cleanup_admission,omitempty"`
	QueueDurationMs  int64                                 `json:"queue_duration_ms"`
	RunDurationMs    int64                                 `json:"run_duration_ms"`
	LeaseOwner       *string                               `json:"lease_owner,omitempty"`
	LeaseExpiresAt   *time.Time                            `json:"lease_expires_at,omitempty"`
	LeaseState       string                                `json:"lease_state"`
	RecoveryReason   string                                `json:"recovery_reason,omitempty"`
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
	Truncated bool                  `json:"truncated"`
}

// ListExecutions 分页查询执行记录
func (s *ExecutionQueryService) readListExecutions(ctx context.Context, req *ListExecutionsRequest) (*ListExecutionsResponse, error) {
	normalized := *req
	req = &normalized
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
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
		view, err := s.observe(ctx, execution, s.now().UTC())
		if err != nil {
			return nil, err
		}
		observed = append(observed, view)
	}
	return &ListExecutionsResponse{
		Executions: observed,
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
	}, nil
}

// GetExecution 获取单条执行记录
func (s *ExecutionQueryService) readGetExecution(ctx context.Context, id int64, tenantID int) (*ExecutionObservation, error) {
	execution, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	return s.observe(ctx, execution, s.now().UTC())
}

// GetExecutionByExecutionID 根据全局 execution_id 查询执行记录。
func (s *ExecutionQueryService) readGetExecutionByExecutionID(ctx context.Context, executionID string, tenantID int) (*ExecutionObservation, error) {
	execution, err := s.repo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil {
		return nil, err
	}
	return s.observe(ctx, execution, s.now().UTC())
}

// GetExecutionTree 获取执行记录及其子执行树
func (s *ExecutionQueryService) readGetExecutionTree(ctx context.Context, id int64, tenantID int) (*ExecutionTreeNode, error) {
	root, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	visited := map[string]struct{}{}
	return s.buildExecutionTree(ctx, root, tenantID, 0, visited, s.now().UTC())
}

// GetExecutionTreeByExecutionID 根据全局 execution_id 获取执行记录及其子执行树。
func (s *ExecutionQueryService) readGetExecutionTreeByExecutionID(ctx context.Context, executionID string, tenantID int) (*ExecutionTreeNode, error) {
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

	view, err := s.observe(ctx, exec, observedAt)
	if err != nil {
		return nil, err
	}
	node := &ExecutionTreeNode{
		Execution: view,
		Children:  []*ExecutionTreeNode{},
	}
	limit := 201 - len(visited)
	if depth >= maxExecutionTreeDepth {
		limit = 1
	}
	children, err := s.repo.ListChildrenByParentExecutionID(ctx, exec.ExecutionID, tenantID, limit)
	if err != nil {
		return nil, err
	}
	if depth >= maxExecutionTreeDepth {
		node.Truncated = len(children) > 0
		return node, nil
	}
	for _, child := range children {
		if len(visited) >= 200 {
			node.Truncated = true
			break
		}
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

	var leaseOwner *string
	if execution.LeaseOwner != nil {
		value := commonExecution.SafeDiagnosticText(*execution.LeaseOwner)
		leaseOwner = &value
	}
	return &ExecutionObservation{
		Observation: commonExecution.Observe(execution), QueueDurationMs: queueDuration, RunDurationMs: runDuration,
		LeaseOwner: leaseOwner, LeaseExpiresAt: execution.LeaseExpiresAt,
		LeaseState: leaseState, RecoveryReason: commonExecution.SafeDiagnosticText(executionRecoveryReason(execution)),
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

// Each response uses one short read-only snapshot. No Owner call happens here.
func executionSnapshot[T any](s *ExecutionQueryService, ctx context.Context, tenantID int, module string, read func(*ExecutionQueryService) (T, error)) (value T, err error) {
	err = s.repo.ReadSnapshot(ctx, func(repo *commonExecution.TaskExecutionRepository, now time.Time) error {
		if scopes, checked := commonExecution.ReadScopesFromContext(ctx); checked {
			owners, e := repo.ListOwnerModules(ctx, tenantID)
			if e != nil {
				return e
			}
			known := map[string]bool{}
			for _, scope := range scopes {
				if scope.TenantID == tenantID {
					known[scope.Module] = true
				}
			}
			for _, owner := range owners {
				if (module == "" || module == owner) && !known[owner] {
					return commonclient.ErrExecutionOwnerUnavailable
				}
			}
		}
		snapshot := &ExecutionQueryService{repo: repo, now: func() time.Time { return now }}
		var e error
		value, e = read(snapshot)
		return e
	})
	return
}
func (s *ExecutionQueryService) ListExecutions(ctx context.Context, req *ListExecutionsRequest) (*ListExecutionsResponse, error) {
	return executionSnapshot(s, ctx, req.TenantID, req.Module, func(q *ExecutionQueryService) (*ListExecutionsResponse, error) { return q.readListExecutions(ctx, req) })
}
func (s *ExecutionQueryService) GetExecution(ctx context.Context, id int64, tenantID int) (*ExecutionObservation, error) {
	return executionSnapshot(s, ctx, tenantID, "", func(q *ExecutionQueryService) (*ExecutionObservation, error) {
		return q.readGetExecution(ctx, id, tenantID)
	})
}
func (s *ExecutionQueryService) GetExecutionByExecutionID(ctx context.Context, id string, tenantID int) (*ExecutionObservation, error) {
	return executionSnapshot(s, ctx, tenantID, "", func(q *ExecutionQueryService) (*ExecutionObservation, error) {
		return q.readGetExecutionByExecutionID(ctx, id, tenantID)
	})
}
func (s *ExecutionQueryService) GetExecutionTree(ctx context.Context, id int64, tenantID int) (*ExecutionTreeNode, error) {
	return executionSnapshot(s, ctx, tenantID, "", func(q *ExecutionQueryService) (*ExecutionTreeNode, error) {
		return q.readGetExecutionTree(ctx, id, tenantID)
	})
}
func (s *ExecutionQueryService) GetExecutionTreeByExecutionID(ctx context.Context, id string, tenantID int) (*ExecutionTreeNode, error) {
	return executionSnapshot(s, ctx, tenantID, "", func(q *ExecutionQueryService) (*ExecutionTreeNode, error) {
		return q.readGetExecutionTreeByExecutionID(ctx, id, tenantID)
	})
}
func (s *ExecutionQueryService) observe(ctx context.Context, item *commonExecution.TaskExecution, at time.Time) (*ExecutionObservation, error) {
	view := observeExecution(item, at)
	if item.TaskType != commonExecution.TaskTypeCleanupExecutor || item.ParentExecutionID == nil {
		return view, nil
	}
	parent, err := s.repo.GetByExecutionID(ctx, *item.ParentExecutionID, item.TenantID)
	if errors.Is(err, commonapi.ErrNotFound) {
		parent = nil
	} else if err != nil {
		return nil, err
	}
	view.CleanupAdmission = commonExecution.ProjectCleanupAdmission(*item, parent, at)
	return view, nil
}
