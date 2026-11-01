package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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

// UpsertEngineScanTaskFromPolicy 根据 Console 提交的 engine 扫描策略创建或更新绑定任务。
func (s *ScanTaskService) UpsertEngineScanTaskFromPolicy(ctx context.Context, tenantID, userID, engineID uint, engineType, engineName string, scanPolicy *commonModels.ScanPolicy) (*models.ScanTask, error) {
	if engineID == 0 || strings.TrimSpace(engineType) == "" {
		return nil, errors.New("engine_id 不能为空")
	}
	depth := scantask.DefaultEngineTaskScanDepth
	if scanPolicy != nil && scanPolicy.ScanDepth != "" {
		depth = scanPolicy.ScanDepth
	}
	target := &models.ScanTask{TenantID: tenantID, EngineID: engineID, Scope: scantask.EngineScope(engineID), Parameters: models.JSONMap{"scan_depth": depth}}
	operation := "update"
	disabled := scanPolicy == nil || !scanPolicy.Enabled || !scanPolicy.ScheduledScan
	if disabled {
		operation = "inspect-update"
	}
	if err := checkScanTaskManagement(ctx, target, operation); err != nil {
		return nil, err
	}
	if disabled {
		if engineType == "postgresql" {
			return s.disableEngineTaskBinding(ctx, tenantID, userID, engineID)
		}
		return nil, s.deleteEngineTaskBinding(tenantID, engineID)
	}

	var existingTask models.ScanTask
	findErr := s.db.Where("engine_id = ? AND tenant_id = ? AND owner_module = ? AND owner_ref = ?",
		engineID, tenantID, "system", scantask.AutomaticTaskOwnerRef(engineID)).First(&existingTask).Error

	cronExpr, err := scantask.BuildCronExpressionFromPolicy(s.exprBuilder, scanPolicy)
	if err != nil {
		return nil, fmt.Errorf("构建 Cron 表达式失败: %w", err)
	}
	if strings.TrimSpace(cronExpr) == "" {
		if engineType != "postgresql" {
			return nil, s.deleteEngineTaskBinding(tenantID, engineID)
		}
		return nil, commonapi.ErrBadRequest
	}

	excludeTaskID := uint(0)
	if findErr == nil {
		excludeTaskID = existingTask.ID
	}
	if err := s.validateScheduledTaskScope(tenantID, excludeTaskID, engineID, scantask.EngineScope(engineID), cronExpr, true); err != nil {
		return nil, err
	}

	now := time.Now()
	nextRunAt := s.nextTimeFromSpec(cronExpr, now)
	taskName := engineName
	if strings.TrimSpace(taskName) == "" {
		taskName = fmt.Sprintf("Engine %d", engineID)
	}
	scanDepth := depth

	if errors.Is(findErr, gorm.ErrRecordNotFound) {
		task := scantask.NewEngineScanTask(tenantID, userID, engineID, taskName, cronExpr, scanDepth, now, nextRunAt)
		if err := s.db.Create(task).Error; err != nil {
			return nil, fmt.Errorf("创建 engine 扫描任务失败: %w", err)
		}

		s.log.Info("engine 扫描任务已创建",
			"task_id", task.ID,
			"engine_id", engineID,
			"engine_name", taskName)

		return task, nil
	} else if findErr != nil {
		return nil, fmt.Errorf("查询已有任务失败: %w", findErr)
	}

	if err := s.db.Model(&existingTask).Updates(scantask.EngineScanTaskUpdates(userID, taskName, cronExpr, scanDepth, nextRunAt, now)).Error; err != nil {
		return nil, fmt.Errorf("更新 engine 扫描任务失败: %w", err)
	}
	if err := s.db.First(&existingTask, existingTask.ID).Error; err != nil {
		return nil, err
	}

	s.log.Info("engine 扫描任务已更新",
		"task_id", existingTask.ID,
		"engine_id", engineID,
		"engine_name", taskName)

	return &existingTask, nil
}

// disableEngineTaskBinding preserves the plan and execution history. The same
// row lock used by the scheduler orders disable against its next dispatch.
func (s *ScanTaskService) disableEngineTaskBinding(ctx context.Context, tenantID, userID, engineID uint) (*models.ScanTask, error) {
	var task *models.ScanTask
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stored models.ScanTask
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND engine_id = ? AND owner_module = ? AND owner_ref = ?", tenantID, engineID, "system", scantask.AutomaticTaskOwnerRef(engineID)).Take(&stored).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&stored).Updates(map[string]any{"enabled": false, "next_run_at": nil, "updated_by": userID, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		if err := tx.First(&stored, stored.ID).Error; err != nil {
			return err
		}
		task = &stored
		return nil
	})
	return task, err
}

// DeleteEngineTaskBinding 删除当前租户下指定 engine 绑定的自动扫描任务。
func (s *ScanTaskService) deleteEngineTaskBinding(tenantID, engineID uint) error {
	var tasks []models.ScanTask
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND owner_module = ? AND owner_ref = ?",
		tenantID, engineID, "system", scantask.AutomaticTaskOwnerRef(engineID)).Find(&tasks).Error; err != nil {
		return fmt.Errorf("查询资源关联任务失败: %w", err)
	}

	for _, task := range tasks {
		if err := s.db.Delete(&task).Error; err != nil {
			s.log.Warn("删除任务失败", "task_id", task.ID, "error", err)
		}
	}

	return nil
}

// DeleteEngineTaskBindings 删除已不存在 engine 关联的所有自动扫描任务。
func (s *ScanTaskService) DeleteEngineTaskBindings(engineID uint) error {
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

func (s *ScanTaskService) DeleteEngineTaskBinding(ctx context.Context, tenantID, engineID uint) error {
	if err := checkScanTaskManagement(ctx, &models.ScanTask{TenantID: tenantID, EngineID: engineID}, "delete"); err != nil {
		return err
	}
	return s.deleteEngineTaskBinding(tenantID, engineID)
}
