package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *ScanTaskService) runDueScheduledTasks(ctx context.Context) {
	now := time.Now()
	var taskIDs []uint
	if err := s.db.WithContext(ctx).
		Model(&models.ScanTask{}).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC").
		Limit(100).
		Pluck("id", &taskIDs).Error; err != nil {
		s.log.Warn("查询到期扫描任务失败", "error", err)
		return
	}

	for _, taskID := range taskIDs {
		executionID, err := s.claimDueScheduledTask(ctx, taskID, now)
		if err != nil {
			s.log.Warn("创建定时扫描执行失败", "task_id", taskID, "error", err)
			continue
		}
		if executionID != "" {
			s.enqueueExecution(executionID)
		}
	}
}

func (s *ScanTaskService) claimDueScheduledTask(ctx context.Context, taskID uint, now time.Time) (string, error) {
	var executionID string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.ScanTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ? AND enabled = ? AND schedule <> '' AND next_run_at IS NOT NULL AND next_run_at <= ?", taskID, true, now).
			First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		plannedRunAt := *task.NextRunAt
		exists, err := scheduledExecutionExists(tx, task.ID, plannedRunAt)
		if err != nil {
			return err
		}
		if exists {
			next := s.nextTimeFromSpec(task.Schedule, now)
			return tx.Model(&models.ScanTask{}).
				Where("id = ?", task.ID).
				Updates(scantask.ScheduledTaskTriggerFields(now, next, time.Now())).Error
		}

		if s.dedupService != nil {
			taskKey := s.dedupService.GenerateTaskKey(task.TenantID, task.EngineID, models.TriggerTypeScheduled)
			if s.dedupService.CheckTaskExists(ctx, taskKey) {
				s.log.Info("该资源正在扫描中，保留本次定时任务到期状态", "task_id", taskID)
				return nil
			}
			if err := s.dedupService.MarkTaskRunning(ctx, taskKey, 2*time.Hour); err != nil {
				s.log.Warn("标记任务运行失败", "error", err)
			}
		}

		targets := s.computeInheritedTargets(&task)
		if (targets.ScopeType == "catalog_path" && len(targets.CatalogPaths) == 0) ||
			(targets.ScopeType == "ref_group" && len(targets.RefGroups) == 0) {
			next := s.nextTimeFromSpec(task.Schedule, now)
			return tx.Model(&models.ScanTask{}).
				Where("id = ?", task.ID).
				Updates(scantask.ScheduledTaskTriggerFields(now, next, time.Now())).Error
		}

		storageType := s.lookupStorageType(task.EngineID, task.TenantID)
		execution := scantask.NewScheduledExecution(&task, storageType, targets, plannedRunAt, now)
		if err := tx.Create(execution).Error; err != nil {
			return err
		}

		next := s.nextTimeFromSpec(task.Schedule, now)
		if err := tx.Model(&models.ScanTask{}).
			Where("id = ?", task.ID).
			Updates(scantask.ScheduledTaskTriggerFields(now, next, time.Now())).Error; err != nil {
			return err
		}

		executionID = execution.ExecutionID
		return nil
	})
	return executionID, err
}

func scheduledExecutionExists(db *gorm.DB, taskID uint, plannedRunAt time.Time) (bool, error) {
	var count int64
	planned := plannedRunAt.Format(time.RFC3339Nano)
	if err := db.Model(&commonExecution.TaskExecution{}).
		Where("module = ? AND task_type = ? AND source_task_id = ? AND execution_config ->> 'planned_run_at' = ?",
			commonExecution.ModuleMeta, "scan", int(taskID), planned).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// computeInheritedTargets 计算继承目标（排除已有独立调度的schema/bucket）
func (s *ScanTaskService) computeInheritedTargets(task *models.ScanTask) scanflow.TargetSet {
	if task == nil {
		return scanflow.TargetSet{}
	}
	if task.Scope == nil {
		return scanflow.TargetSet{}
	}

	var independentTasks []models.ScanTask
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND id != ? AND enabled = ? AND schedule <> ''",
		task.TenantID, task.EngineID, task.ID, true).Find(&independentTasks).Error; err != nil {
		s.log.Warn("查询独立调度任务失败", "engine_id", task.EngineID, "error", err)
		return scanflow.TargetsFromScope(task.Scope)
	}

	independentScopes := make([]models.JSONMap, 0, len(independentTasks))
	for _, independent := range independentTasks {
		independentScopes = append(independentScopes, independent.Scope)
	}
	return scanflow.InheritedTargets(task.Scope, independentScopes)
}

