package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"gorm.io/gorm"
)

// GISExecutionService GIS 执行服务
type GISExecutionService struct {
	repo                   *repository.GISExecutionRepository
	taskRepo               *repository.SpatialTaskRepository
	spatialWorkflowService *SpatialWorkflowService
	db                     *gorm.DB
}

// NewGISExecutionService 创建 GIS 执行服务
func NewGISExecutionService(
	repo *repository.GISExecutionRepository,
	taskRepo *repository.SpatialTaskRepository,
	spatialWorkflowService *SpatialWorkflowService,
	db *gorm.DB,
) *GISExecutionService {
	return &GISExecutionService{
		repo:                   repo,
		taskRepo:               taskRepo,
		spatialWorkflowService: spatialWorkflowService,
		db:                     db,
	}
}

// CreateExecution 创建执行记录
func (s *GISExecutionService) CreateExecution(
	taskID uint,
	inputs map[string]interface{},
	triggerType string,
	triggerBy uint,
	tenantID uint,
) (*models.GISExecution, error) {
	// 获取任务信息
	task, err := s.taskRepo.GetByID(taskID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	// 创建执行记录
	execution := &models.GISExecution{
		TaskID:      &task.ID,
		TaskName:    task.Name,
		Status:      "pending",
		Inputs:      inputs,
		TriggerType: triggerType,
		TriggerBy:   &triggerBy,
		TenantID:    tenantID,
		StartedAt:   time.Now(),
	}

	if err := s.repo.Create(execution); err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	return execution, nil
}

// ExecuteAsync 异步执行 GIS 任务
func (s *GISExecutionService) ExecuteAsync(ctx context.Context, executionID uint, tenantID uint) {
	go func() {
		// 独立的 context，避免受调用方取消影响
		execCtx := context.Background()

		// 获取执行记录
		execution, err := s.repo.GetByID(executionID, tenantID)
		if err != nil {
			return
		}

		// 获取任务定义
		if execution.TaskID == nil {
			execution.Status = "failed"
			execution.ErrorMessage = "task_id is null"
			execution.CompletedAt = timePtr(time.Now())
			s.repo.Update(execution)
			return
		}

		task, err := s.taskRepo.GetByID(*execution.TaskID, tenantID)
		if err != nil {
			execution.Status = "failed"
			execution.ErrorMessage = fmt.Sprintf("task not found: %v", err)
			execution.CompletedAt = timePtr(time.Now())
			s.repo.Update(execution)
			return
		}

		// 更新状态为 running
		execution.Status = "running"
		execution.Logs = "[INFO] Starting workflow execution...\n"
		if err := s.repo.Update(execution); err != nil {
			return
		}

		// 执行工作流（直接使用 task.WorkflowDef，它已经是 map[string]interface{} 格式）
		startTime := time.Now()
		execution.Logs += fmt.Sprintf("[INFO] %s Calling GeoPandas Engine...\n", time.Now().Format("2006-01-02 15:04:05"))

		resp, err := s.spatialWorkflowService.ExecuteWorkflow(execCtx, task.WorkflowDef, execution.Inputs)

		executionTimeMs := int(time.Since(startTime).Milliseconds())
		execution.ExecutionTimeMs = &executionTimeMs

		if err != nil {
			// 执行失败
			execution.Status = "failed"
			execution.ErrorMessage = err.Error()
			execution.Logs += fmt.Sprintf("[ERROR] %s Execution failed: %v\n", time.Now().Format("2006-01-02 15:04:05"), err)
			execution.CompletedAt = timePtr(time.Now())
			s.repo.Update(execution)
			return
		}

		// 解析结果并保存到 PostGIS
		if resp.Status == "success" {
			execution.Logs += fmt.Sprintf("[INFO] %s Workflow execution completed successfully\n", time.Now().Format("2006-01-02 15:04:05"))

			// 保存结果到数据库
			resultTable := fmt.Sprintf("spatial_execution_results_%d", executionID)
			execution.ResultTable = resultTable

			// TODO: 实现结果保存逻辑
			// 1. 解析 resp.FinalResult (GeoJSON 字符串)
			// 2. 插入到 develop.spatial_execution_results 表
			// 3. 更新 result_count

			execution.Logs += fmt.Sprintf("[INFO] %s Results saved to table: %s\n", time.Now().Format("2006-01-02 15:04:05"), resultTable)

			// 临时设置结果数量为 0
			resultCount := 0
			execution.ResultCount = &resultCount

			execution.Status = "success"
		} else {
			execution.Status = "failed"
			execution.ErrorMessage = resp.Error
			execution.Logs += fmt.Sprintf("[ERROR] %s Workflow failed: %s\n", time.Now().Format("2006-01-02 15:04:05"), resp.Error)
		}

		execution.CompletedAt = timePtr(time.Now())
		execution.Logs += fmt.Sprintf("[INFO] %s Execution completed (duration: %d ms)\n",
			time.Now().Format("2006-01-02 15:04:05"), executionTimeMs)

		s.repo.Update(execution)

		// 更新任务的最后执行信息
		if task != nil {
			task.LastExecutionStatus = execution.Status
			task.LastExecutionStartedAt = &execution.StartedAt
			task.LastExecutionFinishedAt = execution.CompletedAt
			s.taskRepo.Update(task)
		}
	}()
}

// GetExecution 获取执行详情
func (s *GISExecutionService) GetExecution(id uint, tenantID uint) (*models.GISExecutionResponse, error) {
	return s.repo.GetByIDWithTask(id, tenantID)
}

// ListExecutions 查询执行列表
func (s *GISExecutionService) ListExecutions(req *models.ListExecutionsRequest, tenantID uint) (*models.ListExecutionsResponse, error) {
	executions, total, err := s.repo.List(req, tenantID)
	if err != nil {
		return nil, err
	}

	return &models.ListExecutionsResponse{
		Executions: executions,
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
	}, nil
}

// GetExecutionLogs 获取执行日志
func (s *GISExecutionService) GetExecutionLogs(id uint, tenantID uint) (string, error) {
	execution, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return "", err
	}
	return execution.Logs, nil
}

