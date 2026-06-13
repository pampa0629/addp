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

// TileCacheTaskScheduler 负责按 manager.tile_cache_tasks.next_run_at 触发定时瓦片缓存生成任务。
type TileCacheTaskScheduler struct {
	taskService *TileCacheTaskService
	exprBuilder *commonScheduler.ExpressionBuilder
	log         *slog.Logger

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewTileCacheTaskScheduler(taskService *TileCacheTaskService) *TileCacheTaskScheduler {
	return &TileCacheTaskScheduler{
		taskService: taskService,
		exprBuilder: commonScheduler.NewExpressionBuilder(),
		log:         logger.With("component", "tile_cache_task_scheduler"),
		stopCh:      make(chan struct{}),
	}
}

func (s *TileCacheTaskScheduler) Start(ctx context.Context) error {
	if s == nil || s.taskService == nil || s.taskService.tileCacheRepo == nil {
		return nil
	}
	if err := s.ensureScheduledTaskNextRuns(ctx); err != nil {
		s.log.Warn("初始化瓦片缓存定时任务 next_run_at 失败", "error", err)
	}
	s.wg.Add(1)
	go s.scheduledLoop(ctx)
	s.log.Info("瓦片缓存任务调度器已启动")
	return nil
}

func (s *TileCacheTaskScheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
	s.log.Info("瓦片缓存任务调度器已停止")
}

func (s *TileCacheTaskScheduler) ensureScheduledTaskNextRuns(ctx context.Context) error {
	tasks, err := s.taskService.tileCacheRepo.ListTileCacheTasksMissingNextRun(ctx)
	if err != nil {
		return err
	}
	now := tileCacheScheduleNow()
	for i := range tasks {
		task := tasks[i]
		next := s.nextRunTime(task.Schedule, now)
		if next == nil {
			continue
		}
		if err := s.taskService.tileCacheRepo.UpdateTileCacheTaskNextRun(ctx, task.ID, next); err != nil {
			s.log.Warn("初始化瓦片缓存定时任务 next_run_at 失败", "task_id", task.ID, "error", err)
		}
	}
	return nil
}

func (s *TileCacheTaskScheduler) scheduledLoop(ctx context.Context) {
	defer s.wg.Done()

	s.runDueScheduledTasks(context.Background())
	ticker := time.NewTicker(tileCacheTaskSchedulePollInterval)
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

func (s *TileCacheTaskScheduler) runDueScheduledTasks(ctx context.Context) {
	now := tileCacheScheduleNow()
	taskIDs, err := s.taskService.tileCacheRepo.ListDueTileCacheTaskIDs(ctx, now, 100)
	if err != nil {
		s.log.Warn("查询到期瓦片缓存任务失败", "error", err)
		return
	}
	for _, taskID := range taskIDs {
		if err := s.claimAndExecuteDueTask(ctx, taskID, now); err != nil {
			s.log.Warn("触发定时瓦片缓存任务失败", "task_id", taskID, "error", err)
		}
	}
}

func (s *TileCacheTaskScheduler) claimAndExecuteDueTask(ctx context.Context, taskID uint, now time.Time) error {
	task, err := s.taskService.tileCacheRepo.GetTileCacheTaskByID(ctx, taskID)
	if err != nil || task == nil {
		return err
	}
	next := s.nextRunTime(task.Schedule, now)
	claimed, err := s.taskService.tileCacheRepo.ClaimDueTileCacheTask(ctx, taskID, task.Schedule, now, next)
	if err != nil || claimed == nil {
		return err
	}

	executionID, err := s.taskService.Execute(ctx, claimed.ID, claimed.TenantID, commonExecution.TriggerTypeScheduled, commonExecution.ModuleManager, nil)
	if err != nil {
		return err
	}
	s.log.Info("已触发定时瓦片缓存任务", "task_id", claimed.ID, "execution_id", executionID)
	return nil
}

func (s *TileCacheTaskScheduler) nextRunTime(schedule string, from time.Time) *time.Time {
	next, err := s.exprBuilder.NextRunTime(schedule, from)
	if err != nil {
		s.log.Warn("计算瓦片缓存任务下次执行时间失败", "schedule", schedule, "error", err)
		return nil
	}
	return &next
}
