package service

import (
	"context"
	"errors"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

// GetTask 获取单个任务
func (s *ScanTaskService) GetTask(tenantID, taskID uint) (*models.ScanTask, error) {
	var task models.ScanTask
	if err := s.db.Where("tenant_id = ?", tenantID).First(&task, taskID).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// ListTasks 获取租户下的任务列表
func (s *ScanTaskService) ListTasks(tenantID uint) ([]models.ScanTask, error) {
	var tasks []models.ScanTask
	if err := s.db.Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// CreateTask 创建新的扫描任务
func (s *ScanTaskService) CreateTask(ctx context.Context, tenantID, userID uint, req *models.ScanTaskUpsertRequest) (*models.ScanTask, error) {
	if req == nil {
		return nil, errors.New("请求不能为空")
	}
	if req.EngineID == 0 {
		return nil, errors.New("engine_id 不能为空")
	}
	if req.Name == "" {
		return nil, errors.New("任务名称不能为空")
	}

	now := time.Now()
	var nextRunAt *time.Time
	if req.Schedule != "" {
		nextRunAt = s.nextTimeFromSpec(req.Schedule, now)
	}
	task := scantask.NewTaskFromUpsertRequest(tenantID, userID, req, now, nextRunAt)

	if err := s.db.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

// UpdateTask 更新任务配置
func (s *ScanTaskService) UpdateTask(ctx context.Context, tenantID, taskID, userID uint, req *models.ScanTaskUpsertRequest) (*models.ScanTask, error) {
	task, err := s.GetTask(tenantID, taskID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var nextRunAt *time.Time
	if req.Schedule != "" {
		nextRunAt = s.nextTimeFromSpec(req.Schedule, now)
	}
	scantask.ApplyUpsertRequest(task, userID, req, now, nextRunAt)

	if err := s.db.Save(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

// DeleteTask 删除任务
func (s *ScanTaskService) DeleteTask(ctx context.Context, tenantID, taskID uint) error {
	if _, err := s.GetTask(tenantID, taskID); err != nil {
		return err
	}
	return s.db.Delete(&models.ScanTask{}, taskID).Error
}

// TriggerTaskNow 立即触发任务执行
func (s *ScanTaskService) TriggerTaskNow(ctx context.Context, tenantID, taskID, userID uint) (*commonExecution.TaskExecution, error) {
	task, err := s.GetTask(tenantID, taskID)
	if err != nil {
		return nil, err
	}

	storageType := s.lookupStorageType(task.EngineID, task.TenantID)

	execution := scantask.NewTaskManualExecution(task, userID, storageType, time.Now())

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		return nil, err
	}

	s.enqueueExecution(execution.ExecutionID)
	return execution, nil
}