// RetryExecution 重试失败的执行
func (s *GISExecutionService) RetryExecution(id uint, triggerBy uint, tenantID uint) (*models.GISExecution, error) {
	// 获取原执行记录
	original, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("execution not found: %w", err)
	}

	if original.TaskID == nil {
		return nil, fmt.Errorf("cannot retry: task_id is null")
	}

	// 创建新执行（复用输入参数）
	newExecution, err := s.CreateExecution(
		*original.TaskID,
		original.Inputs,
		"retry",
		triggerBy,
		tenantID,
	)
	if err != nil {
		return nil, err
	}

	// 异步执行
	s.ExecuteAsync(context.Background(), newExecution.ID, tenantID)

	return newExecution, nil
}

// CancelExecution 取消运行中的执行
func (s *GISExecutionService) CancelExecution(id uint, tenantID uint) error {
	execution, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return err
	}

	if execution.Status != "running" && execution.Status != "pending" {
		return fmt.Errorf("cannot cancel execution with status: %s", execution.Status)
	}

	execution.Status = "cancelled"
	execution.ErrorMessage = "Execution cancelled by user"
	execution.CompletedAt = timePtr(time.Now())
	execution.Logs += fmt.Sprintf("[INFO] %s Execution cancelled by user\n", time.Now().Format("2006-01-02 15:04:05"))

	return s.repo.Update(execution)
}

// DeleteExecution 删除执行记录
func (s *GISExecutionService) DeleteExecution(id uint, tenantID uint) error {
	return s.repo.DeleteByID(id, tenantID)
}

// GetExecutionStatistics 获取执行统计信息
func (s *GISExecutionService) GetExecutionStatistics(tenantID uint) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// 统计各状态的数量
	for _, status := range []string{"pending", "running", "success", "failed", "timeout"} {
		count, err := s.repo.CountByStatus(tenantID, status)
		if err != nil {
			return nil, err
		}
		stats[status] = count
	}

	return stats, nil
}

// timePtr 辅助函数：返回时间指针
func timePtr(t time.Time) *time.Time {
	return &t
}
