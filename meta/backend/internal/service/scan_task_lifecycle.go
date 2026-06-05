package service

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

// SetTaskQueue 设置任务队列（用于异步执行）
func (s *ScanTaskService) SetTaskQueue(queue TaskQueue) {
	s.taskQueue = queue
}

// Start 启动任务服务（队列消费者 + 定时调度）
func (s *ScanTaskService) Start(ctx context.Context) error {
	s.log.Info("启动扫描任务服务")

	if s.taskQueue == nil {
		for i := 0; i < s.workers; i++ {
			s.wg.Add(1)
			go s.workerLoop()
		}
	}

	if err := s.recoverPendingExecutions(); err != nil {
		s.log.Warn("恢复历史执行失败", "error", err)
	}

	if err := s.ensureScheduledTaskNextRuns(); err != nil {
		s.log.Warn("初始化定时任务 next_run_at 失败", "error", err)
	}

	s.wg.Add(1)
	go s.scheduledLoop(ctx)
	return nil
}

// Stop 停止任务服务
func (s *ScanTaskService) Stop(ctx context.Context) {
	s.log.Info("停止扫描任务服务")
	close(s.stopCh)
	s.wg.Wait()
	close(s.queue)
}

func (s *ScanTaskService) workerLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case executionID, ok := <-s.queue:
			if !ok {
				return
			}
			if err := s.executeRun(context.Background(), executionID); err != nil {
				s.log.Error("任务执行失败", "execution_id", executionID, "error", err)
			}
		}
	}
}

func (s *ScanTaskService) recoverPendingExecutions() error {
	ctx := context.Background()
	executions, err := s.taskExecutionRepo.GetRunningExecutions(ctx, 0)
	if err != nil {
		return err
	}

	for _, exec := range executions {
		if exec.Module != commonExecution.ModuleMeta {
			continue
		}
		if exec.Status == commonExecution.ExecutionStatusRunning {
			if err := s.taskExecutionRepo.UpdateFields(ctx, exec.ExecutionID, exec.TenantID, map[string]interface{}{
				"status":       commonExecution.ExecutionStatusPending,
				"current_step": "检测到未完成执行，已重新排队",
				"updated_at":   time.Now(),
			}); err != nil {
				s.log.Warn("重置执行状态失败", "execution_id", exec.ExecutionID, "error", err)
				continue
			}
		}
		s.enqueueExecution(exec.ExecutionID)
	}
	return nil
}

func (s *ScanTaskService) ensureScheduledTaskNextRuns() error {
	var tasks []models.ScanTask
	if err := s.db.Where("enabled = ? AND schedule <> '' AND next_run_at IS NULL", true).Find(&tasks).Error; err != nil {
		return err
	}

	now := time.Now()
	for i := range tasks {
		task := tasks[i]
		next := s.nextTimeFromSpec(task.Schedule, now)
		if next == nil {
			continue
		}
		if err := s.db.Model(&models.ScanTask{}).Where("id = ?", task.ID).Update("next_run_at", *next).Error; err != nil {
			s.log.Warn("初始化定时任务 next_run_at 失败", "task_id", task.ID, "error", err)
		}
	}
	return nil
}

func (s *ScanTaskService) scheduledLoop(ctx context.Context) {
	defer s.wg.Done()

	s.runDueScheduledTasks(context.Background())

	ticker := time.NewTicker(schedulePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDueScheduledTasks(context.Background())
		}
	}
}

func (s *ScanTaskService) enqueueExecution(executionID string) {
	if s.taskQueue != nil {
		ctx := context.Background()
		exec, err := s.taskExecutionRepo.GetByExecutionID(ctx, executionID, 0)
		if err != nil {
			s.log.Error("获取执行信息失败", "execution_id", executionID, "error", err)
			return
		}

		var taskID uint
		if exec.SourceTaskID != nil {
			taskID = uint(*exec.SourceTaskID)
		}

		if err := s.taskQueue.EnqueueScanTask(ctx, executionID, taskID, uint(exec.TenantID)); err != nil {
			s.log.Error("任务入队失败", "execution_id", executionID, "error", err)
			_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, exec.TenantID, map[string]interface{}{
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
