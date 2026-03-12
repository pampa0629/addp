package service

import (
	"context"
	"fmt"

	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/orchestrator/internal/repository"
)

// Scheduler 定时调度器（使用公共调度模块）
type Scheduler struct {
	scheduler        commonScheduler.Scheduler // 公共调度器
	orchRepo         *repository.OrchestrationRepository
	executionService *ExecutionService
	executor         *Executor
}

// NewScheduler 创建调度器
func NewScheduler(
	orchRepo *repository.OrchestrationRepository,
	executionService *ExecutionService,
	executor *Executor,
) *Scheduler {
	// 创建公共调度器
	scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
		Name: "orchestrator",
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create scheduler: %v", err))
	}

	return &Scheduler{
		scheduler:        scheduler,
		orchRepo:         orchRepo,
		executionService: executionService,
		executor:         executor,
	}
}

// Start 启动调度器
func (s *Scheduler) Start() error {
	ctx := context.Background()

	// 手动加载已启用的编排任务
	orchestrations, err := s.orchRepo.ListEnabled()
	if err != nil {
		return err
	}

	for _, orch := range orchestrations {
		if orch.Schedule != "" {
			handler := s.createHandler(orch.ID)
			if err := s.scheduler.Schedule(ctx, fmt.Sprintf("%d", orch.ID), orch.Schedule, handler); err != nil {
				// 记录错误但继续启动其他任务
				fmt.Printf("Failed to schedule orchestration %d: %v\n", orch.ID, err)
			}
		}
	}

	// 启动调度器
	return s.scheduler.Start(ctx)
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	ctx := context.Background()
	s.scheduler.Stop(ctx)
}

// Schedule 调度编排任务
func (s *Scheduler) Schedule(orchID uint, cronExpr string) error {
	ctx := context.Background()
	handler := s.createHandler(orchID)
	return s.scheduler.Schedule(ctx, fmt.Sprintf("%d", orchID), cronExpr, handler)
}

// Unschedule 取消调度
func (s *Scheduler) Unschedule(orchID uint) {
	ctx := context.Background()
	s.scheduler.Unschedule(ctx, fmt.Sprintf("%d", orchID))
}

// createHandler 创建任务处理器
func (s *Scheduler) createHandler(orchID uint) commonScheduler.TaskHandler {
	return func(ctx context.Context, taskID string) error {
		s.triggerOrchestration(orchID)
		return nil
	}
}

// triggerOrchestration 触发编排执行
func (s *Scheduler) triggerOrchestration(orchID uint) {
	ctx := context.Background()
	orch, err := s.orchRepo.GetByID(orchID)
	if err != nil || !orch.Enabled {
		return
	}

	// 使用统一执行服务创建执行记录
	execution, err := s.executionService.CreateExecution(ctx, orchID, orch.TenantID)
	if err != nil {
		fmt.Printf("Failed to create execution for orchestration %d: %v\n", orchID, err)
		return
	}

	s.executor.ExecuteAsync(uint(execution.ID))
}
