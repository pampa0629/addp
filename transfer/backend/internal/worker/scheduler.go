package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
)

// Scheduler 定时调度器（使用公共调度模块）
type Scheduler struct {
	scheduler     commonScheduler.Scheduler // 公共调度器
	taskRepo      *repository.TaskRepository
	taskQueue     *TaskQueue
	executionRepo *repository.ExecutionRepository
}

// NewScheduler 创建定时调度器
func NewScheduler(taskRepo *repository.TaskRepository, executionRepo *repository.ExecutionRepository, taskQueue *TaskQueue) *Scheduler {
	// 创建公共调度器（启用秒级精度）
	scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
		Name:          "transfer",
		EnableSeconds: true, // ⚠️ Transfer 需要秒级精度
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create scheduler: %v", err))
	}

	return &Scheduler{
		scheduler:     scheduler,
		taskRepo:      taskRepo,
		executionRepo: executionRepo,
		taskQueue:     taskQueue,
	}
}

// Start 启动调度器
func (s *Scheduler) Start(ctx context.Context) error {
	// 加载所有启用定时调度的任务
	tasks, err := s.loadScheduledTasks(ctx)
	if err != nil {
		return fmt.Errorf("加载定时任务失败: %w", err)
	}

	// 注册 Cron 任务
	for _, task := range tasks {
		if err := s.registerTask(ctx, task); err != nil {
			log.Printf("⚠️  注册定时任务失败 - TaskID: %d, Error: %v", task.ID, err)
			continue
		}
	}

	// 启动调度器
	if err := s.scheduler.Start(ctx); err != nil {
		return err
	}

	log.Printf("✅ 定时调度器已启动，已注册 %d 个定时任务", len(tasks))
	return nil
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	ctx := context.Background()
	s.scheduler.Stop(ctx)
	log.Println("🛑 定时调度器已停止")
}

// loadScheduledTasks 加载所有定时任务
func (s *Scheduler) loadScheduledTasks(ctx context.Context) ([]models.Task, error) {
	// 查询所有启用定时调度的任务
	filters := map[string]interface{}{
		"has_schedule": true, // 有 schedule 字段的任务
	}

	tasks, _, err := s.taskRepo.List(0, filters, 1, 1000) // 加载前 1000 个任务
	if err != nil {
		return nil, err
	}

	// 过滤出有效的定时任务
	var scheduledTasks []models.Task
	for _, task := range tasks {
		if task.Schedule != "" && (task.Status == models.TaskStatusScheduled || task.Status == models.TaskStatusRunning) {
			scheduledTasks = append(scheduledTasks, task)
		}
	}

	return scheduledTasks, nil
}

// registerTask 注册单个定时任务
func (s *Scheduler) registerTask(ctx context.Context, task models.Task) error {
	handler := s.createHandler(task)
	if err := s.scheduler.Schedule(ctx, fmt.Sprintf("%d", task.ID), task.Schedule, handler); err != nil {
		return fmt.Errorf("无效的 Cron 表达式: %w", err)
	}

	log.Printf("📅 已注册定时任务 - TaskID: %d, Name: %s, Schedule: %s", task.ID, task.Name, task.Schedule)
	return nil
}

// createHandler 创建任务处理器
func (s *Scheduler) createHandler(task models.Task) commonScheduler.TaskHandler {
	return func(ctx context.Context, taskID string) error {
		s.executeScheduledTask(ctx, task)
		return nil
	}
}

// executeScheduledTask 执行定时任务
func (s *Scheduler) executeScheduledTask(ctx context.Context, task models.Task) {
	log.Printf("⏰ 触发定时任务 - TaskID: %d, Name: %s", task.ID, task.Name)

	now := time.Now()

	// 创建执行记录
	execution := &models.TaskExecution{
		TaskID:      task.ID,
		Status:      models.ExecutionStatusPending,
		StartTime:   models.LocalTime{Time: now},
		TriggerType: "schedule",
	}

	if err := s.executionRepo.Create(execution); err != nil {
		log.Printf("❌ 创建执行记录失败 - TaskID: %d, Error: %v", task.ID, err)
		return
	}

	if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{
		"status":                     models.TaskStatusRunning,
		"progress":                   0,
		"last_execution_id":          execution.ID,
		"last_execution_status":      models.ExecutionStatusRunning,
		"last_execution_started_at":  now,
		"last_execution_finished_at": nil,
	}); err != nil {
		log.Printf("❌ 更新任务状态失败 - TaskID: %d, Error: %v", task.ID, err)
	}

	// 将任务加入队列
	if err := s.taskQueue.EnqueueExecuteTask(ctx, task.ID, execution.ID, task.TenantID); err != nil {
		log.Printf("❌ 任务入队失败 - TaskID: %d, Error: %v", task.ID, err)
		// 更新执行状态为失败
		finishedAt := time.Now()
		if err := s.executionRepo.FinishExecution(execution.ID, models.ExecutionStatusFailed, err.Error()); err != nil {
			log.Printf("❌ 更新执行状态失败 - ExecutionID: %d, Error: %v", execution.ID, err)
		}
		if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{
			"status":                     models.TaskStatusScheduled,
			"last_execution_status":      models.ExecutionStatusFailed,
			"last_execution_finished_at": finishedAt,
			"progress":                   0,
		}); err != nil {
			log.Printf("❌ 回滚任务状态失败 - TaskID: %d, Error: %v", task.ID, err)
		}
		return
	}

	log.Printf("✅ 定时任务已入队 - TaskID: %d, ExecutionID: %d", task.ID, execution.ID)
}

// AddTask 添加新的定时任务
func (s *Scheduler) AddTask(ctx context.Context, task models.Task) error {
	if task.Schedule == "" {
		return fmt.Errorf("任务没有定时调度配置")
	}

	return s.registerTask(ctx, task)
}

// RemoveTask 移除定时任务
func (s *Scheduler) RemoveTask(taskID uint) {
	ctx := context.Background()
	s.scheduler.Unschedule(ctx, fmt.Sprintf("%d", taskID))
	log.Printf("✅ 已移除定时任务 - TaskID: %d", taskID)
}

// Reload 重新加载所有定时任务
func (s *Scheduler) Reload(ctx context.Context) error {
	log.Println("🔄 重新加载定时任务...")

	// 停止现有调度
	s.Stop()

	// 重新创建调度器
	scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
		Name:          "transfer",
		EnableSeconds: true,
	})
	if err != nil {
		return fmt.Errorf("创建调度器失败: %w", err)
	}
	s.scheduler = scheduler

	// 重新启动
	return s.Start(ctx)
}
