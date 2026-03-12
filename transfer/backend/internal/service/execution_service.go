package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ExecutionService 执行记录服务（使用统一执行表）
type ExecutionService struct {
	taskExecutionRepo *commonRepo.TaskExecutionRepository // 统一执行记录仓库
	taskRepo          *repository.TaskRepository
	logger            *slog.Logger
}

// NewExecutionService 创建执行服务
func NewExecutionService(db *gorm.DB, taskExecutionRepo *commonRepo.TaskExecutionRepository) *ExecutionService {
	return &ExecutionService{
		taskExecutionRepo: taskExecutionRepo,
		taskRepo:          repository.NewTaskRepository(db),
		logger:            logger.With("component", "execution_service"),
	}
}

// convertToTransferExecution 将统一执行记录转换为 Transfer 执行记录格式（API 兼容层）
func (s *ExecutionService) convertToTransferExecution(exec *commonModels.TaskExecution) *models.TaskExecution {
	if exec == nil {
		return nil
	}

	transferExec := &models.TaskExecution{
		ID:          uint(exec.ID),
		TaskID:      uint(*exec.SourceTaskID),
		Status:      models.ExecutionStatus(exec.Status),
		TriggerType: exec.TriggerType,
	}

	// 安全转换指针字段
	if exec.RecordsRead != nil {
		transferExec.RecordsRead = *exec.RecordsRead
	}
	if exec.RecordsWritten != nil {
		transferExec.RecordsWritten = *exec.RecordsWritten
	}
	if exec.BytesRead != nil {
		transferExec.BytesRead = *exec.BytesRead
	}
	if exec.BytesWritten != nil {
		transferExec.BytesWritten = *exec.BytesWritten
	}
	// checkpoint 数据存在 metadata JSONB 中
	if exec.Metadata != nil {
		if offset, ok := exec.Metadata["checkpoint_offset"].(float64); ok {
			v := int64(offset)
			transferExec.CheckpointOffset = v
		}
		if state, ok := exec.Metadata["checkpoint_state"].(map[string]interface{}); ok {
			transferExec.CheckpointState = state
		}
	}

	// 转换时间字段
	if exec.StartedAt != nil {
		transferExec.StartTime = models.LocalTime{Time: *exec.StartedAt}
	}
	if exec.CompletedAt != nil {
		transferExec.EndTime = &models.LocalTime{Time: *exec.CompletedAt}
	}

	// 转换触发人
	if exec.TriggeredBy != nil {
		triggerBy := uint(*exec.TriggeredBy)
		transferExec.TriggerBy = &triggerBy
	}

	// 从 error_details 提取错误信息
	if exec.ErrorDetails != nil {
		if errMsg, ok := exec.ErrorDetails["message"].(string); ok {
			transferExec.ErrorMsg = errMsg
		}
		// 如果有 logs 字段，也提取出来
		if logs, ok := exec.ErrorDetails["logs"].(string); ok {
			transferExec.Logs = logs
		}
	}

	return transferExec
}

// GetExecution 获取执行记录
func (s *ExecutionService) GetExecution(ctx context.Context, id, tenantID uint) (*models.TaskExecution, error) {
	// 从统一表获取执行记录
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), int(tenantID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("execution not found")
		}
		return nil, err
	}

	// 验证是否为 transfer 模块的执行
	if execution.Module != commonModels.ModuleTransfer {
		return nil, fmt.Errorf("execution not found or access denied")
	}

	// 检查租户权限（通过 task）
	if execution.SourceTaskID != nil {
		task, err := s.taskRepo.GetByID(uint(*execution.SourceTaskID))
		if err != nil {
			return nil, err
		}
		if task.TenantID != tenantID {
			return nil, fmt.Errorf("execution not found or access denied")
		}
	}

	return s.convertToTransferExecution(execution), nil
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

	// 从统一表查询
	taskIDInt := int(taskID)
	filter := commonRepo.TaskExecutionFilter{
		TenantID:     int(tenantID),
		Module:       commonModels.ModuleTransfer,
		SourceTaskID: &taskIDInt,
		Page:         page,
		PageSize:     pageSize,
	}

	executions, total, err := s.taskExecutionRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// 转换为 Transfer 格式
	result := make([]models.TaskExecution, 0, len(executions))
	for _, exec := range executions {
		result = append(result, *s.convertToTransferExecution(exec))
	}

	return result, total, nil
}

