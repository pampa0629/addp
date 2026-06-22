package service

import (
	"context"
	"log"
	"time"

	commonScheduler "github.com/addp/common/scheduler"
)

// SchedulerService 定时任务调度服务
type SchedulerService struct {
	scheduler       commonScheduler.Scheduler
	archiveService  *LogArchiveService
	archiveSchedule string // cron 表达式
	retentionDays   int
}

// NewSchedulerService 创建调度服务
func NewSchedulerService(archiveService *LogArchiveService, schedule string, retentionDays int) (*SchedulerService, error) {
	scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
		Name: "system-log-archive",
	})
	if err != nil {
		return nil, err
	}

	return &SchedulerService{
		scheduler:       scheduler,
		archiveService:  archiveService,
		archiveSchedule: schedule,
		retentionDays:   retentionDays,
	}, nil
}

// Start 启动定时任务
func (s *SchedulerService) Start() error {
	ctx := context.Background()

	// 注册日志归档任务
	if err := s.scheduler.Schedule(ctx, "log-archive", s.archiveSchedule, func(ctx context.Context, taskID string) error {
		log.Println("⏰ 开始执行日志归档任务...")
		if err := s.archiveService.ArchiveOldLogsToCSV(s.retentionDays); err != nil {
			log.Printf("❌ 日志归档任务失败: %v", err)
			return err
		} else {
			log.Println("✅ 日志归档任务完成")
		}
		return nil
	}); err != nil {
		return err
	}

	if err := s.scheduler.Start(ctx); err != nil {
		return err
	}

	log.Printf("✓ 日志归档定时任务已启动（计划：%s）", s.archiveSchedule)
	return nil
}

// Stop 停止定时任务
func (s *SchedulerService) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.scheduler.Stop(ctx); err != nil {
		log.Printf("定时任务停止失败: %v", err)
		return
	}
	log.Println("定时任务已停止")
}
