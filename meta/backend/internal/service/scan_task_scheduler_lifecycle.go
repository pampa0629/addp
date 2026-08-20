package service

import (
	"context"
	"fmt"
)

// Start 启动 owner scheduler。实际扫描只由独立 meta-worker claim 执行。
func (s *ScanTaskScheduler) Start(ctx context.Context) error {
	if s.taskService == nil {
		return fmt.Errorf("task service is required")
	}
	if s.executionService == nil {
		return fmt.Errorf("execution service is required")
	}

	s.taskService.log.Info("启动扫描任务调度器")

	if err := s.ensureScheduledTaskNextRuns(); err != nil {
		s.taskService.log.Warn("初始化定时任务 next_run_at 失败", "error", err)
	}

	s.wg.Add(1)
	go s.scheduledLoop(ctx)
	return nil
}

// Stop 停止调度器
func (s *ScanTaskScheduler) Stop(ctx context.Context) {
	if s.taskService == nil {
		return
	}

	s.taskService.log.Info("停止扫描任务调度器")
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}
