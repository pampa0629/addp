package service

import (
	"context"
	"errors"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *ScanTaskScheduler) ensureScheduledTaskNextRuns() error {
	var tasks []models.ScanTask
	if err := s.taskService.db.Where("enabled = ? AND schedule <> '' AND next_run_at IS NULL", true).Find(&tasks).Error; err != nil {
		return err
	}

	now := time.Now()
	for i := range tasks {
		task := tasks[i]
		next := s.taskService.nextTimeFromSpec(task.Schedule, now)
		if next == nil {
			continue
		}
		if err := s.taskService.db.Model(&models.ScanTask{}).Where("id = ?", task.ID).Update("next_run_at", *next).Error; err != nil {
			s.taskService.log.Warn("初始化定时任务 next_run_at 失败", "task_id", task.ID, "error", err)
		}
	}
	return nil
}

func (s *ScanTaskScheduler) scheduledLoop(ctx context.Context) {
	defer s.wg.Done()

	s.runDueScheduledTasks(context.Background())

	ticker := time.NewTicker(schedulePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDueScheduledTasks(context.Background())
		}
	}
}

func (s *ScanTaskScheduler) runDueScheduledTasks(ctx context.Context) {
	now := time.Now()
	var taskIDs []uint
	if err := s.taskService.db.WithContext(ctx).
		Model(&models.ScanTask{}).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC").
		Limit(100).
		Pluck("id", &taskIDs).Error; err != nil {
		s.taskService.log.Warn("查询到期扫描任务失败", "error", err)
		return
	}

	for _, taskID := range taskIDs {
		executionID, err := s.claimDueScheduledTask(ctx, taskID, now)
		if err != nil {
			s.taskService.log.Warn("创建定时扫描执行失败", "task_id", taskID, "error", err)
			continue
		}
		if executionID != "" {
			s.EnqueueExecution(executionID)
		}
	}
}

func (s *ScanTaskScheduler) claimDueScheduledTask(ctx context.Context, taskID uint, now time.Time) (string, error) {
	var executionID string
	var lockKey string
	var lockOwner string
	var lockAcquired bool
	err := s.taskService.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
			next := s.taskService.nextTimeFromSpec(task.Schedule, now)
			return tx.Model(&models.ScanTask{}).
				Where("id = ?", task.ID).
				Updates(scantask.ScheduledTaskTriggerFields(now, next, time.Now())).Error
		}

		targets := s.computeInheritedTargets(&task)
		if (targets.ScopeType == "catalog_path" && len(targets.CatalogPaths) == 0) ||
			(targets.ScopeType == "ref_group" && len(targets.RefGroups) == 0) {
			next := s.taskService.nextTimeFromSpec(task.Schedule, now)
			return tx.Model(&models.ScanTask{}).
				Where("id = ?", task.ID).
				Updates(scantask.ScheduledTaskTriggerFields(now, next, time.Now())).Error
		}

		storageType := s.executionService.lookupStorageType(task.EngineID, task.TenantID)
		execution := scantask.NewScheduledExecution(&task, storageType, targets, plannedRunAt, now)

		if s.executionService.dedupService != nil {
			lockKey = s.executionService.dedupService.GenerateExecutionLockKey(task.TenantID, task.EngineID, 0, targets.CatalogPaths, targets.RefGroups)
			acquired, err := s.executionService.dedupService.TryAcquireOwnedLock(ctx, lockKey, execution.ExecutionID, 2*time.Hour)
			if err != nil {
				s.taskService.log.Warn("标记定时扫描范围运行失败，将继续创建执行", "task_id", taskID, "error", err, "lock_key", lockKey)
			} else if !acquired {
				s.taskService.log.Info("该扫描范围正在执行中，保留本次定时任务到期状态", "task_id", taskID)
				return nil
			} else {
				lockAcquired = true
				lockOwner = execution.ExecutionID
			}
		}

		if err := tx.Create(execution).Error; err != nil {
			s.executionService.releaseExecutionLock(ctx, lockAcquired, lockKey, execution.ExecutionID, "创建定时执行失败后释放扫描范围锁失败", "task_id", taskID)
			return err
		}

		next := s.taskService.nextTimeFromSpec(task.Schedule, now)
		if err := tx.Model(&models.ScanTask{}).
			Where("id = ?", task.ID).
			Updates(scantask.ScheduledTaskTriggerFields(now, next, time.Now())).Error; err != nil {
			return err
		}

		executionID = execution.ExecutionID
		return nil
	})
	if err != nil {
		s.executionService.releaseExecutionLock(ctx, lockAcquired, lockKey, lockOwner, "定时执行事务失败后释放扫描范围锁失败", "task_id", taskID)
	}
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
func (s *ScanTaskScheduler) computeInheritedTargets(task *models.ScanTask) scanflow.TargetSet {
	if task == nil {
		return scanflow.TargetSet{}
	}
	if task.Scope == nil {
		return scanflow.TargetSet{}
	}

	var independentTasks []models.ScanTask
	if err := s.taskService.db.Where("tenant_id = ? AND engine_id = ? AND id != ? AND enabled = ? AND schedule <> ''",
		task.TenantID, task.EngineID, task.ID, true).Find(&independentTasks).Error; err != nil {
		s.taskService.log.Warn("查询独立调度任务失败", "engine_id", task.EngineID, "error", err)
		return scanflow.TargetsFromScope(task.Scope)
	}

	independentScopes := make([]models.JSONMap, 0, len(independentTasks))
	for _, independent := range independentTasks {
		independentScopes = append(independentScopes, independent.Scope)
	}
	return scanflow.InheritedTargets(task.Scope, independentScopes)
}
