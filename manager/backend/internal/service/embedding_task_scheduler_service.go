package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonScheduler "github.com/addp/common/scheduler"
)

const embeddingTaskSchedulePollInterval = time.Minute

// EmbeddingTaskScheduler 负责按 manager.embedding_tasks.next_run_at 触发定时向量化任务。
type EmbeddingTaskScheduler struct {
	taskService *EmbeddingTaskService
	exprBuilder *commonScheduler.ExpressionBuilder
	log         *slog.Logger
	claimGate   func() bool

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func (s *EmbeddingTaskScheduler) SetClaimGate(claimGate func() bool) {
	s.claimGate = claimGate
}

func NewEmbeddingTaskScheduler(taskService *EmbeddingTaskService) *EmbeddingTaskScheduler {
	return &EmbeddingTaskScheduler{
		taskService: taskService,
		exprBuilder: commonScheduler.NewExpressionBuilder(),
		log:         logger.With("component", "embedding_task_scheduler"),
		stopCh:      make(chan struct{}),
	}
}

func (s *EmbeddingTaskScheduler) Start(ctx context.Context) error {
	if s == nil || s.taskService == nil || s.taskService.embeddingRepo == nil {
		return nil
	}
	if err := s.ensureScheduledTaskNextRuns(ctx); err != nil {
		s.log.Warn("初始化向量化定时任务 next_run_at 失败", "error", err)
	}
	s.wg.Add(1)
	go s.scheduledLoop(ctx)
	s.log.Info("向量化任务调度器已启动")
	return nil
}

func (s *EmbeddingTaskScheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
	s.log.Info("向量化任务调度器已停止")
}

func (s *EmbeddingTaskScheduler) ensureScheduledTaskNextRuns(ctx context.Context) error {
	tasks, err := s.taskService.embeddingRepo.ListEmbeddingTasksMissingNextRun(ctx)
	if err != nil {
		return err
	}
	now := embeddingScheduleNow()
	for i := range tasks {
		task := tasks[i]
		next := s.nextRunTime(task.Schedule, now)
		if next == nil {
			continue
		}
		if err := s.taskService.embeddingRepo.UpdateEmbeddingTaskNextRun(ctx, task.ID, next); err != nil {
			s.log.Warn("初始化向量化定时任务 next_run_at 失败", "task_id", task.ID, "error", err)
		}
	}
	return nil
}

func (s *EmbeddingTaskScheduler) scheduledLoop(ctx context.Context) {
	defer s.wg.Done()

	s.runDueScheduledTasks(context.Background())
	ticker := time.NewTicker(embeddingTaskSchedulePollInterval)
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

func (s *EmbeddingTaskScheduler) runDueScheduledTasks(ctx context.Context) {
	if s.claimGate != nil && !s.claimGate() {
		return
	}
	now := embeddingScheduleNow()
	taskIDs, err := s.taskService.embeddingRepo.ListDueEmbeddingTaskIDs(ctx, now, 100)
	if err != nil {
		s.log.Warn("查询到期向量化任务失败", "error", err)
		return
	}
	for _, taskID := range taskIDs {
		if err := s.claimAndExecuteDueTask(ctx, taskID, now); err != nil {
			s.log.Warn("触发定时向量化任务失败", "task_id", taskID, "error", err)
		}
	}
}

func (s *EmbeddingTaskScheduler) claimAndExecuteDueTask(ctx context.Context, taskID uint, now time.Time) error {
	task, err := s.taskService.embeddingRepo.GetEmbeddingTaskByID(ctx, taskID)
	if err != nil || task == nil {
		return err
	}
	next := s.nextRunTime(task.Schedule, now)
	claimed, err := s.taskService.embeddingRepo.ClaimDueEmbeddingTask(ctx, taskID, task.Schedule, now, next)
	if err != nil || claimed == nil {
		return err
	}

	executionID, err := s.taskService.Execute(ctx, claimed.ID, claimed.TenantID, commonExecution.TriggerTypeScheduled, commonExecution.ModuleManager, nil)
	if err != nil {
		return err
	}
	s.log.Info("已触发定时向量化任务", "task_id", claimed.ID, "execution_id", executionID)
	return nil
}

func (s *EmbeddingTaskScheduler) nextRunTime(schedule string, from time.Time) *time.Time {
	next, err := s.exprBuilder.NextRunTime(schedule, from)
	if err != nil {
		s.log.Warn("计算向量化任务下次执行时间失败", "schedule", schedule, "error", err)
		return nil
	}
	return &next
}
