package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TaskQueue 任务队列接口（避免循环依赖）
type TaskQueue interface {
	EnqueueExecuteTask(ctx context.Context, taskID, executionID, tenantID uint) error
	Close() error
}

// TaskService 任务服务
type TaskService struct {
	db              *gorm.DB
	taskRepo        *repository.TaskRepository
	execRepo        *repository.ExecutionRepository
	mappingRepo     *repository.MappingRepository
	executionEngine *ExecutionEngineService
	cfg             *config.Config
	taskQueue       TaskQueue
	logger          *slog.Logger
}

// NewTaskService 创建任务服务
func NewTaskService(
	db *gorm.DB,
	executionEngine *ExecutionEngineService,
	cfg *config.Config,
	taskQueue TaskQueue,
) *TaskService {
	return &TaskService{
		db:              db,
		taskRepo:        repository.NewTaskRepository(db),
		execRepo:        repository.NewExecutionRepository(db),
		mappingRepo:     repository.NewMappingRepository(db),
		executionEngine: executionEngine,
		taskQueue:       taskQueue,
		cfg:             cfg,
		logger:          logger.With("component", "task_service"),
	}
}

// CreateTask 创建任务
func (s *TaskService) CreateTask(ctx context.Context, req *models.CreateTaskRequest, tenantID, userID uint) (*models.Task, error) {
	s.logger.Info("creating task", "name", req.Name, "type", req.Type, "tenant_id", tenantID)

	// 构建任务对象
	task := &models.Task{
		Name:        req.Name,
		Description: req.Description,
		Type:        req.Type,
		Config:      req.Config,
		Schedule:    req.Schedule,
		BatchSize:   req.BatchSize,
		Status:      models.TaskStatusIdle,
		Progress:    0,
		Enabled:     req.Schedule != "", // 有 schedule 的任务默认不启用，需要用户手动启动
		TenantID:    tenantID,
		CreatedBy:   &userID,
	}

	// 处理 auto_scan_metadata 字段
	if req.AutoScanMetadata != nil {
		task.AutoScanMetadata = *req.AutoScanMetadata
	} else {
		task.AutoScanMetadata = true // 默认为 true
	}

	// 设置默认值
	if task.BatchSize == 0 {
		task.BatchSize = 1000
	}

	// 创建任务
	if err := s.taskRepo.Create(task); err != nil {
		s.logger.Error("failed to create task", "error", err)
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	// 创建字段映射
	if len(req.Mappings) > 0 {
		for i := range req.Mappings {
			req.Mappings[i].TaskID = task.ID
		}
		if err := s.mappingRepo.CreateBatch(req.Mappings); err != nil {
			s.logger.Warn("failed to create mappings", "error", err)
		}
	}

	s.logger.Info("task created successfully", "task_id", task.ID)
	return task, nil
}

// GetTask 获取任务
func (s *TaskService) GetTask(ctx context.Context, id, tenantID uint) (*models.Task, error) {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return nil, err
	}

	// 检查租户权限
	if task.TenantID != tenantID {
		return nil, fmt.Errorf("task not found or access denied")
	}

	return task, nil
}

// UpdateTask 更新任务
func (s *TaskService) UpdateTask(ctx context.Context, id, tenantID uint, req *models.UpdateTaskRequest) (*models.Task, error) {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}

	// 运行中的任务需要先停止才能更新
	if task.Status == models.TaskStatusRunning {
		return nil, fmt.Errorf("cannot update task in %s status", task.Status)
	}

	// 更新字段
	if req.Name != nil {
		task.Name = *req.Name
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.Config != nil {
		task.Config = req.Config
	}
	if req.Schedule != nil {
		task.Schedule = *req.Schedule
	}
	if req.BatchSize != nil {
		task.BatchSize = *req.BatchSize
	}
	if req.Enabled != nil {
		task.Enabled = *req.Enabled
	}
	if req.AutoScanMetadata != nil {
		task.AutoScanMetadata = *req.AutoScanMetadata
	}

	if err := s.taskRepo.Update(task); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	s.logger.Info("task updated", "task_id", id)
	return task, nil
}

