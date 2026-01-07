package service

import (
	"log"

	"github.com/robfig/cron/v3"
)

// SchedulerService 定时任务调度服务
type SchedulerService struct {
	cron            *cron.Cron
	archiveService  *LogArchiveService
	archiveSchedule string // cron 表达式
}

// NewSchedulerService 创建调度服务
func NewSchedulerService(archiveService *LogArchiveService, schedule string) *SchedulerService {
	return &SchedulerService{
		cron:            cron.New(),
		archiveService:  archiveService,
		archiveSchedule: schedule,
	}
}

// Start 启动定时任务
func (s *SchedulerService) Start() error {
	// 注册日志归档任务
	_, err := s.cron.AddFunc(s.archiveSchedule, func() {
		log.Println("⏰ 开始执行日志归档任务...")
		retentionDays := GetRetentionDaysFromEnv()
		if err := s.archiveService.ArchiveOldLogsToCSV(retentionDays); err != nil {
			log.Printf("❌ 日志归档任务失败: %v", err)
		} else {
			log.Println("✅ 日志归档任务完成")
		}
	})

	if err != nil {
		return err
	}

	s.cron.Start()
	log.Printf("✓ 日志归档定时任务已启动（计划：%s）", s.archiveSchedule)
	return nil
}

// Stop 停止定时任务
func (s *SchedulerService) Stop() {
	s.cron.Stop()
	log.Println("定时任务已停止")
}
