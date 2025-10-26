package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"gorm.io/gorm"
)

// ExecutionService 执行记录服务
type ExecutionService struct {
	execRepo *repository.ExecutionRepository
	taskRepo *repository.TaskRepository
	logger   *slog.Logger
}

// NewExecutionService 创建执行服务
func NewExecutionService(db *gorm.DB) *ExecutionService {
	return &ExecutionService{
		execRepo: repository.NewExecutionRepository(db),
		taskRepo: repository.NewTaskRepository(db),
		logger:   logger.With("component", "execution_service"),
	}
}

// GetExecution 获取执行记录
func (s *ExecutionService) GetExecution(ctx context.Context, id, tenantID uint) (*models.TaskExecution, error) {
	execution, err := s.execRepo.GetByID(id)
	if err != nil {
		return nil, err
	}

	// 检查租户权限
	task, err := s.taskRepo.GetByID(execution.TaskID)
	if err != nil {
		return nil, err
	}
	if task.TenantID != tenantID {
		return nil, fmt.Errorf("execution not found or access denied")
	}

	return execution, nil
}

// ListExecutions 列出任务的执行记录
func (s *ExecutionService) ListExecutions(ctx context.Context, taskID, tenantID uint, page, pageSize int) ([]models.TaskExecution, int64, error) {
	// 检查任务权限
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return nil, 0, err
	}
	if task.TenantID != tenantID {
		return nil, 0, fmt.Errorf("task not found or access denied")
	}

	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}

	return s.execRepo.ListByTaskID(taskID, page, pageSize)
}

// ListAllExecutions 列出租户的所有执行记录（跨任务）
func (s *ExecutionService) ListAllExecutions(ctx context.Context, tenantID uint, filters map[string]interface{}, page, pageSize int) ([]models.TaskExecution, int64, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}

	return s.execRepo.ListAllExecutions(tenantID, filters, page, pageSize)
}

// GetLatestExecution 获取任务的最新执行记录
func (s *ExecutionService) GetLatestExecution(ctx context.Context, taskID, tenantID uint) (*models.TaskExecution, error) {
	// 检查任务权限
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return nil, err
	}
	if task.TenantID != tenantID {
		return nil, fmt.Errorf("task not found or access denied")
	}

	return s.execRepo.GetLatestByTaskID(taskID)
}

// GetRunningExecutions 获取所有运行中的执行记录
func (s *ExecutionService) GetRunningExecutions(ctx context.Context) ([]models.TaskExecution, error) {
	return s.execRepo.GetRunningExecutions()
}

// RetryExecution 重试失败的执行
func (s *ExecutionService) RetryExecution(ctx context.Context, id, tenantID, userID uint) (*models.TaskExecution, error) {
	// 获取原执行记录
	oldExecution, err := s.GetExecution(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}

	// 只有失败的执行才能重试
	if oldExecution.Status != models.ExecutionStatusFailed {
		return nil, fmt.Errorf("only failed executions can be retried")
	}

	s.logger.Info("retrying execution", "execution_id", id, "task_id", oldExecution.TaskID)

	// 创建新的执行记录
	newExecution := &models.TaskExecution{
		TaskID:      oldExecution.TaskID,
		Status:      models.ExecutionStatusPending,
		TriggerType: "retry",
		TriggerBy:   &userID,
	}

	if err := s.execRepo.Create(newExecution); err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	// 更新任务状态为 pending
	s.taskRepo.UpdateStatus(oldExecution.TaskID, models.TaskStatusPending)

	// TODO: 将任务提交到队列

	s.logger.Info("execution retry created", "new_execution_id", newExecution.ID)
	return newExecution, nil
}

// CancelExecution 取消正在运行的执行
func (s *ExecutionService) CancelExecution(ctx context.Context, id, tenantID uint) error {
	execution, err := s.GetExecution(ctx, id, tenantID)
	if err != nil {
		return err
	}

	if execution.Status != models.ExecutionStatusRunning {
		return fmt.Errorf("only running executions can be cancelled")
	}

	// TODO: 实现任务取消逻辑（通过 context cancel）

	// 更新执行状态
	if err := s.execRepo.FinishExecution(id, models.ExecutionStatusFailed, "cancelled by user"); err != nil {
		return fmt.Errorf("failed to cancel execution: %w", err)
	}

	task, err := s.taskRepo.GetByID(execution.TaskID)
	if err != nil {
		return fmt.Errorf("failed to load task: %w", err)
	}

	finalStatus := models.TaskStatusStopped
	if task.Schedule != "" {
		finalStatus = models.TaskStatusScheduled
	}

	if err := s.taskRepo.UpdateFields(execution.TaskID, map[string]interface{}{
		"status":                     finalStatus,
		"last_execution_id":          id,
		"last_execution_status":      models.ExecutionStatusFailed,
		"last_execution_finished_at": time.Now(),
	}); err != nil {
		return fmt.Errorf("failed to update task after cancellation: %w", err)
	}

	s.logger.Info("execution cancelled", "execution_id", id)
	return nil
}

// GetExecutionProgress 获取执行进度（实时）
func (s *ExecutionService) GetExecutionProgress(ctx context.Context, id, tenantID uint) (map[string]interface{}, error) {
	execution, err := s.GetExecution(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}

	progress := map[string]interface{}{
		"execution_id":    execution.ID,
		"task_id":         execution.TaskID,
		"status":          execution.Status,
		"start_time":      execution.StartTime,
		"end_time":        execution.EndTime,
		"records_read":    execution.RecordsRead,
		"records_written": execution.RecordsWritten,
		"bytes_read":      execution.BytesRead,
		"bytes_written":   execution.BytesWritten,
		"error_msg":       execution.ErrorMsg,
	}

	// 计算时长
	if execution.EndTime != nil {
		progress["duration"] = execution.Duration().Seconds()
	} else {
		progress["duration"] = execution.Duration().Seconds()
	}

	// 计算 QPS
	if duration := execution.Duration(); duration.Seconds() > 0 {
		progress["qps"] = float64(execution.RecordsWritten) / duration.Seconds()
	} else {
		progress["qps"] = 0.0
	}

	return progress, nil
}

// GetExecutionLogs 获取执行日志
func (s *ExecutionService) GetExecutionLogs(ctx context.Context, id, tenantID uint) (string, error) {
	execution, err := s.GetExecution(ctx, id, tenantID)
	if err != nil {
		return "", err
	}

	return execution.Logs, nil
}

// GetExecutionStatistics 获取执行统计信息
func (s *ExecutionService) GetExecutionStatistics(ctx context.Context, tenantID uint, filters map[string]interface{}) (map[string]interface{}, error) {
	// TODO: 实现执行统计逻辑
	// 目前返回一个空的统计对象
	return map[string]interface{}{
		"total_executions":   0,
		"success_executions": 0,
		"failed_executions":  0,
		"running_executions": 0,
		"total_records":      0,
		"total_bytes":        0,
	}, nil
}