// DeleteTask 删除任务
func (s *TaskService) DeleteTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	// 只有非运行中的任务才能删除
	if task.Status == models.TaskStatusRunning {
		return fmt.Errorf("cannot delete running task")
	}

	// 删除字段映射
	if err := s.mappingRepo.DeleteByTaskID(id); err != nil {
		s.logger.Warn("failed to delete mappings", "error", err)
	}

	// 删除任务
	if err := s.taskRepo.Delete(id); err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	s.logger.Info("task deleted", "task_id", id)
	return nil
}

// ListTasks 列出任务
func (s *TaskService) ListTasks(ctx context.Context, tenantID uint, req *models.ListTasksRequest) ([]models.Task, int64, error) {
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 20
	}

	filters := make(map[string]interface{})
	if req.Type != nil {
		filters["type"] = *req.Type
	}
	if req.Status != nil {
		filters["status"] = *req.Status
	}

	return s.taskRepo.List(tenantID, filters, req.Page, req.PageSize)
}

// GetTaskStatistics 获取任务统计
func (s *TaskService) GetTaskStatistics(ctx context.Context, tenantID uint) (*models.TaskStatistics, error) {
	return s.taskRepo.GetStatistics(tenantID)
}

// StartTask 启动任务（立即执行）- 使用事务确保原子性
func (s *TaskService) StartTask(ctx context.Context, id, tenantID, userID uint) (*models.TaskExecution, error) {
	s.logger.Info("starting task", "task_id", id)

	var execution *models.TaskExecution

	// 使用事务包装所有操作，确保原子性
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 1. 锁定任务行（FOR UPDATE），防止并发冲突
		task := &models.Task{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			First(task).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("task not found or access denied")
			}
			return fmt.Errorf("failed to lock task: %w", err)
		}

		// 2. 检查任务状态
		if task.Status == models.TaskStatusRunning {
			return fmt.Errorf("task is already running")
		}

		now := time.Now()

		// 3. 创建执行记录
		execution = &models.TaskExecution{
			TaskID:      id,
			Status:      models.ExecutionStatusPending,
			StartTime:   models.LocalTime{Time: now},
			TriggerType: "manual",
			TriggerBy:   &userID,
		}

		if err := tx.Create(execution).Error; err != nil {
			return fmt.Errorf("failed to create execution: %w", err)
		}

		// 4. 原子更新任务状态（在同一事务中）
		updates := map[string]interface{}{
			"status":   models.TaskStatusRunning,
			"progress": 0,
		}

		if err := tx.Model(task).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update task state: %w", err)
		}

		return nil
	})

	if err != nil {
		s.logger.Error("failed to start task in transaction", "error", err, "task_id", id)
		return nil, err
	}

	// 事务提交成功后，将任务提交到队列（Asynq）
	if s.taskQueue != nil {
		if err := s.taskQueue.EnqueueExecuteTask(ctx, id, execution.ID, tenantID); err != nil {
			s.logger.Error("failed to enqueue task", "error", err, "task_id", id)

			// 回滚：标记执行失败
			s.execRepo.FinishExecution(execution.ID, models.ExecutionStatusFailed, err.Error())

			// 回滚：恢复任务状态为空闲
			if err := s.taskRepo.UpdateFields(id, map[string]interface{}{
				"status":   models.TaskStatusIdle,
				"progress": 0,
			}); err != nil {
				s.logger.Warn("failed to rollback task state after enqueue failure", "error", err, "task_id", id)
			}

			return nil, fmt.Errorf("failed to enqueue task: %w", err)
		}
		s.logger.Info("task enqueued to worker", "task_id", id, "execution_id", execution.ID)
	} else {
		s.logger.Warn("task queue not available, task will not be executed", "task_id", id)
	}

	s.logger.Info("task started", "task_id", id, "execution_id", execution.ID)
	return execution, nil
}

// StopTask 停止任务
func (s *TaskService) StopTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	if task.Status != models.TaskStatusRunning {
		return fmt.Errorf("task is not running")
	}

	// 查询最后一次执行记录
	lastExecution, err := s.execRepo.GetLatestByTaskID(id)
	if err == nil && lastExecution != nil && lastExecution.Status == models.ExecutionStatusRunning {
		if err := s.execRepo.FinishExecution(lastExecution.ID, models.ExecutionStatusFailed, "execution stopped by user"); err != nil {
			s.logger.Warn("failed to finish execution when stopping task", "error", err, "task_id", id)
		}
	}

	updates := map[string]interface{}{
		"status":   models.TaskStatusIdle,
		"progress": 0,
	}

	if err := s.taskRepo.UpdateFields(id, updates); err != nil {
		return fmt.Errorf("failed to stop task: %w", err)
	}

	s.logger.Info("task stopped", "task_id", id)
	return nil
}

