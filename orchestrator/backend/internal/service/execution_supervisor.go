package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	commonapi "github.com/addp/common/api"
	execution "github.com/addp/common/execution"
	"github.com/google/uuid"
)

// The budget limits concurrent advances, not the number of parents waiting in PostgreSQL.
// A waiting parent therefore cannot consume the capacity needed by its nested child.
type ExecutionSupervisorConfig struct {
	Concurrency       int
	BatchSize         int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	PollInterval      time.Duration
}

func DefaultExecutionSupervisorConfig() ExecutionSupervisorConfig {
	return ExecutionSupervisorConfig{Concurrency: 4, BatchSize: 100, LeaseDuration: 2 * time.Minute, HeartbeatInterval: 20 * time.Second, PollInterval: time.Second}
}

type ExecutionSupervisor struct {
	service  *ExecutionService
	executor *Executor
	config   ExecutionSupervisorConfig
	owner    string
	logger   *slog.Logger
	cursor   int64
}

func NewExecutionSupervisor(service *ExecutionService, executor *Executor, config ExecutionSupervisorConfig) (*ExecutionSupervisor, error) {
	if service == nil || executor == nil || config.Concurrency < 1 || config.BatchSize < 1 || config.BatchSize > 1000 || config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration || config.PollInterval <= 0 {
		return nil, fmt.Errorf("invalid orchestration supervisor configuration")
	}
	return &ExecutionSupervisor{service: service, executor: executor, config: config, owner: "orchestrator-" + uuid.NewString(), logger: slog.Default()}, nil
}

func (s *ExecutionSupervisor) Run(ctx context.Context, canClaim func() bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() { defer heartbeat.Done(); s.heartbeatLoop(ctx, cancel) }()
	defer heartbeat.Wait()
	defer s.stopOwned()
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if canClaim == nil || canClaim() {
			s.pass(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *ExecutionSupervisor) pass(ctx context.Context) {
	if _, err := s.service.RecoverExpired(ctx, time.Now().UTC(), s.config.BatchSize); err != nil {
		s.logger.Warn("编排失联执行收敛失败")
		return
	}
	active, err := s.service.OwnedPage(ctx, s.owner, s.cursor, s.config.BatchSize)
	if err != nil {
		s.logger.Warn("读取编排运行队列失败")
		return
	}
	if len(active) == 0 {
		s.cursor = 0
	} else {
		s.cursor = active[len(active)-1].ID
	}
	leases := make([]execution.Lease, 0, len(active)+s.config.Concurrency)
	for _, item := range active {
		lease, err := execution.LeaseFromExecution(item)
		if err == nil {
			leases = append(leases, lease)
		}
	}
	for i := 0; i < s.config.Concurrency && ctx.Err() == nil; i++ {
		item, lease, err := s.service.ClaimNext(ctx, s.owner, s.config.LeaseDuration)
		if err != nil {
			s.logger.Warn("领取编排执行失败")
			break
		}
		if item == nil {
			break
		}
		leases = append(leases, *lease)
	}
	for offset := 0; offset < len(leases) && ctx.Err() == nil; offset += s.config.Concurrency {
		var group sync.WaitGroup
		end := offset + s.config.Concurrency
		if end > len(leases) {
			end = len(leases)
		}
		for _, lease := range leases[offset:end] {
			group.Add(1)
			go func(lease execution.Lease) {
				defer group.Done()
				item, err := s.service.OwnedExecution(ctx, lease)
				if err != nil {
					return
				}
				// Also cancel at the observed lease deadline: ownership cannot outlive the lease.
				advanceContext, cancel := context.WithDeadline(ctx, *item.LeaseExpiresAt)
				defer cancel()
				if err := s.executor.Advance(advanceContext, lease); err != nil && ctx.Err() == nil {
					s.logger.Warn("推进编排执行失败", "execution_id", lease.ExecutionID)
				}
			}(lease)
		}
		group.Wait()
	}
}

func (s *ExecutionSupervisor) heartbeatLoop(ctx context.Context, stop context.CancelFunc) {
	ticker := time.NewTicker(s.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		after := int64(0)
		for ctx.Err() == nil {
			items, err := s.service.OwnedPage(ctx, s.owner, after, s.config.BatchSize)
			if err != nil {
				s.logger.Warn("读取编排续租队列失败")
				stop()
				return
			}
			if len(items) == 0 {
				break
			}
			for _, item := range items {
				lease, err := execution.LeaseFromExecution(item)
				if err == nil {
					err = execution.RenewLease(ctx, s.service.db, lease, time.Now().UTC().Add(s.config.LeaseDuration))
				}
				if err != nil && ctx.Err() == nil {
					// Completion may race renewal. A proven terminal row needs no lease.
					if errors.Is(err, commonapi.ErrConflict) {
						terminal, readErr := execution.AttemptIsTerminal(ctx, s.service.db, lease)
						if readErr == nil && terminal {
							continue
						}
					}
					s.logger.Warn("编排续租失败", "execution_id", item.ExecutionID)
					stop()
					return
				}
			}
			after = items[len(items)-1].ID
			if len(items) < s.config.BatchSize {
				break
			}
		}
	}
}

func (s *ExecutionSupervisor) stopOwned() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	after := int64(0)
	for ctx.Err() == nil {
		items, err := s.service.OwnedPage(ctx, s.owner, after, s.config.BatchSize)
		if err != nil || len(items) == 0 {
			return
		}
		for _, item := range items {
			lease, err := execution.LeaseFromExecution(item)
			if err != nil {
				continue
			}
			code := "orchestrator.execution.coordinator_stopped"
			results, _ := readStepResults(item.Metadata)
			for _, result := range results {
				if result.Phase == "dispatching" {
					code = "orchestrator.execution.dispatch_uncertain"
				}
			}
			if err := s.service.FinishExecution(ctx, lease, "failed", code); err != nil {
				s.logger.Warn("编排停机收敛失败", "execution_id", item.ExecutionID)
			}
		}
		after = items[len(items)-1].ID
	}
}
