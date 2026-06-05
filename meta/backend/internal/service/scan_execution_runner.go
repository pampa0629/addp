package service

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
)

// ExecuteScanRun 执行扫描（供 Worker 调用）
func (s *ScanExecutionService) ExecuteScanRun(ctx context.Context, executionID string) error {
	return s.executeRun(ctx, executionID)
}

func (s *ScanExecutionService) releaseExecutionLock(ctx context.Context, acquired bool, lockKey string, owner string, msg string, fields ...any) {
	if !acquired || lockKey == "" || s.dedupService == nil {
		return
	}
	if _, err := s.dedupService.ReleaseOwnedLock(ctx, lockKey, owner); err != nil {
		s.log.Warn(msg, append(fields, "error", err)...)
	}
}

func (s *ScanExecutionService) executeRun(ctx context.Context, executionID string) error {
	exec, err := s.taskExecutionRepo.GetByExecutionID(ctx, executionID, 0)
	if err != nil {
		return err
	}

	execConfig := scanflow.ParseExecutionConfig(exec.ExecutionConfig)
	if execConfig.EngineID == 0 {
		return fmt.Errorf("执行配置缺少 engine_id: execution_id=%s", executionID)
	}
	lockKey := ""
	if s.dedupService != nil {
		lockKey = s.dedupService.GenerateExecutionLockKey(uint(exec.TenantID), execConfig.EngineID, execConfig.ItemID, execConfig.CatalogPaths, execConfig.RefGroups)
	}

	defer func() {
		if s.dedupService != nil {
			s.releaseExecutionLock(context.Background(), lockKey != "", lockKey, executionID, "清除扫描范围锁失败", "execution_id", executionID)
			if err := s.dedupService.UpdateLastScanTime(context.Background(), execConfig.EngineID); err != nil {
				s.log.Warn("更新最后扫描时间失败", "execution_id", executionID, "error", err)
			}
		}
	}()

	if exec.Status != commonExecution.ExecutionStatusPending {
		s.log.Info("跳过非待执行任务", "execution_id", executionID, "status", exec.Status)
		return nil
	}

	start := time.Now()
	if err := s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, scantask.RunningExecutionFields(start, time.Now())); err != nil {
		return err
	}

	if execConfig.ScanDepth == "" {
		execConfig.ScanDepth = scanflow.ScanDepthDeep
	}

	reporter := scantask.NewExecProgressReporter(s, executionID, exec.TenantID)
	reporter.Message("任务开始执行")

	resp, scanErr := s.scanService.ScanEngineWithOptions(scanflow.Options{
		EngineID:     execConfig.EngineID,
		TenantID:     uint(exec.TenantID),
		CatalogPaths: execConfig.CatalogPaths,
		RefGroups:    execConfig.RefGroups,
		ItemID:       execConfig.ItemID,
		Token:        execConfig.Token,
		ScanDepth:    execConfig.ScanDepth,
		Force:        execConfig.Force,
		Source:       execConfig.Source,
		Reporter:     reporter,
	})
	completeTime := time.Now()
	durationMs := completeTime.Sub(start).Milliseconds()

	if scanErr != nil {
		_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, scantask.FailedExecutionFields(scanErr, completeTime, durationMs, time.Now()))

		if exec.SourceTaskID != nil {
			s.backfillTaskStatus(uint(*exec.SourceTaskID), executionID, commonExecution.ExecutionStatusFailed, completeTime, exec.TenantID)
		}
		return scanErr
	}

	_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, scantask.SuccessfulExecutionFields(resp, execConfig.StorageType, completeTime, durationMs, time.Now()))

	if exec.SourceTaskID != nil {
		s.backfillTaskStatus(uint(*exec.SourceTaskID), executionID, commonExecution.ExecutionStatusSuccess, completeTime, exec.TenantID)
	}

	return nil
}

// backfillTaskStatus 回写 ScanTask 的最近执行状态字段
func (s *ScanExecutionService) backfillTaskStatus(taskID uint, executionID string, status string, completedAt time.Time, tenantID int) {
	taskUpdate := scantask.TaskStatusBackfillFields(executionID, status, completedAt, time.Now())
	if err := s.db.Model(&models.ScanTask{}).Where("id = ? AND tenant_id = ?", taskID, tenantID).Updates(taskUpdate).Error; err != nil {
		s.log.Warn("更新任务执行状态失败", "task_id", taskID, "error", err)
	}
}

func (s *ScanExecutionService) updateExecutionProgress(executionID string, tenantID int, fields map[string]interface{}) {
	fields["updated_at"] = time.Now()
	if err := s.taskExecutionRepo.UpdateFields(context.Background(), executionID, tenantID, fields); err != nil {
		s.log.Warn("更新执行进度失败", "execution_id", executionID, "error", err)
	}
}

func (s *ScanExecutionService) UpdateExecutionProgress(executionID string, tenantID int, fields map[string]interface{}) {
	s.updateExecutionProgress(executionID, tenantID, fields)
}
