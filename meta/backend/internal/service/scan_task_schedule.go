package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
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
func (s *ScanTaskService) UpsertEngineScanTaskFromPolicy(tenantID, userID, engineID uint, engineName string, scanPolicy *commonModels.ScanPolicy) (*models.ScanTask, error) {
	if engineID == 0 {
		return nil, errors.New("engine_id 不能为空")
	}
	if scanPolicy == nil || !scanPolicy.Enabled || !scanPolicy.ScheduledScan {
		return nil, s.DeleteEngineTaskBinding(tenantID, engineID)
	}

	var existingTask models.ScanTask
	findErr := s.db.Where("engine_id = ? AND tenant_id = ? AND owner_module = ? AND owner_ref = ?",
		engineID, tenantID, "system", scantask.AutomaticTaskOwnerRef(engineID)).First(&existingTask).Error

	cronExpr, err := scantask.BuildCronExpressionFromPolicy(s.exprBuilder, scanPolicy)
	if err != nil {
		return nil, fmt.Errorf("构建 Cron 表达式失败: %w", err)
	}
	if strings.TrimSpace(cronExpr) == "" {
		return nil, s.DeleteEngineTaskBinding(tenantID, engineID)
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
	scanDepth := strings.TrimSpace(scanPolicy.ScanDepth)
	if scanDepth == "" {
		scanDepth = scantask.DefaultEngineTaskScanDepth
	}

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

// DeleteEngineTaskBinding 删除当前租户下指定 engine 绑定的自动扫描任务。
func (s *ScanTaskService) DeleteEngineTaskBinding(tenantID, engineID uint) error {
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
