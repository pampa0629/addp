package service

import (
	"context"
	"fmt"
	"log"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	commonScheduler "github.com/addp/common/scheduler"
	"gorm.io/gorm"
)

// SpatialScheduler GIS 工作流任务调度器
type SpatialScheduler struct {
	scheduler            commonScheduler.Scheduler
	spatialTaskRepo      *repository.SpatialTaskRepository
	gisExecutionService  *GISExecutionService
	db                   *gorm.DB
}

// NewSpatialScheduler 创建调度器实例
func NewSpatialScheduler(
	spatialTaskRepo *repository.SpatialTaskRepository,
	gisExecutionService *GISExecutionService,
	db *gorm.DB,
) *SpatialScheduler {
	// 创建调度器
	scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
		Name: "develop-spatial",
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create scheduler: %v", err))
	}

	service := &SpatialScheduler{
		scheduler:           scheduler,
		spatialTaskRepo:     spatialTaskRepo,
		gisExecutionService: gisExecutionService,
		db:                  db,
	}

	return service
}

// Start 启动调度器
func (s *SpatialScheduler) Start(ctx context.Context) error {
	log.Println("🔄 Starting Spatial Task Scheduler...")

	// 加载启用的定时任务
	tasks, err := s.loadEnabledTasks(ctx)
	if err != nil {
		return fmt.Errorf("failed to load tasks: %w", err)
	}

	// 调度所有任务
	for _, task := range tasks {
		if err := s.scheduleTask(ctx, task); err != nil {
			log.Printf("⚠️  Failed to schedule task %d: %v", task.ID, err)
		}
	}

	// 启动调度器
	if err := s.scheduler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	log.Printf("✅ Spatial Task Scheduler started with %d tasks", len(tasks))
	return nil
}

// Stop 停止调度器
func (s *SpatialScheduler) Stop(ctx context.Context) error {
	log.Println("🛑 Stopping Spatial Task Scheduler...")
	return s.scheduler.Stop(ctx)
}

// loadEnabledTasks 加载所有启用的定时任务
func (s *SpatialScheduler) loadEnabledTasks(ctx context.Context) ([]models.SpatialTask, error) {
	var tasks []models.SpatialTask
	err := s.db.
		Where("status = ? AND schedule IS NOT NULL AND schedule != ''", "active").
		Find(&tasks).Error

	return tasks, err
}

// scheduleTask 调度单个任务
func (s *SpatialScheduler) scheduleTask(ctx context.Context, task models.SpatialTask) error {
	taskID := fmt.Sprintf("%d", task.ID)

	// 创建任务处理器
	handler := func(ctx context.Context, id string) error {
		log.Printf("⏰ Executing scheduled task: %s (ID: %s)", task.Name, id)

		// 创建执行记录
		execution, err := s.gisExecutionService.CreateExecution(
			task.ID,
			nil, // 无输入参数（定时任务）
			"scheduled",
			task.CreatedBy,
			task.TenantID,
		)
		if err != nil {
			log.Printf("❌ Failed to create execution for scheduled task: %s, error: %v", task.Name, err)
			return err
		}

		// 异步执行
		s.gisExecutionService.ExecuteAsync(ctx, execution.ID, task.TenantID)

		log.Printf("✅ Scheduled task execution started: %s (execution ID: %d)", task.Name, execution.ID)
		return nil
	}

	// 调度任务
	if err := s.scheduler.Schedule(ctx, taskID, task.Schedule, handler); err != nil {
		return fmt.Errorf("schedule task %d failed: %w", task.ID, err)
	}

	log.Printf("📅 Scheduled task: %s (Cron: %s)", task.Name, task.Schedule)
	return nil
}

// RescheduleTask 重新调度任务（更新时调用）
func (s *SpatialScheduler) RescheduleTask(ctx context.Context, taskID uint) error {
	var task models.SpatialTask
	if err := s.db.First(&task, taskID).Error; err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	taskIDStr := fmt.Sprintf("%d", taskID)

	// 先取消现有调度
	s.scheduler.Unschedule(ctx, taskIDStr)

	// 如果任务未启用或没有 schedule，直接返回
	if task.Status != "active" || task.Schedule == "" {
		log.Printf("📭 Task %d unscheduled (status: %s, schedule: %s)", taskID, task.Status, task.Schedule)
		return nil
	}

	// 重新调度
	return s.scheduleTask(ctx, task)
}

// UnscheduleTask 取消调度任务（删除时调用）
func (s *SpatialScheduler) UnscheduleTask(ctx context.Context, taskID uint) error {
	taskIDStr := fmt.Sprintf("%d", taskID)
	return s.scheduler.Unschedule(ctx, taskIDStr)
}

// GetScheduledTasks 获取所有已调度的任务
func (s *SpatialScheduler) GetScheduledTasks() []commonScheduler.TaskInfo {
	return s.scheduler.GetScheduledTasks()
}
