package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	commonExecution "github.com/addp/common/execution"
)

type BoundedExecutionSupervisorConfig struct {
	InstanceID        string
	Concurrency       int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	ClaimInterval     time.Duration
	TaskTypes         []string
}

type BoundedExecutionQueue interface {
	ClaimNext(context.Context, string, string, time.Time, time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, error)
	RenewLease(context.Context, commonExecution.Lease, time.Time) error
	AttemptIsTerminal(context.Context, commonExecution.Lease) (bool, error)
	FailClaimed(context.Context, *commonExecution.TaskExecution, commonExecution.Lease, string, string, time.Time) error
	RecoverUnleased(context.Context, time.Time, int) (int, error)
	RecoverExpired(context.Context, time.Time, int) (int, error)
}

type BoundedExecutionDispatcher interface {
	RunClaimedExecution(context.Context, *commonExecution.TaskExecution, commonExecution.Lease) error
}

type BoundedExecutionSupervisor struct {
	queue      BoundedExecutionQueue
	dispatcher BoundedExecutionDispatcher
	config     BoundedExecutionSupervisorConfig
	logger     *slog.Logger
	active     atomic.Int64
}

func NewBoundedExecutionSupervisor(queue BoundedExecutionQueue, dispatcher BoundedExecutionDispatcher, config BoundedExecutionSupervisorConfig, logger *slog.Logger) (*BoundedExecutionSupervisor, error) {
	if queue == nil || dispatcher == nil {
		return nil, fmt.Errorf("Manager bounded execution queue and dispatcher are required")
	}
	if config.InstanceID == "" || config.Concurrency <= 0 || config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 || config.ClaimInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration || len(config.TaskTypes) == 0 {
		return nil, fmt.Errorf("Manager bounded execution supervisor config is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &BoundedExecutionSupervisor{queue: queue, dispatcher: dispatcher, config: config, logger: logger}, nil
}

func (s *BoundedExecutionSupervisor) ActiveCount() int { return int(s.active.Load()) }

func (s *BoundedExecutionSupervisor) Run(ctx context.Context, canClaim func() bool) {
	var group sync.WaitGroup
	group.Add(1)
	go func() { defer group.Done(); s.recoveryLoop(ctx, canClaim) }()
	for slot := 1; slot <= s.config.Concurrency; slot++ {
		slot := slot
		group.Add(1)
		go func() { defer group.Done(); s.claimLoop(ctx, slot, canClaim) }()
	}
	group.Wait()
}

func (s *BoundedExecutionSupervisor) claimLoop(ctx context.Context, slot int, canClaim func() bool) {
	ticker := time.NewTicker(s.config.ClaimInterval)
	defer ticker.Stop()
	nextType := slot % len(s.config.TaskTypes)
	for {
		worked := false
		if canClaim == nil || canClaim() {
			taskType := s.config.TaskTypes[nextType]
			nextType = (nextType + 1) % len(s.config.TaskTypes)
			worked = s.processNext(ctx, slot, taskType)
		}
		if ctx.Err() != nil {
			return
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *BoundedExecutionSupervisor) processNext(ctx context.Context, slot int, taskType string) bool {
	owner := fmt.Sprintf("%s-%d", s.config.InstanceID, slot)
	execution, lease, err := s.queue.ClaimNext(ctx, taskType, owner, time.Now().UTC(), s.config.LeaseDuration)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("claim Manager bounded execution failed", "task_type", taskType, "owner", owner, "error", err)
		}
		return false
	}
	if execution == nil || lease == nil {
		return false
	}
	s.active.Add(1)
	defer s.active.Add(-1)

	execCtx, cancel := context.WithCancel(commonExecution.ContextWithLease(ctx, *lease))
	heartbeatDone := make(chan error, 1)
	go s.heartbeat(execCtx, cancel, *lease, heartbeatDone)
	runErr := s.dispatcher.RunClaimedExecution(execCtx, execution, *lease)
	terminal, terminalErr := s.queue.AttemptIsTerminal(context.Background(), *lease)
	if terminalErr != nil {
		s.logger.Error("read Manager bounded execution terminal state failed", "execution_id", execution.ExecutionID, "error", terminalErr)
	} else if !terminal {
		code := "manager.execution.incomplete"
		message := "Manager bounded execution returned without reaching a terminal state"
		if runErr != nil {
			code = "manager.execution.dispatch_failed"
			message = runErr.Error()
		}
		if failErr := s.queue.FailClaimed(context.Background(), execution, *lease, code, message, time.Now().UTC()); failErr != nil {
			s.logger.Error("converge Manager bounded execution failure failed", "execution_id", execution.ExecutionID, "error", failErr)
		}
	}
	cancel()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil {
		s.logger.Error("Manager bounded execution lease lost", "execution_id", execution.ExecutionID, "error", heartbeatErr)
	}
	return true
}

func (s *BoundedExecutionSupervisor) heartbeat(ctx context.Context, cancel context.CancelFunc, lease commonExecution.Lease, done chan<- error) {
	ticker := time.NewTicker(s.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case now := <-ticker.C:
			if err := s.queue.RenewLease(ctx, lease, now.UTC().Add(s.config.LeaseDuration)); err != nil {
				terminal, stateErr := s.queue.AttemptIsTerminal(context.Background(), lease)
				if stateErr == nil && terminal {
					done <- nil
					return
				}
				cancel()
				done <- err
				return
			}
		}
	}
}

func (s *BoundedExecutionSupervisor) recoveryLoop(ctx context.Context, canRecover func() bool) {
	recoveryInterval := s.config.ClaimInterval
	if recoveryInterval < 30*time.Second {
		recoveryInterval = 30 * time.Second
	}
	ticker := time.NewTicker(recoveryInterval)
	defer ticker.Stop()
	for {
		if canRecover == nil || canRecover() {
			now := time.Now().UTC()
			unleased, err := s.queue.RecoverUnleased(ctx, now, 100)
			if err != nil {
				if ctx.Err() == nil {
					s.logger.Error("recover unleased Manager bounded executions failed", "error", err)
				}
			} else if unleased > 0 {
				s.logger.Warn("unleased Manager bounded executions converged", "count", unleased)
			}
			if count, err := s.queue.RecoverExpired(ctx, now, 100); err != nil {
				if ctx.Err() == nil {
					s.logger.Error("recover expired Manager bounded executions failed", "error", err)
				}
			} else if count > 0 {
				s.logger.Warn("expired Manager bounded executions converged", "count", count)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