// ListAllExecutions 列出租户的所有执行记录（跨任务）
func (s *ExecutionService) ListAllExecutions(ctx context.Context, tenantID uint, filters map[string]interface{}, page, pageSize int) ([]models.TaskExecution, int64, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}

	// 构建过滤器
	filter := commonRepo.TaskExecutionFilter{
		TenantID: int(tenantID),
		Module:   commonModels.ModuleTransfer,
		Page:     page,
		PageSize: pageSize,
	}

	// 应用额外过滤条件
	if status, ok := filters["status"].(string); ok && status != "" {
		filter.Status = status
	}
	if taskID, ok := filters["task_id"].(uint); ok && taskID > 0 {
		taskIDInt := int(taskID)
		filter.SourceTaskID = &taskIDInt
	}

	executions, total, err := s.taskExecutionRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// 转换为 Transfer 格式
	result := make([]models.TaskExecution, 0, len(executions))
	for _, exec := range executions {
		result = append(result, *s.convertToTransferExecution(exec))
	}

	return result, total, nil
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

	// 从统一表查询最新记录
	taskIDInt := int(taskID)
	filter := commonRepo.TaskExecutionFilter{
		TenantID:     int(tenantID),
		Module:       commonModels.ModuleTransfer,
		SourceTaskID: &taskIDInt,
		Page:         1,
		PageSize:     1,
	}

	executions, _, err := s.taskExecutionRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(executions) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return s.convertToTransferExecution(executions[0]), nil
}

// GetRunningExecutions 获取所有运行中的执行记录
func (s *ExecutionService) GetRunningExecutions(ctx context.Context) ([]models.TaskExecution, error) {
	// 查询所有租户的运行中执行（这里不限制 tenantID，返回所有）
	filter := commonRepo.TaskExecutionFilter{
		TenantID: 0, // 0 表示不过滤租户
		Module:   commonModels.ModuleTransfer,
		Status:   commonModels.ExecutionStatusRunning,
		Page:     1,
		PageSize: 1000, // 假设最多1000个运行中任务
	}

	executions, _, err := s.taskExecutionRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// 转换为 Transfer 格式
	result := make([]models.TaskExecution, 0, len(executions))
	for _, exec := range executions {
		result = append(result, *s.convertToTransferExecution(exec))
	}

	return result, nil
}

// CreateExecution 创建执行记录
func (s *ExecutionService) CreateExecution(ctx context.Context, taskID uint, triggerType string, triggerBy *uint) (*models.TaskExecution, error) {
	// 获取任务信息
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	// 创建统一执行记录
	now := time.Now()
	taskIDInt := int(taskID)

	var triggeredByInt *int
	if triggerBy != nil {
		val := int(*triggerBy)
		triggeredByInt = &val
	}

	execution := &commonModels.TaskExecution{
		TenantID:       int(task.TenantID),
		ExecutionID:    uuid.New().String(),
		Module:         commonModels.ModuleTransfer,
		TaskType:       "transfer", // Transfer 模块的执行类型统一为 transfer
		SourceTaskID:   &taskIDInt,
		SourceTaskName: &task.Name,
		Status:         commonModels.ExecutionStatusPending,
		Progress:       0,
		TriggerType:    triggerType,
		TriggeredBy:    triggeredByInt,
		StartedAt:      &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	s.logger.Info("execution created", "execution_id", execution.ExecutionID, "task_id", taskID)
	return s.convertToTransferExecution(execution), nil
}

// UpdateExecution 更新执行记录
func (s *ExecutionService) UpdateExecution(ctx context.Context, id uint, updates map[string]interface{}) error {
	// 需要将 Transfer 的字段映射到统一表字段
	unifiedUpdates := make(map[string]interface{})

	for key, value := range updates {
		switch key {
		case "status":
			unifiedUpdates["status"] = value
		case "records_read":
			unifiedUpdates["records_read"] = value
		case "records_written":
			unifiedUpdates["records_written"] = value
		case "bytes_read":
			unifiedUpdates["bytes_read"] = value
		case "bytes_written":
			unifiedUpdates["bytes_written"] = value
		case "checkpoint_offset", "checkpoint_state":
			// checkpoint 数据存入 metadata JSONB，需先获取当前值再合并
			// 此处仅标记需要更新 metadata，统一在循环后处理
		case "error_msg":
			// 错误信息存入 error_details
			unifiedUpdates["error_details"] = commonModels.JSONMap{
				"message": value,
			}
		case "end_time":
			unifiedUpdates["completed_at"] = value
		}
	}

	// 获取执行记录以获取 execution_id 和 tenant_id
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0) // tenant_id 传 0 不过滤
	if err != nil {
		return err
	}

	// 处理 checkpoint 数据：合并入现有 metadata
	if _, hasOffset := updates["checkpoint_offset"]; hasOffset {
		metadata := execution.Metadata
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["checkpoint_offset"] = updates["checkpoint_offset"]
		if state, hasState := updates["checkpoint_state"]; hasState {
			metadata["checkpoint_state"] = state
		}
		unifiedUpdates["metadata"] = metadata
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, unifiedUpdates)
}

