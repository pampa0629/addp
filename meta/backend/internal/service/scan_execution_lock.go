package service

import (
	"context"

	"github.com/addp/meta/internal/scanflow"
)

func (s *ScanExecutionService) releaseExecutionLock(ctx context.Context, acquired bool, lockKey string, owner string, msg string, fields ...any) {
	if !acquired || lockKey == "" || s.dedupService == nil {
		return
	}
	if _, err := s.dedupService.ReleaseOwnedLock(ctx, lockKey, owner); err != nil {
		s.log.Warn(msg, append(fields, "error", err)...)
	}
}

func (s *ScanExecutionService) finishExecutionDedupState(executionID string, execConfig scanflow.ExecutionConfig, lockKey string) {
	if s.dedupService == nil {
		return
	}
	s.releaseExecutionLock(context.Background(), lockKey != "", lockKey, executionID, "清除扫描范围锁失败", "execution_id", executionID)
	if err := s.dedupService.UpdateLastScanTime(context.Background(), execConfig.EngineID); err != nil {
		s.log.Warn("更新最后扫描时间失败", "execution_id", executionID, "error", err)
	}
}
