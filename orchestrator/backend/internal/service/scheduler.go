package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/orchestrator/internal/repository"
)

const orchestrationSchedulePollInterval = time.Minute

// Scheduler 按 orchestrator.orchestrations.next_run_at 触发编排定时执行。
type Scheduler struct {
	orchRepo         *repository.OrchestrationRepository
	executionService *ExecutionService
	systemClient     *commonClient.SystemServiceClient
	log              *slog.Logger
	claimGate        func() bool

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func (s *Scheduler) SetClaimGate(claimGate func() bool) {
	s.claimGate = claimGate
}

// NewScheduler 创建调度器。
func NewScheduler(
	orchRepo *repository.OrchestrationRepository,
	executionService *ExecutionService,
	systemClient *commonClient.SystemServiceClient,
) *Scheduler {
	return &Scheduler{
		orchRepo:         orchRepo,
		executionService: executionService,
		systemClient:     systemClient,
		log:              logger.With("component", "orchestrator_scheduler"),
		stopCh:           make(chan struct{}),
	}
}

// Start 启动调度器。
func (s *Scheduler) Start(ctx context.Context) error {
	if err := s.ensureNextRuns(ctx); err != nil {
		return err
	}
	s.wg.Add(1)
	go s.scheduledLoop(ctx)
	s.log.Info("orchestrator scheduler started")
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
	s.log.Info("orchestrator scheduler stopped")
}

func (s *Scheduler) ensureNextRuns(ctx context.Context) error {
	orchestrations, err := s.orchRepo.ListMissingNextRun(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for i := range orchestrations {
		orch := orchestrations[i]
		if err := ApplyOrchestrationSchedule(&orch, now); err != nil {
			s.log.Warn("calculate orchestration next_run_at failed", "orchestration_id", orch.ID, "error", err)
			continue
		}
		if err := s.orchRepo.UpdateNextRunAt(ctx, orch.ID, orch.NextRunAt); err != nil {
			s.log.Warn("update orchestration next_run_at failed", "orchestration_id", orch.ID, "error", err)
		}
	}
	return nil
}

func (s *Scheduler) scheduledLoop(ctx context.Context) {
	defer s.wg.Done()

	s.runDue(ctx)
	ticker := time.NewTicker(orchestrationSchedulePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDue(ctx)
		}
	}
}

func (s *Scheduler) runDue(ctx context.Context) {
	if s.claimGate != nil && !s.claimGate() {
		return
	}
	now := time.Now()
	ids, err := s.orchRepo.ListDueIDs(ctx, now, 100)
	if err != nil {
		s.log.Warn("list due orchestrations failed", "error", err)
		return
	}
	for _, id := range ids {
		if err := s.claimAndExecute(ctx, id, now); err != nil {
			s.log.Warn("trigger scheduled orchestration failed", "orchestration_id", id, "error", err)
		}
	}
}

func (s *Scheduler) claimAndExecute(ctx context.Context, id uint, now time.Time) error {
	orch, err := s.orchRepo.GetByIDInternal(id)
	if err != nil || orch == nil {
		return err
	}
	if !orch.Enabled {
		return nil
	}
	nextOrch := *orch
	if err := ApplyOrchestrationSchedule(&nextOrch, now); err != nil {
		return err
	}
	if s.systemClient == nil || orch.AuthorizationSubjectID == nil || orch.AuthorizationRef == nil ||
		orch.AuthorizationDefinitionHash == nil {
		return fmt.Errorf("orchestration task authorization subject is required")
	}
	subject, err := s.systemClient.WithTenantID(orch.TenantID).ResolveTaskAuthorizationSubject(
		ctx,
		fmt.Sprintf("%d", *orch.AuthorizationSubjectID),
		commonClient.TaskAuthorizationSubjectRequest{
			OwnerModule:    commonExecution.ModuleOrchestrator,
			TaskType:       commonExecution.TaskTypeOrchestration,
			TaskRef:        orch.AuthorizationRef.String(),
			DefinitionHash: *orch.AuthorizationDefinitionHash,
		},
	)
	if err != nil {
		return fmt.Errorf("resolve scheduled task authorization subject: %w", err)
	}
	principalID, err := parsePositiveActorID(subject.PrincipalID)
	if err != nil {
		return err
	}
	membershipID, err := parsePositiveActorID(subject.TenantMembershipID)
	if err != nil {
		return err
	}
	authorizationVersion, err := parsePositiveActorID(subject.AuthorizationVersion)
	if err != nil {
		return err
	}

	_, err = s.executionService.CreateScheduled(
		ctx, orch, now, nextOrch.NextRunAt, ExecutionActor{
			PrincipalID: principalID, TenantMembershipID: membershipID,
			AuthorizationVersion: authorizationVersion,
		},
	)
	if err != nil {
		return fmt.Errorf("create scheduled orchestration execution: %w", err)
	}
	return nil
}

func parsePositiveActorID(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, fmt.Errorf("invalid task authorization actor ID")
	}
	return parsed, nil
}
