package service

import (
	"context"
	"time"
)

// Reuse Manager's lifecycle-owned cleanup service. Exact tombstones survive
// process restarts and soft-deletion, including uncertain remote late writes.
func (s *CleanupService) runModel3DOutputCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.model3DGLBSvc.cleanupExecutionOutputs(cleanupCtx)
		cancel()
		if err != nil {
			s.log.Warn("清理三维快显执行对象失败", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
		}
	}
}
