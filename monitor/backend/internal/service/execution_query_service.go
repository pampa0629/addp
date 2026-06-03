package service

import (
	"context"
	"time"

	commonExecution "github.com/addp/common/execution"
)

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
	Status       string
	TriggerType  string
	SourceTaskID *int
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

// ListExecutions 分页查询执行记录
func (s *ExecutionQueryService) ListExecutions(ctx context.Context, req *ListExecutionsRequest) (*ListExecutionsResponse, error) {
	filter := commonExecution.TaskExecutionFilter{
		TenantID:     req.TenantID,
		Module:       req.Module,
		TaskType:     req.TaskType,
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
