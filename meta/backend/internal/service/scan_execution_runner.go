package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
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
		lockKey = s.dedupService.GenerateExecutionLockKey(uint(exec.TenantID), execConfig.EngineID, execConfig.ItemID, execConfig.CatalogPaths, execConfig.RefGroups, execConfig.Targets)
	}

	lease, ok := s.boundedLease(ctx, executionID)
	if !ok || lease.TenantID != exec.TenantID || lease.Attempt != exec.Attempt {
		return fmt.Errorf("执行缺少当前有界租约: execution_id=%s", executionID)
	}
	if exec.Status != commonExecution.ExecutionStatusRunning {
		return fmt.Errorf("有界执行不是运行中状态: execution_id=%s status=%s", executionID, exec.Status)
	}
	defer s.finishExecutionDedupState(executionID, execConfig, lockKey)
	// Reject malformed frozen read declarations before opening content.
	if _, err := format.HeaderModeFromOptions(exec.ExecutionConfig); err != nil {
		return err
	}
	collectionCtx, err := s.bindCollectionRead(ctx, exec, lease)
	if err != nil {
		// A denied/dependency-failed check is an explicit failed attempt, not a
		// running execution waiting for lease recovery. No source was opened.
		completedAt := time.Now()
		var durationMs int64
		if exec.StartedAt != nil {
			durationMs = completedAt.Sub(*exec.StartedAt).Milliseconds()
		}
		return errors.Join(err, s.completeExecutionWithFailure(ctx, exec, nil, execConfig.StorageType, err, completedAt, durationMs))
	}
	ctx = collectionCtx

	start := time.Now()
	if exec.StartedAt != nil {
		start = *exec.StartedAt
	}
	if err := commonExecution.UpdateWithLease(ctx, s.db, lease, scantask.RunningExecutionFields()); err != nil {
		return err
	}

	if execConfig.ScanDepth == "" {
		execConfig.ScanDepth = scanflow.ScanDepthDeep
	}

	reporter := scantask.NewExecProgressReporter(s, executionID, exec.TenantID)
	defer reporter.Close()
	reporter.Message("任务开始执行")

	resp, scanErr := s.scanService.ScanEngineWithOptions(scanflow.Options{
		Context:      ctx,
		EngineID:     execConfig.EngineID,
		TenantID:     uint(exec.TenantID),
		CatalogPaths: execConfig.CatalogPaths,
		Targets:      execConfig.Targets,
		RefGroups:    execConfig.RefGroups,
		ItemID:       execConfig.ItemID,
		Encoding:     execConfig.Encoding,
		HasHeader:    execConfig.HasHeader,
		ScanDepth:    execConfig.ScanDepth,
		Force:        execConfig.Force,
		Source:       execConfig.Source,
		Reporter:     reporter,
	})
	reporter.Close()
	completeTime := time.Now()
	durationMs := completeTime.Sub(start).Milliseconds()

	if scanErr != nil {
		if err := s.completeExecutionWithFailure(ctx, exec, resp, execConfig.StorageType, scanErr, completeTime, durationMs); err != nil {
			return err
		}
		return scanErr
	}

	return s.completeExecutionWithSuccess(ctx, exec, resp, execConfig.StorageType, completeTime, durationMs)
}
