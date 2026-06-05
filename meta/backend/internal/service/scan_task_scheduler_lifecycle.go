package service

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

// Start 启动调度器（队列消费者 + 定时调度）
func (s *ScanTaskScheduler) Start(ctx context.Context) error {
	if s.taskService == nil {
		return fmt.Errorf("task service is required")
	}
	if s.executionService == nil {
		return fmt.Errorf("execution service is required")
	}

	s.taskService.log.Info("启动扫描任务调度器")

	if s.taskQueue == nil {
		for i := 0; i < s.workers; i++ {
			s.wg.Add(1)
			go s.workerLoop()
		}
	}

	if err := s.recoverPendingExecutions(); err != nil {
		s.taskService.log.Warn("恢复历史执行失败", "error", err)
	}

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
	close(s.queue)
}

// EnqueueExecution 实现 execution 分发接口
func (s *ScanTaskScheduler) EnqueueExecution(executionID string) {
	if s.executionService == nil {
		return
	}

	if s.taskQueue != nil {
		ctx := context.Background()
		exec, err := s.executionService.taskExecutionRepo.GetByExecutionID(ctx, executionID, 0)
		if err != nil {
			s.executionService.log.Error("获取执行信息失败", "execution_id", executionID, "error", err)
			return
		}

		var taskID uint
		if exec.SourceTaskID != nil {
			taskID = uint(*exec.SourceTaskID)
		}

		if err := s.taskQueue.EnqueueScanTask(ctx, executionID, taskID, uint(exec.TenantID)); err != nil {
			s.executionService.log.Error("任务入队失败", "execution_id", executionID, "error", err)
			_ = s.executionService.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, map[string]interface{}{
				"status": commonExecution.ExecutionStatusFailed,
				"error_details": commonModels.JSONMap{
					"message": fmt.Sprintf("任务入队失败: %v", err),
				},
				"updated_at": time.Now(),
			})
		}
		return
	}

	select {
	case s.queue <- executionID:
	default:
		s.queue <- executionID
	}
}

func (s *ScanTaskScheduler) workerLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case executionID, ok := <-s.queue:
			if !ok {
				return
			}
			if err := s.executionService.executeRun(context.Background(), executionID); err != nil {
				s.executionService.log.Error("任务执行失败", "execution_id", executionID, "error", err)
			}
		}
	}
}

func (s *ScanTaskScheduler) recoverPendingExecutions() error {
	ctx := context.Background()
	executions, err := s.executionService.taskExecutionRepo.GetRunningExecutions(ctx, 0)
	if err != nil {
		return err
	}

	for _, exec := range executions {
		if exec.Module != commonExecution.ModuleMeta {
			continue
		}
		if exec.Status == commonExecution.ExecutionStatusRunning {
			if err := s.executionService.taskExecutionRepo.UpdateFields(ctx, exec.ExecutionID, exec.TenantID, map[string]interface{}{
				"status":       commonExecution.ExecutionStatusPending,
				"current_step": "检测到未完成执行，已重新排队",
				"updated_at":   time.Now(),
			}); err != nil {
				s.taskService.log.Warn("重置执行状态失败", "execution_id", exec.ExecutionID, "error", err)
				continue
			}
		}
		s.EnqueueExecution(exec.ExecutionID)
	}
	return nil
}
