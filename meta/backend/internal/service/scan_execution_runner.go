package service

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
)

// ExecuteScanRun 执行扫描（供 Worker 调用）
func (s *ScanExecutionService) ExecuteScanRun(ctx context.Context, executionID string) error {
	return s.executeRun(ctx, executionID)
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

	defer s.finishExecutionDedupState(executionID, execConfig, lockKey)

	if exec.Status != commonExecution.ExecutionStatusPending {
		s.log.Info("跳过非待执行任务", "execution_id", executionID, "status", exec.Status)
		return nil
	}

	start := time.Now()
	if err := s.taskExecutionRepo.StartExecution(ctx, executionID, exec.TenantID, start); err != nil {
		return err
	}
	if err := s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, scantask.RunningExecutionFields(time.Now())); err != nil {
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
		ScanDepth:    execConfig.ScanDepth,
		Force:        execConfig.Force,
		Source:       execConfig.Source,
		Reporter:     reporter,
	})
	completeTime := time.Now()
	durationMs := completeTime.Sub(start).Milliseconds()

	if scanErr != nil {
		s.completeExecutionWithFailure(ctx, executionID, exec.TenantID, exec.SourceTaskID, scanErr, completeTime, durationMs)
		return scanErr
	}

	s.completeExecutionWithSuccess(ctx, executionID, exec.TenantID, exec.SourceTaskID, resp, execConfig.StorageType, completeTime, durationMs)
	return nil
}
