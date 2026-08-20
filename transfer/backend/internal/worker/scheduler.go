package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/google/uuid"
)

const schedulePollInterval = time.Second

// Scheduler 按 transfer.transfer_tasks.next_run_at 触发定时传输任务。
type Scheduler struct {
	taskRepo         *repository.TaskRepository
	executionService *service.ExecutionService

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewScheduler 创建定时调度器。
func NewScheduler(taskRepo *repository.TaskRepository) *Scheduler {
	return &Scheduler{
		taskRepo: taskRepo,
		stopCh:   make(chan struct{}),
	}
}

// SetExecutionService 设置执行服务（在创建后注入）。
func (s *Scheduler) SetExecutionService(executionService *service.ExecutionService) {
	s.executionService = executionService
}

// Start 启动调度器。
func (s *Scheduler) Start(ctx context.Context) error {
	if s == nil || s.taskRepo == nil {
		return nil
	}
	if err := s.ensureScheduledTaskNextRuns(ctx); err != nil {
		return fmt.Errorf("初始化定时任务 next_run_at 失败: %w", err)
	}

	s.wg.Add(1)
	go s.scheduledLoop(ctx)
	log.Println("✅ Transfer 定时调度器已启动")
	return nil
}

// Stop 停止调度器。
func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
	log.Println("🛑 Transfer 定时调度器已停止")
}

func (s *Scheduler) ensureScheduledTaskNextRuns(ctx context.Context) error {
	tasks, err := s.taskRepo.ListScheduledTasksMissingNextRun(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for i := range tasks {
		task := tasks[i]
		next, err := service.NextTransferRunAtForScheduler(task.Schedule, now)
		if err != nil {
			log.Printf("⚠️  计算定时任务下次执行时间失败 - TaskID: %d, Error: %v", task.ID, err)
			continue
		}
		if err := s.taskRepo.UpdateNextRunAt(ctx, task.ID, next); err != nil {
			log.Printf("⚠️  初始化定时任务 next_run_at 失败 - TaskID: %d, Error: %v", task.ID, err)
		}
	}
	return nil
}

func (s *Scheduler) scheduledLoop(ctx context.Context) {
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

func (s *Scheduler) runDueScheduledTasks(ctx context.Context) {
	if s.executionService == nil {
		log.Println("⚠️  Transfer 定时调度器缺少 executionService")
		return
	}

	now := time.Now()
	taskIDs, err := s.taskRepo.ListDueScheduledTaskIDs(ctx, now, 100)
	if err != nil {
		log.Printf("⚠️  查询到期定时任务失败: %v", err)
		return
	}
	for _, taskID := range taskIDs {
		if err := s.claimAndExecuteDueTask(ctx, taskID, now); err != nil {
			log.Printf("⚠️  触发定时任务失败 - TaskID: %d, Error: %v", taskID, err)
		}
	}
}

func (s *Scheduler) claimAndExecuteDueTask(ctx context.Context, taskID uint, now time.Time) error {
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil || task == nil {
		return err
	}
	next, err := service.NextTransferRunAtForScheduler(task.Schedule, now)
	if err != nil {
		return err
	}
	executionID := uuid.New().String()
	taskName := task.Name
	execution := &commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: executionID, Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), SourceTaskName: &taskName,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeScheduled,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		ExecutionConfig:   task.Config, CreatedAt: now, UpdatedAt: now,
	}
	claimed, _, err := s.taskRepo.ClaimDueScheduledExecution(ctx, taskID, task.Schedule, now, next, execution, service.IncrementalSourceIdentityForTask(task))
	if err != nil || claimed == nil {
		return err
	}

	log.Printf("✅ 定时任务已创建 pending execution - TaskID: %d, ExecutionID: %d", claimed.ID, execution.ID)
	return nil
}
