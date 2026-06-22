package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
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

	reqCopy := *req
	reqCopy.Schedule = strings.TrimSpace(reqCopy.Schedule)
	if err := s.validateScheduledTaskDefinition(tenantID, 0, &reqCopy); err != nil {
		return nil, err
	}

	now := time.Now()
	var nextRunAt *time.Time
	if reqCopy.Schedule != "" {
		nextRunAt = s.nextTimeFromSpec(reqCopy.Schedule, now)
	}
	task := scantask.NewTaskFromUpsertRequest(tenantID, userID, &reqCopy, now, nextRunAt)

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

	reqCopy := *req
	reqCopy.Schedule = strings.TrimSpace(reqCopy.Schedule)
	if err := s.validateScheduledTaskDefinition(tenantID, taskID, &reqCopy); err != nil {
		return nil, err
	}

	now := time.Now()
	var nextRunAt *time.Time
	if reqCopy.Schedule != "" {
		nextRunAt = s.nextTimeFromSpec(reqCopy.Schedule, now)
	}
	scantask.ApplyUpsertRequest(task, userID, &reqCopy, now, nextRunAt)

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

func (s *ScanTaskService) validateScheduledTaskDefinition(tenantID, excludeTaskID uint, req *models.ScanTaskUpsertRequest) error {
	if req == nil || !req.Enabled || strings.TrimSpace(req.Schedule) == "" {
		return nil
	}
	scope := scantask.TaskScope(req.EngineID, req.Scope, req.CatalogPaths)
	return s.validateScheduledTaskScope(tenantID, excludeTaskID, req.EngineID, scope, req.Schedule, req.Enabled)
}

func (s *ScanTaskService) validateScheduledTaskScope(tenantID, excludeTaskID, engineID uint, scope models.JSONMap, schedule string, enabled bool) error {
	if !enabled || strings.TrimSpace(schedule) == "" {
		return nil
	}
	if err := s.exprBuilder.Validate(schedule); err != nil {
		return fmt.Errorf("无效的 Cron 表达式: %w", err)
	}

	scopeKey, err := scanTaskScopeKey(engineID, scope)
	if err != nil {
		return err
	}

	var tasks []models.ScanTask
	query := s.db.Where("tenant_id = ? AND engine_id = ? AND enabled = ? AND schedule <> ''", tenantID, engineID, true)
	if excludeTaskID > 0 {
		query = query.Where("id <> ?", excludeTaskID)
	}
	if err := query.Find(&tasks).Error; err != nil {
		return fmt.Errorf("查询已有定时扫描任务失败: %w", err)
	}

	for _, task := range tasks {
		existingKey, err := scanTaskScopeKey(task.EngineID, task.Scope)
		if err != nil {
			return err
		}
		if existingKey == scopeKey {
			return fmt.Errorf("同一扫描范围已存在启用的定时任务: %s", task.Name)
		}
	}
	return nil
}

func scanTaskScopeKey(engineID uint, scope models.JSONMap) (string, error) {
	targets := scanflow.TargetsFromScope(scope)
	switch targets.ScopeType {
	case "", "engine":
		return fmt.Sprintf("engine:%d", engineID), nil
	case "catalog_path":
		paths := append([]string(nil), targets.CatalogPaths...)
		sort.Strings(paths)
		return "catalog_path:" + strings.Join(paths, "\x00"), nil
	case "ref_group":
		refGroups := append([]models.ScanRefGroup(nil), targets.RefGroups...)
		raw, err := json.Marshal(refGroups)
		if err != nil {
			return "", fmt.Errorf("序列化扫描范围失败: %w", err)
		}
		return "ref_group:" + string(raw), nil
	default:
		raw, err := json.Marshal(scope)
		if err != nil {
			return "", fmt.Errorf("序列化扫描范围失败: %w", err)
		}
		return targets.ScopeType + ":" + string(raw), nil
	}
}
