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

	lease, ok := s.boundedLease(ctx, executionID)
	if !ok || lease.TenantID != exec.TenantID || lease.Attempt != exec.Attempt {
		return fmt.Errorf("执行缺少当前有界租约: execution_id=%s", executionID)
	}
	if exec.Status != commonExecution.ExecutionStatusRunning {
		return fmt.Errorf("有界执行不是运行中状态: execution_id=%s status=%s", executionID, exec.Status)
	}
	defer s.finishExecutionDedupState(executionID, execConfig, lockKey)

	start := time.Now()
	if exec.StartedAt != nil {
		start = *exec.StartedAt
	}
	if err := commonExecution.UpdateWithLease(ctx, s.db, lease, scantask.RunningExecutionFields(time.Now())); err != nil {
		return err
	}

	if execConfig.ScanDepth == "" {
		execConfig.ScanDepth = scanflow.ScanDepthDeep
	}

	reporter := scantask.NewExecProgressReporter(s, executionID, exec.TenantID)
	reporter.Message("任务开始执行")

	resp, scanErr := s.scanService.ScanEngineWithOptions(scanflow.Options{
		Context:      ctx,
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
		if err := s.completeExecutionWithFailure(ctx, exec, scanErr, completeTime, durationMs); err != nil {
			return err
		}
		return scanErr
	}

	return s.completeExecutionWithSuccess(ctx, exec, resp, execConfig.StorageType, completeTime, durationMs)
}
