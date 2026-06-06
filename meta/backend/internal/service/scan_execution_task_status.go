package service

import (
	"time"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

// backfillTaskStatus 回写 ScanTask 的最近执行状态字段。
func (s *ScanExecutionService) backfillTaskStatus(taskID uint, executionID string, status string, completedAt time.Time, tenantID int) {
	taskUpdate := scantask.TaskStatusBackfillFields(executionID, status, completedAt, time.Now())
	if err := s.db.Model(&models.ScanTask{}).Where("id = ? AND tenant_id = ?", taskID, tenantID).Updates(taskUpdate).Error; err != nil {
		s.log.Warn("更新任务执行状态失败", "task_id", taskID, "error", err)
	}
}