func (s *ScanTaskService) lookupStorageType(engineID, tenantID uint) string {
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, "")
	if err != nil {
		s.log.Warn("获取资源存储类型失败", "engine_id", engineID, "tenant_id", tenantID, "error", err)
		return "unknown"
	}
	return scantask.NormalizeStorageType(resource.EngineType)
}

func (s *ScanTaskService) nextTimeFromSpec(spec string, from time.Time) *time.Time {
	if spec == "" {
		return nil
	}
	next, err := s.exprBuilder.NextRunTime(spec, from)
	if err != nil {
		s.log.Warn("解析 Cron 表达式失败", "spec", spec, "error", err)
		return nil
	}
	return &next
}

// CreateOrUpdateTaskFromScanConfig 根据资源的扫描配置创建或更新自动扫描任务
func (s *ScanTaskService) CreateOrUpdateTaskFromScanConfig(resource *commonModels.Engine) error {
	if resource == nil {
		return nil
	}
	if resource.ScanConfig == nil || !resource.ScanConfig.Enabled || !resource.ScanConfig.ScheduledScan {
		return s.DeleteTaskByResourceID(resource.ID)
	}

	scanConfig := resource.ScanConfig

	var tenantID uint
	if resource.TenantID != nil {
		tenantID = *resource.TenantID
	}

	var existingTask models.ScanTask
	findErr := s.db.Where("engine_id = ? AND tenant_id = ? AND owner_module = ? AND owner_ref = ?",
		resource.ID, tenantID, "system", scantask.AutomaticTaskOwnerRef(resource.ID)).First(&existingTask).Error

	cronExpr, err := scantask.BuildCronExpressionFromScanConfig(s.exprBuilder, scanConfig)
	if err != nil {
		return fmt.Errorf("构建 Cron 表达式失败: %w", err)
	}

	if errors.Is(findErr, gorm.ErrRecordNotFound) {
		task := scantask.NewAutomaticTask(resource, tenantID, cronExpr)
		task.NextRunAt = s.nextTimeFromSpec(cronExpr, time.Now())
		if err := s.db.Create(task).Error; err != nil {
			return fmt.Errorf("创建自动扫描任务失败: %w", err)
		}

		s.log.Info("自动扫描任务已创建",
			"task_id", task.ID,
			"engine_id", resource.ID,
			"resource_name", resource.Name)

		return nil
	} else if findErr != nil {
		return fmt.Errorf("查询已有任务失败: %w", findErr)
	}

	now := time.Now()
	nextRunAt := s.nextTimeFromSpec(cronExpr, now)
	if err := s.db.Model(&existingTask).Updates(scantask.AutomaticTaskUpdates(resource, cronExpr, nextRunAt, now)).Error; err != nil {
		return fmt.Errorf("更新自动扫描任务失败: %w", err)
	}

	s.log.Info("自动扫描任务已更新",
		"task_id", existingTask.ID,
		"engine_id", resource.ID,
		"resource_name", resource.Name)

	return nil
}

// DeleteTaskByResourceID 删除指定资源关联的所有自动扫描任务
func (s *ScanTaskService) DeleteTaskByResourceID(engineID uint) error {
	var tasks []models.ScanTask
	if err := s.db.Where("engine_id = ? AND owner_module = ? AND owner_ref = ?",
		engineID, "system", scantask.AutomaticTaskOwnerRef(engineID)).Find(&tasks).Error; err != nil {
		return fmt.Errorf("查询资源关联任务失败: %w", err)
	}

	for _, task := range tasks {
		if err := s.db.Delete(&task).Error; err != nil {
			s.log.Warn("删除任务失败", "task_id", task.ID, "error", err)
		}
	}

	return nil
}
