package service

import (
	"context"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ExecutionService 执行记录服务（使用统一执行表）
type ExecutionService struct {
	taskExecutionRepo *commonExecution.TaskExecutionRepository // 统一执行记录仓库
	taskRepo          *repository.TaskRepository
	taskQueue         TaskQueue
	logger            *slog.Logger
}

// NewExecutionService 创建执行服务
func NewExecutionService(db *gorm.DB, taskExecutionRepo *commonExecution.TaskExecutionRepository) *ExecutionService {
	return &ExecutionService{
		taskExecutionRepo: taskExecutionRepo,
		taskRepo:          repository.NewTaskRepository(db),
		logger:            logger.With("component", "execution_service"),
	}
}

// SetTaskQueue 设置执行重试使用的任务队列。
func (s *ExecutionService) SetTaskQueue(taskQueue TaskQueue) {
	s.taskQueue = taskQueue
}

// convertToTransferExecution 将统一执行记录转换为 Transfer API 的执行记录 DTO。
func (s *ExecutionService) convertToTransferExecution(exec *commonExecution.TaskExecution) *models.TaskExecution {
	if exec == nil {
		return nil
	}
	taskID, err := commonExecution.ParseSourceTaskIDUint(exec.SourceTaskID)
	if err != nil {
		taskID = 0
	}

	transferExec := &models.TaskExecution{
		ID:          uint(exec.ID),
		ExecutionID: exec.ExecutionID,
		TaskID:      taskID,
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
		transferExec.Metadata = exec.Metadata
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
	if execution.Module != commonExecution.ModuleTransfer {
		return nil, fmt.Errorf("execution not found or access denied")
	}

	// 检查租户权限（通过 task）
	if execution.SourceTaskID != nil {
		taskID, parseErr := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if parseErr != nil {
			return nil, parseErr
		}
		task, err := s.taskRepo.GetByID(taskID)
		if err != nil {
			return nil, err
		}
		if task.TenantID != tenantID {
			return nil, fmt.Errorf("execution not found or access denied")
		}
	}

	return s.convertToTransferExecution(execution), nil
}

// GetExecutionByExecutionID 按统一 execution_id 查询执行记录。
func (s *ExecutionService) GetExecutionByExecutionID(ctx context.Context, executionID string, tenantID uint) (*models.TaskExecution, error) {
	execution, err := s.taskExecutionRepo.GetByExecutionID(ctx, executionID, int(tenantID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("execution not found")
		}
		return nil, err
	}
	if execution.Module != commonExecution.ModuleTransfer {
		return nil, fmt.Errorf("execution not found or access denied")
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
	sourceTaskID := commonExecution.NewSourceTaskIDFromUint(taskID)
	filter := commonExecution.TaskExecutionFilter{
		TenantID:     int(tenantID),
		Module:       commonExecution.ModuleTransfer,
		TaskType:     commonExecution.TaskTypeSync,
		SourceTaskID: sourceTaskID,
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
	filter := commonExecution.TaskExecutionFilter{
		TenantID: int(tenantID),
		Module:   commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync,
		Page:     page,
		PageSize: pageSize,
	}

	// 应用额外过滤条件
	if status, ok := filters["status"].(string); ok && status != "" {
		filter.Status = status
	}
	if taskID, ok := filters["task_id"].(uint); ok && taskID > 0 {
		filter.SourceTaskID = commonExecution.NewSourceTaskIDFromUint(taskID)
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
	sourceTaskID := commonExecution.NewSourceTaskIDFromUint(taskID)
	filter := commonExecution.TaskExecutionFilter{
		TenantID:     int(tenantID),
		Module:       commonExecution.ModuleTransfer,
		TaskType:     commonExecution.TaskTypeSync,
		SourceTaskID: sourceTaskID,
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
	filter := commonExecution.TaskExecutionFilter{
		TenantID: 0, // 0 表示不过滤租户
		Module:   commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync,
		Status:   commonExecution.ExecutionStatusRunning,
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
	return s.CreateExecutionWithContext(ctx, taskID, triggerType, commonExecution.ModuleTransfer, nil, triggerBy)
}

// CreateExecutionWithContext 创建执行记录，并记录触发来源和父执行。
func (s *ExecutionService) CreateExecutionWithContext(ctx context.Context, taskID uint, triggerType string, source string, parentExecutionID *string, triggerBy *uint) (*models.TaskExecution, error) {
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(source) == "" {
		source = commonExecution.ModuleTransfer
	}

	// 获取任务信息
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	// 创建统一执行记录
	now := time.Now()
	var triggeredByInt *int
	if triggerBy != nil {
		val := int(*triggerBy)
		triggeredByInt = &val
	}

	execution := &commonExecution.TaskExecution{
		TenantID:          int(task.TenantID),
		ExecutionID:       uuid.New().String(),
		Module:            commonExecution.ModuleTransfer,
		TaskType:          commonExecution.TaskTypeSync,
		Source:            source,
		SourceTaskID:      commonExecution.NewSourceTaskIDFromUint(taskID),
		SourceTaskName:    &task.Name,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusPending,
		Progress:          0,
		TriggerType:       normalizedTriggerType,
		TriggeredBy:       triggeredByInt,
		StartedAt:         &now,
		CreatedAt:         now,
		UpdatedAt:         now,
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
		case "metadata":
			// 统一在循环后合并入 metadata JSONB。
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

	// 处理通用 metadata 和 checkpoint 数据：合并入现有 metadata。
	if _, hasMetadata := updates["metadata"]; hasMetadata {
		metadata := execution.Metadata
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		if values, ok := updates["metadata"].(map[string]interface{}); ok {
			for key, value := range values {
				metadata[key] = value
			}
		} else if values, ok := updates["metadata"].(commonModels.JSONMap); ok {
			for key, value := range values {
				metadata[key] = value
			}
		} else {
			return fmt.Errorf("metadata update must be map[string]interface{}")
		}
		unifiedUpdates["metadata"] = metadata
	}
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

	// 获取执行记录
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0)
	if err != nil {
		return err
	}
	if errorDetails, changed := finishErrorDetails(execution.ErrorDetails, status, errorMsg); changed {
		updates["error_details"] = errorDetails
	}

	// 计算执行时间
	if execution.StartedAt != nil {
		duration := now.Sub(*execution.StartedAt)
		executionTimeMs := duration.Milliseconds()
		updates["execution_time_ms"] = executionTimeMs
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, updates)
}

func finishErrorDetails(existing commonModels.JSONMap, status models.ExecutionStatus, errorMsg string) (commonModels.JSONMap, bool) {
	if existing == nil {
		existing = commonModels.JSONMap{}
	}
	next := commonModels.JSONMap{}
	for key, value := range existing {
		next[key] = value
	}
	if errorMsg != "" {
		next["message"] = errorMsg
		return next, true
	}
	if status == models.ExecutionStatusSuccess {
		if _, ok := next["message"]; ok {
			delete(next, "message")
			return next, true
		}
		return next, len(next) > 0
	}
	return next, false
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

	task, err := s.taskRepo.GetByID(oldExecution.TaskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	if task.Status == models.TaskStatusBlocked && planner.IsPostgreSQLCDCTaskConfig(task.Config) {
		return nil, ErrCDCSchemaChangeBlocked
	}
	if mode := taskApplyMode(task); mode != "replace" && mode != "upsert" {
		return nil, fmt.Errorf("retry execution only supports replace snapshot or resumable upsert tasks; got apply_mode %q", mode)
	}

	if s.taskQueue == nil {
		return nil, fmt.Errorf("task queue is not available")
	}
	now := time.Now()
	triggeredBy := int(userID)
	record := &commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: uuid.New().String(), Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), SourceTaskName: &task.Name,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		TriggeredBy: &triggeredBy, ExecutionConfig: task.Config, StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := s.taskRepo.ClaimExecution(ctx, task.ID, tenantID, record, incrementalSourceIdentity(task)); err != nil {
		return nil, fmt.Errorf("claim retry execution: %w", err)
	}
	newExecution := s.convertToTransferExecution(record)

	if err := s.taskQueue.EnqueueExecuteTask(ctx, oldExecution.TaskID, newExecution.ID, tenantID); err != nil {
		if finishErr := s.FinishExecution(ctx, newExecution.ID, models.ExecutionStatusFailed, err.Error()); finishErr != nil {
			s.logger.Warn("failed to mark retry execution as failed after enqueue failure", "error", finishErr, "execution_id", newExecution.ID)
		}
		if updateErr := s.taskRepo.UpdateFields(oldExecution.TaskID, map[string]interface{}{
			"status":   models.TaskStatusIdle,
			"progress": 0,
		}); updateErr != nil {
			s.logger.Warn("failed to rollback task state after retry enqueue failure", "error", updateErr, "task_id", oldExecution.TaskID)
		}
		return nil, fmt.Errorf("failed to enqueue retry execution: %w", err)
	}

	s.logger.Info("execution retry enqueued", "old_execution_id", id, "new_execution_id", newExecution.ID, "task_id", oldExecution.TaskID)
	return newExecution, nil
}

func taskApplyMode(task *models.TransferTask) string {
	if task == nil {
		return ""
	}
	target, ok := task.Config["target"].(map[string]interface{})
	if !ok {
		return ""
	}
	policy, ok := target["policy"].(map[string]interface{})
	if !ok {
		return ""
	}
	mode, _ := policy["apply_mode"].(string)
	return strings.ToLower(strings.TrimSpace(mode))
}

// GetExecutionProgress 获取执行进度（实时）
func (s *ExecutionService) GetExecutionProgress(ctx context.Context, id, tenantID uint) (map[string]interface{}, error) {
	execution, err := s.GetExecution(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}

	progress := map[string]interface{}{
		"execution_id":    execution.ExecutionID,
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
	stats, err := s.taskExecutionRepo.GetStatistics(ctx, commonExecution.TaskExecutionFilter{
		TenantID: int(tenantID),
		Module:   commonExecution.ModuleTransfer,
	})
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
