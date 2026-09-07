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
	IdleMaxInterval   time.Duration
	TaskTypes         []string
}

type BoundedExecutionQueue interface {
	ClaimNext(context.Context, []string, string, time.Time, time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, error)
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
	wake       chan struct{}
}

func NewBoundedExecutionSupervisor(queue BoundedExecutionQueue, dispatcher BoundedExecutionDispatcher, config BoundedExecutionSupervisorConfig, logger *slog.Logger) (*BoundedExecutionSupervisor, error) {
	if queue == nil || dispatcher == nil {
		return nil, fmt.Errorf("Manager bounded execution queue and dispatcher are required")
	}
	if config.InstanceID == "" || config.Concurrency <= 0 || config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 || config.ClaimInterval <= 0 || config.IdleMaxInterval < config.ClaimInterval || config.HeartbeatInterval >= config.LeaseDuration || len(config.TaskTypes) == 0 {
		return nil, fmt.Errorf("Manager bounded execution supervisor config is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &BoundedExecutionSupervisor{queue: queue, dispatcher: dispatcher, config: config, logger: logger, wake: make(chan struct{}, 1)}, nil
}

func (s *BoundedExecutionSupervisor) ActiveCount() int { return int(s.active.Load()) }

// Notify interrupts idle backoff after this Manager process has committed a
// pending execution. The bounded poll remains the cross-instance fallback.
func (s *BoundedExecutionSupervisor) Notify() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *BoundedExecutionSupervisor) Run(ctx context.Context, canClaim func() bool) {
	var group sync.WaitGroup
	group.Add(1)
	go func() { defer group.Done(); s.recoveryLoop(ctx, canClaim) }()

	availableSlots := make(chan int, s.config.Concurrency)
	for slot := 1; slot <= s.config.Concurrency; slot++ {
		availableSlots <- slot
	}
	idleInterval := s.config.ClaimInterval
	for ctx.Err() == nil {
		worked := false
		if canClaim == nil || canClaim() {
			worked = s.claimAvailable(ctx, availableSlots, &group)
		}
		if ctx.Err() != nil {
			break
		}
		if worked {
			idleInterval = s.config.ClaimInterval
			continue
		}
		if len(availableSlots) == 0 {
			select {
			case <-ctx.Done():
			case <-s.wake:
			}
			continue
		}

		timer := time.NewTimer(idleInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
		case <-s.wake:
			if !timer.Stop() {
				<-timer.C
			}
			idleInterval = s.config.ClaimInterval
		case <-timer.C:
			idleInterval = nextIdleInterval(idleInterval, s.config.IdleMaxInterval)
		}
	}
	group.Wait()
}

func (s *BoundedExecutionSupervisor) claimAvailable(ctx context.Context, availableSlots chan int, group *sync.WaitGroup) bool {
	worked := false
	for {
		select {
		case slot := <-availableSlots:
			owner := fmt.Sprintf("%s-%d", s.config.InstanceID, slot)
			execution, lease, err := s.queue.ClaimNext(ctx, s.config.TaskTypes, owner, time.Now().UTC(), s.config.LeaseDuration)
			if err != nil {
				availableSlots <- slot
				if ctx.Err() == nil {
					s.logger.Error("claim Manager bounded execution failed", "owner", owner, "error", err)
				}
				return worked
			}
			if execution == nil || lease == nil {
				availableSlots <- slot
				return worked
			}
			worked = true
			s.active.Add(1)
			group.Add(1)
			go func() {
				defer group.Done()
				defer s.active.Add(-1)
				defer func() {
					availableSlots <- slot
					s.Notify()
				}()
				s.processClaimed(ctx, execution, *lease)
			}()
		default:
			return worked
		}
	}
}

func (s *BoundedExecutionSupervisor) processClaimed(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease) {
	execCtx, cancel := context.WithCancel(commonExecution.ContextWithLease(ctx, lease))
	heartbeatDone := make(chan error, 1)
	go s.heartbeat(execCtx, cancel, lease, heartbeatDone)
	runErr := s.dispatcher.RunClaimedExecution(execCtx, execution, lease)
	terminal, terminalErr := s.queue.AttemptIsTerminal(context.Background(), lease)
	if terminalErr != nil {
		s.logger.Error("read Manager bounded execution terminal state failed", "execution_id", execution.ExecutionID, "error", terminalErr)
	} else if !terminal {
		code := "manager.execution.incomplete"
		message := "Manager bounded execution returned without reaching a terminal state"
		if runErr != nil {
			code = "manager.execution.dispatch_failed"
			message = runErr.Error()
		}
		if failErr := s.queue.FailClaimed(context.Background(), execution, lease, code, message, time.Now().UTC()); failErr != nil {
			s.logger.Error("converge Manager bounded execution failure failed", "execution_id", execution.ExecutionID, "error", failErr)
		}
	}
	cancel()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil {
		s.logger.Error("Manager bounded execution lease lost", "execution_id", execution.ExecutionID, "error", heartbeatErr)
	}
}

func nextIdleInterval(current, maximum time.Duration) time.Duration {
	if current >= maximum || current > maximum/2 {
		return maximum
	}
	return current * 2
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
