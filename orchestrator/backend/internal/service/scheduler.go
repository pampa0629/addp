package service

import (
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/robfig/cron/v3"
)

// Scheduler 定时调度器
type Scheduler struct {
	cron     *cron.Cron
	orchRepo *repository.OrchestrationRepository
	execRepo *repository.ExecutionRepository
	executor *Executor
	entryIDs map[uint]cron.EntryID
}

// NewScheduler 创建调度器
func NewScheduler(
	orchRepo *repository.OrchestrationRepository,
	execRepo *repository.ExecutionRepository,
	executor *Executor,
) *Scheduler {
	return &Scheduler{
		cron:     cron.New(),
		orchRepo: orchRepo,
		execRepo: execRepo,
		executor: executor,
		entryIDs: make(map[uint]cron.EntryID),
	}
}

// Start 启动调度器
func (s *Scheduler) Start() error {
	orchestrations, err := s.orchRepo.ListEnabled()
	if err != nil {
		return err
	}

	for _, orch := range orchestrations {
		if orch.CronExpr != "" {
			s.Schedule(orch.ID, orch.CronExpr)
		}
	}

	s.cron.Start()
	return nil
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// Schedule 调度编排任务
func (s *Scheduler) Schedule(orchID uint, cronExpr string) error {
	// 如果已有旧的调度，先移除
	if entryID, ok := s.entryIDs[orchID]; ok {
		s.cron.Remove(entryID)
		delete(s.entryIDs, orchID)
	}

	entryID, err := s.cron.AddFunc(cronExpr, func() {
		s.triggerOrchestration(orchID)
	})
	if err != nil {
		return err
	}

	s.entryIDs[orchID] = entryID
	return nil
}

// Unschedule 取消调度
func (s *Scheduler) Unschedule(orchID uint) {
	if entryID, ok := s.entryIDs[orchID]; ok {
		s.cron.Remove(entryID)
		delete(s.entryIDs, orchID)
	}
}

// triggerOrchestration 触发编排执行
func (s *Scheduler) triggerOrchestration(orchID uint) {
	orch, err := s.orchRepo.GetByID(orchID)
	if err != nil || !orch.Enabled {
		return
	}

	execution := &models.Execution{
		OrchestrationID: orchID,
		TenantID:        orch.TenantID,
		Status:          "pending",
	}

	if err := s.execRepo.Create(execution); err != nil {
		return
	}

	s.executor.ExecuteAsync(execution.ID)
}
