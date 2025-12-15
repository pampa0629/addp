package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
)

// defaultScheduler 默认调度器实现
type defaultScheduler struct {
	opts      Options
	cron      *cron.Cron
	entryIDs  map[string]cron.EntryID
	taskInfos map[string]*TaskInfo
	mu        sync.RWMutex
	running   atomic.Bool
}

// NewScheduler 创建调度器实例
func NewScheduler(opts Options) (Scheduler, error) {
	// 应用默认配置
	if err := opts.ApplyDefaults(); err != nil {
		return nil, fmt.Errorf("apply defaults failed: %w", err)
	}

	// 创建 Cron 实例
	cronOpts := []cron.Option{
		cron.WithLocation(opts.Location),
	}
	if opts.EnableSeconds {
		cronOpts = append(cronOpts, cron.WithSeconds())
	}

	return &defaultScheduler{
		opts:      opts,
		cron:      cron.New(cronOpts...),
		entryIDs:  make(map[string]cron.EntryID),
		taskInfos: make(map[string]*TaskInfo),
	}, nil
}

// Start 启动调度器
func (s *defaultScheduler) Start(ctx context.Context) error {
	if s.running.Load() {
		return fmt.Errorf("scheduler already running")
	}

	// 加载任务（如果配置了 TaskLoader）
	if s.opts.TaskLoader != nil {
		tasks, err := s.opts.TaskLoader.LoadTasks(ctx)
		if err != nil {
			return fmt.Errorf("load tasks failed: %w", err)
		}

		for _, task := range tasks {
			if !task.Enabled {
				continue
			}
			if err := s.Schedule(ctx, task.ID, task.CronExpr, task.Handler); err != nil {
				s.opts.Logger.Warn("failed to schedule task", "task_id", task.ID, "error", err)
			}
		}
	}

	s.cron.Start()
	s.running.Store(true)
	s.opts.Logger.Info("scheduler started", "name", s.opts.Name)
	return nil
}

// Stop 停止调度器
func (s *defaultScheduler) Stop(ctx context.Context) error {
	if !s.running.Load() {
		return nil
	}

	cronCtx := s.cron.Stop()
	select {
	case <-cronCtx.Done():
		s.running.Store(false)
		s.opts.Logger.Info("scheduler stopped", "name", s.opts.Name)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsRunning 检查调度器是否运行中
func (s *defaultScheduler) IsRunning() bool {
	return s.running.Load()
}

// Schedule 调度任务
func (s *defaultScheduler) Schedule(ctx context.Context, id string, cronExpr string, handler TaskHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 去重检查（如果启用）
	if s.opts.EnableDedup && s.opts.DedupService != nil {
		key := s.opts.DedupKeyFunc(id)
		if s.opts.DedupService.CheckTaskExists(ctx, key) {
			return fmt.Errorf("task already running: %s", id)
		}
	}

	// 移除旧的调度（如果存在）
	if entryID, exists := s.entryIDs[id]; exists {
		s.cron.Remove(entryID)
		delete(s.entryIDs, id)
	}

	// 添加新的调度
	wrappedHandler := s.wrapHandler(ctx, id, handler)
	entryID, err := s.cron.AddFunc(cronExpr, wrappedHandler)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	// 计算下次执行时间
	nextRunTime, _ := s.GetNextRunTime(cronExpr)

	// 保存任务信息
	s.entryIDs[id] = entryID
	s.taskInfos[id] = &TaskInfo{
		ID:          id,
		CronExpr:    cronExpr,
		NextRunTime: nextRunTime,
		IsEnabled:   true,
	}

	s.opts.Logger.Info("task scheduled", "task_id", id, "cron_expr", cronExpr, "next_run", nextRunTime)
	return nil
}

// wrapHandler 包装任务处理器（增加去重、监控、错误处理）
func (s *defaultScheduler) wrapHandler(ctx context.Context, taskID string, handler TaskHandler) func() {
	return func() {
		startTime := time.Now()

		// 去重保护
		if s.opts.EnableDedup && s.opts.DedupService != nil {
			key := s.opts.DedupKeyFunc(taskID)

			// 检查最小执行间隔
			if s.opts.DedupMinInterval > 0 {
				if s.opts.DedupService.CheckTaskExists(ctx, key) {
					s.opts.Logger.Warn("task skipped due to dedup", "task_id", taskID)
					return
				}
			}

			// 标记任务运行中
			s.opts.DedupService.MarkTaskRunning(ctx, key, 2*time.Hour)
			defer s.opts.DedupService.ClearTask(ctx, key)
		}

		// 执行任务
		err := handler(ctx, taskID)

		// 更新任务信息
		s.mu.Lock()
		if info, ok := s.taskInfos[taskID]; ok {
			now := time.Now()
			info.LastRunTime = &now
			if info.CronExpr != "" {
				nextRun, _ := s.GetNextRunTime(info.CronExpr)
				info.NextRunTime = nextRun
			}
		}
		s.mu.Unlock()

		// 错误处理
		if err != nil {
			s.opts.ErrorHandler(ctx, taskID, err)
		}

		// 监控指标（如果启用）
		if s.opts.EnableMetrics {
			duration := time.Since(startTime)
			// TODO: 记录 Prometheus 指标
			s.opts.Logger.Info("task executed",
				"task_id", taskID,
				"duration", duration,
				"error", err)
		}
	}
}

// Unschedule 取消调度
func (s *defaultScheduler) Unschedule(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.entryIDs[id]; exists {
		s.cron.Remove(entryID)
		delete(s.entryIDs, id)
		delete(s.taskInfos, id)
		s.opts.Logger.Info("task unscheduled", "task_id", id)
		return nil
	}
	return fmt.Errorf("task not found: %s", id)
}

// UpdateSchedule 更新任务调度
func (s *defaultScheduler) UpdateSchedule(ctx context.Context, id string, cronExpr string) error {
	s.mu.RLock()
	handler := s.taskInfos[id]
	s.mu.RUnlock()

	if handler == nil {
		return fmt.Errorf("task not found: %s", id)
	}

	// 重新调度（Schedule 会自动移除旧的）
	// 注意：这里无法获取原始 handler，所以需要调用者重新 Schedule
	return fmt.Errorf("UpdateSchedule not fully implemented, please use Unschedule + Schedule")
}

// GetNextRunTime 计算下次执行时间
func (s *defaultScheduler) GetNextRunTime(cronExpr string) (time.Time, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if s.opts.EnableSeconds {
		parser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	}

	sched, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(time.Now()), nil
}

// GetScheduledTasks 获取所有调度任务
func (s *defaultScheduler) GetScheduledTasks() []TaskInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]TaskInfo, 0, len(s.taskInfos))
	for _, info := range s.taskInfos {
		tasks = append(tasks, *info)
	}
	return tasks
}

// GetTaskInfo 获取任务信息
func (s *defaultScheduler) GetTaskInfo(id string) (*TaskInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if info, ok := s.taskInfos[id]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("task not found: %s", id)
}