// PauseTask 暂停任务（暂停定时调度）
func (s *TaskService) PauseTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	if task.Schedule == "" {
		return fmt.Errorf("manual tasks cannot be paused")
	}

	updates := map[string]interface{}{
		"enabled": false,
	}

	// 如果任务正在运行，需要停止当前执行
	if task.Status == models.TaskStatusRunning {
		// 查询最后一次执行记录
		lastExecution, err := s.execRepo.GetLatestByTaskID(id)
		if err == nil && lastExecution != nil && lastExecution.Status == models.ExecutionStatusRunning {
			if err := s.execRepo.FinishExecution(lastExecution.ID, models.ExecutionStatusFailed, "task paused by user"); err != nil {
				s.logger.Warn("failed to finish execution when pausing task", "error", err, "task_id", id)
			}
		}
		updates["status"] = models.TaskStatusIdle
		updates["progress"] = 0
	}

	if err := s.taskRepo.UpdateFields(id, updates); err != nil {
		return fmt.Errorf("failed to pause task: %w", err)
	}

	s.logger.Info("task paused", "task_id", id)
	return nil
}

// ResumeTask 恢复任务（恢复定时调度）
func (s *TaskService) ResumeTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	if task.Schedule == "" {
		return fmt.Errorf("manual tasks do not support resume")
	}

	if task.Enabled {
		return fmt.Errorf("task is already enabled")
	}

	updates := map[string]interface{}{
		"enabled": true,
	}

	if err := s.taskRepo.UpdateFields(id, updates); err != nil {
		return fmt.Errorf("failed to resume task: %w", err)
	}

	s.logger.Info("task resumed", "task_id", id)
	return nil
}

// ExecuteTask 执行任务（由 Worker 调用，委托给 ExecutionEngineService）
func (s *TaskService) ExecuteTask(ctx context.Context, taskID, executionID uint) error {
	return s.executionEngine.ExecuteTask(ctx, taskID, executionID)
}

// GetStatistics 获取任务统计信息
func (s *TaskService) GetStatistics(ctx context.Context, tenantID uint) (*models.TaskStatistics, error) {
	return s.taskRepo.GetStatistics(tenantID)
}

// CreateMapping 创建数据映射
func (s *TaskService) CreateMapping(ctx context.Context, taskID uint, req *models.CreateDataMappingRequest, tenantID, userID uint) (*models.DataMapping, error) {
	// 验证任务存在且属于该租户
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	if task.TenantID != tenantID {
		return nil, fmt.Errorf("task not found or access denied")
	}

	mapping := models.DataMapping{
		TaskID:       taskID,
		SourceField:  req.SourceField,
		TargetField:  req.TargetField,
		Transform:    req.Transform,
		DefaultValue: req.DefaultValue,
		FieldType:    req.FieldType,
		Format:       req.Format,
		Nullable:     req.Nullable,
	}

	// 使用 CreateBatch 创建单个映射
	if err := s.mappingRepo.CreateBatch([]models.DataMapping{mapping}); err != nil {
		return nil, fmt.Errorf("failed to create mapping: %w", err)
	}

	return &mapping, nil
}

// GetTaskMappings 获取任务的所有映射
func (s *TaskService) GetTaskMappings(ctx context.Context, taskID, tenantID uint) ([]*models.DataMapping, error) {
	// 验证任务存在且属于该租户
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	if task.TenantID != tenantID {
		return nil, fmt.Errorf("task not found or access denied")
	}

	mappings, err := s.mappingRepo.GetByTaskID(taskID)
	if err != nil {
		return nil, err
	}

	// 转换为指针数组
	result := make([]*models.DataMapping, len(mappings))
	for i := range mappings {
		result[i] = &mappings[i]
	}

	return result, nil
}

// DeleteMapping 删除数据映射
func (s *TaskService) DeleteMapping(ctx context.Context, mappingID, tenantID uint) error {
	// TODO: 需要在 MappingRepository 中添加 Delete 方法
	return fmt.Errorf("DeleteMapping not yet implemented")
}
