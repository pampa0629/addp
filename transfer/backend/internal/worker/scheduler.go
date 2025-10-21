package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/hibiken/asynq"
	"github.com/robfig/cron/v3"
)

// Scheduler 定时调度器
type Scheduler struct {
	cron         *cron.Cron
	taskRepo     *repository.TaskRepository
	taskQueue    *TaskQueue
	executionRepo *repository.ExecutionRepository
}

// NewScheduler 创建定时调度器
func NewScheduler(taskRepo *repository.TaskRepository, executionRepo *repository.ExecutionRepository, taskQueue *TaskQueue) *Scheduler {
	return &Scheduler{
		cron:         cron.New(cron.WithSeconds()), // 支持秒级调度
		taskRepo:     taskRepo,
		executionRepo: executionRepo,
		taskQueue:    taskQueue,
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

	// 启动 Cron
	s.cron.Start()
	log.Printf("✅ 定时调度器已启动，已注册 %d 个定时任务", len(tasks))

	return nil
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
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
		if task.Schedule != "" && task.Status != models.TaskStatusStopped {
			scheduledTasks = append(scheduledTasks, task)
		}
	}

	return scheduledTasks, nil
}

// registerTask 注册单个定时任务
func (s *Scheduler) registerTask(ctx context.Context, task models.Task) error {
	// 解析 Cron 表达式
	_, err := s.cron.AddFunc(task.Schedule, func() {
		s.executeScheduledTask(context.Background(), task)
	})

	if err != nil {
		return fmt.Errorf("无效的 Cron 表达式: %w", err)
	}

	log.Printf("📅 已注册定时任务 - TaskID: %d, Name: %s, Schedule: %s", task.ID, task.Name, task.Schedule)
	return nil
}

// executeScheduledTask 执行定时任务
func (s *Scheduler) executeScheduledTask(ctx context.Context, task models.Task) {
	log.Printf("⏰ 触发定时任务 - TaskID: %d, Name: %s", task.ID, task.Name)

	// 创建执行记录
	execution := &models.TaskExecution{
		TaskID:      task.ID,
		Status:      models.ExecutionStatusPending,
		StartTime:   time.Now(),
		TriggerType: "schedule",
	}

	if err := s.executionRepo.Create(execution); err != nil {
		log.Printf("❌ 创建执行记录失败 - TaskID: %d, Error: %v", task.ID, err)
		return
	}

	// 将任务加入队列
	if err := s.taskQueue.EnqueueExecuteTask(ctx, task.ID, execution.ID, task.TenantID); err != nil {
		log.Printf("❌ 任务入队失败 - TaskID: %d, Error: %v", task.ID, err)
		// 更新执行状态为失败
		execution.Status = models.ExecutionStatusFailed
		execution.ErrorMsg = err.Error()
		s.executionRepo.Update(execution)
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
	// Cron 库不支持按 ID 删除，需要重启调度器
	// 实际使用时可以维护一个 map[taskID]cron.EntryID 来管理
	log.Printf("⚠️  移除定时任务 - TaskID: %d (需要重启调度器)", taskID)
}

// Reload 重新加载所有定时任务
func (s *Scheduler) Reload(ctx context.Context) error {
	log.Println("🔄 重新加载定时任务...")

	// 停止现有调度
	s.cron.Stop()

	// 创建新的 Cron 实例
	s.cron = cron.New(cron.WithSeconds())

	// 重新启动
	return s.Start(ctx)
}
