package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
)

const maxExecutionTreeDepth = 8

// ExecutionQueryService 执行查询服务
type ExecutionQueryService struct {
	repo *commonExecution.TaskExecutionRepository
}

// NewExecutionQueryService 创建执行查询服务
func NewExecutionQueryService(repo *commonExecution.TaskExecutionRepository) *ExecutionQueryService {
	return &ExecutionQueryService{
		repo: repo,
	}
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
	Executions []*commonExecution.TaskExecution `json:"executions"`
	Total      int64                            `json:"total"`
	Page       int                              `json:"page"`
	PageSize   int                              `json:"page_size"`
}

// ExecutionTreeNode 执行树节点
type ExecutionTreeNode struct {
	Execution *commonExecution.TaskExecution `json:"execution"`
	Children  []*ExecutionTreeNode           `json:"children"`
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

	return &ListExecutionsResponse{
		Executions: executions,
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
	}, nil
}

// GetExecution 获取单条执行记录
func (s *ExecutionQueryService) GetExecution(ctx context.Context, id int64, tenantID int) (*commonExecution.TaskExecution, error) {
	return s.repo.GetByID(ctx, id, tenantID)
}

// GetExecutionByExecutionID 根据全局 execution_id 查询执行记录。
func (s *ExecutionQueryService) GetExecutionByExecutionID(ctx context.Context, executionID string, tenantID int) (*commonExecution.TaskExecution, error) {
	return s.repo.GetByExecutionID(ctx, executionID, tenantID)
}

// GetExecutionTree 获取执行记录及其子执行树
func (s *ExecutionQueryService) GetExecutionTree(ctx context.Context, id int64, tenantID int) (*ExecutionTreeNode, error) {
	root, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	visited := map[string]struct{}{}
	return s.buildExecutionTree(ctx, root, tenantID, 0, visited)
}

// GetExecutionTreeByExecutionID 根据全局 execution_id 获取执行记录及其子执行树。
func (s *ExecutionQueryService) GetExecutionTreeByExecutionID(ctx context.Context, executionID string, tenantID int) (*ExecutionTreeNode, error) {
	root, err := s.repo.GetByExecutionID(ctx, executionID, tenantID)
	if err != nil {
		return nil, err
	}
	visited := map[string]struct{}{}
	return s.buildExecutionTree(ctx, root, tenantID, 0, visited)
}

func (s *ExecutionQueryService) buildExecutionTree(
	ctx context.Context,
	exec *commonExecution.TaskExecution,
	tenantID int,
	depth int,
	visited map[string]struct{},
) (*ExecutionTreeNode, error) {
	if exec == nil {
		return nil, errors.New("execution is nil")
	}
	if _, exists := visited[exec.ExecutionID]; exists {
		return nil, fmt.Errorf("execution tree contains cycle at %s", exec.ExecutionID)
	}
	visited[exec.ExecutionID] = struct{}{}

	node := &ExecutionTreeNode{
		Execution: exec,
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
		childNode, err := s.buildExecutionTree(ctx, child, tenantID, depth+1, visited)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, childNode)
	}
	return node, nil
}