// FinishExecution 完成执行（设置结束时间和状态）
func (s *ExecutionService) FinishExecution(ctx context.Context, id uint, status models.ExecutionStatus, errorMsg string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":       string(status),
		"completed_at": now,
		"progress":     100,
	}

	if errorMsg != "" {
		updates["error_details"] = commonModels.JSONMap{
			"message": errorMsg,
		}
	}

	// 获取执行记录
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0)
	if err != nil {
		return err
	}

	// 计算执行时间
	if execution.StartedAt != nil {
		duration := now.Sub(*execution.StartedAt)
		executionTimeMs := duration.Milliseconds()
		updates["execution_time_ms"] = executionTimeMs
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, updates)
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
	newExecution, err := s.CreateExecution(ctx, oldExecution.TaskID, "retry", &userID)
	if err != nil {
		return nil, err
	}

	// 更新任务状态为 running
	s.taskRepo.UpdateStatus(oldExecution.TaskID, models.TaskStatusRunning)

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

	// 更新执行状态
	if err := s.FinishExecution(ctx, id, models.ExecutionStatusFailed, "cancelled by user"); err != nil {
		return fmt.Errorf("failed to cancel execution: %w", err)
	}

	// 取消执行后，恢复任务为空闲状态
	if err := s.taskRepo.UpdateFields(execution.TaskID, map[string]interface{}{
		"status":   models.TaskStatusIdle,
		"progress": 0,
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
	progress["duration"] = execution.Duration().Seconds()

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
	// 使用统一仓库的统计功能
	stats, err := s.taskExecutionRepo.GetStatistics(ctx, int(tenantID), commonModels.ModuleTransfer, nil, nil)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_executions":   stats.Total,
		"success_executions": stats.SuccessCount,
		"failed_executions":  stats.FailedCount,
		"running_executions": stats.RunningCount,
		"success_rate":       stats.SuccessRate,
		"avg_execution_time": stats.AvgExecutionTimeMs,
	}, nil
}

// AppendLog 追加日志到执行记录（用于实时日志更新）
func (s *ExecutionService) AppendLog(ctx context.Context, id uint, logLine string) error {
	// 获取执行记录
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0) // tenant_id 传 0 不过滤
	if err != nil {
		return err
	}

	// 追加日志到 error_details.logs 字段
	errorDetails := execution.ErrorDetails
	if errorDetails == nil {
		errorDetails = commonModels.JSONMap{}
	}

	existingLogs := ""
	if logs, ok := errorDetails["logs"].(string); ok {
		existingLogs = logs
	}

	errorDetails["logs"] = existingLogs + logLine + "\n"

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, map[string]interface{}{
		"error_details": errorDetails,
	})
}

// UpdateMetrics 更新执行指标（用于实时指标更新）
func (s *ExecutionService) UpdateMetrics(ctx context.Context, id uint, metrics map[string]interface{}) error {
	// 获取执行记录
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0)
	if err != nil {
		return err
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, metrics)
}

// UpdateStatus 更新执行状态（用于状态变更）
func (s *ExecutionService) UpdateStatus(ctx context.Context, id uint, status models.ExecutionStatus) error {
	// 获取执行记录
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0)
	if err != nil {
		return err
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, map[string]interface{}{
		"status": string(status),
	})
}
