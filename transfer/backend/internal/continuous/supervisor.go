package continuous

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
)

type SessionRunner interface {
	Run(ctx context.Context, claim repository.RuntimeLeaseClaim) error
}

type SessionRunnerFunc func(context.Context, repository.RuntimeLeaseClaim) error

func (f SessionRunnerFunc) Run(ctx context.Context, claim repository.RuntimeLeaseClaim) error {
	return f(ctx, claim)
}

type LeaseStore interface {
	ClaimNext(ctx context.Context, owner string, now time.Time, duration time.Duration) (*repository.RuntimeLeaseClaim, error)
	Renew(ctx context.Context, taskID uint, owner string, token uint64, now time.Time, duration time.Duration) error
	Finish(ctx context.Context, claim repository.RuntimeLeaseClaim, status, stopReason, errorMessage string, now time.Time) error
	DesiredState(ctx context.Context, taskID uint) (models.TaskDesiredState, error)
}

type Config struct {
	OwnerInstanceID   string
	Capacity          int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	ClaimInterval     time.Duration
}

func (c Config) Validate() error {
	if c.OwnerInstanceID == "" {
		return fmt.Errorf("continuous worker owner instance id is required")
	}
	if c.Capacity <= 0 {
		return fmt.Errorf("continuous worker capacity must be greater than zero")
	}
	if c.LeaseDuration <= 0 || c.HeartbeatInterval <= 0 || c.ClaimInterval <= 0 {
		return fmt.Errorf("continuous worker durations must be greater than zero")
	}
	if c.HeartbeatInterval >= c.LeaseDuration/2 {
		return fmt.Errorf("continuous heartbeat interval must be less than half the lease duration")
	}
	return nil
}

type Supervisor struct {
	repo   LeaseStore
	runner SessionRunner
	cfg    Config
	logger *slog.Logger

	mu     sync.Mutex
	active map[uint]context.CancelFunc
	wg     sync.WaitGroup
}

func NewSupervisor(repo LeaseStore, runner SessionRunner, cfg Config, logger *slog.Logger) (*Supervisor, error) {
	if repo == nil || runner == nil {
		return nil, fmt.Errorf("continuous supervisor repository and runner are required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{repo: repo, runner: runner, cfg: cfg, logger: logger, active: map[uint]context.CancelFunc{}}, nil
}

func (s *Supervisor) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.ClaimInterval)
	defer ticker.Stop()
	for {
		if err := s.claimAvailable(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Error("continuous claim failed", "error", err)
		}
		select {
		case <-ctx.Done():
			s.stopAll()
			s.wg.Wait()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) claimAvailable(ctx context.Context) error {
	for s.activeCount() < s.cfg.Capacity {
		claim, err := s.repo.ClaimNext(ctx, s.cfg.OwnerInstanceID, time.Now(), s.cfg.LeaseDuration)
		if err != nil {
			return err
		}
		if claim == nil {
			return nil
		}
		sessionCtx, cancel := context.WithCancel(ctx)
		s.mu.Lock()
		if _, exists := s.active[claim.Task.ID]; exists {
			s.mu.Unlock()
			cancel()
			return fmt.Errorf("task %d already active in this supervisor", claim.Task.ID)
		}
		s.active[claim.Task.ID] = cancel
		s.mu.Unlock()
		s.wg.Add(1)
		go s.runSession(sessionCtx, *claim)
	}
	return nil
}

func (s *Supervisor) runSession(ctx context.Context, claim repository.RuntimeLeaseClaim) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.active, claim.Task.ID)
		s.mu.Unlock()
	}()

	runnerDone := make(chan error, 1)
	runnerCtx, cancelRunner := context.WithCancel(ctx)
	defer cancelRunner()
	go func() { runnerDone <- s.runner.Run(runnerCtx, claim) }()

	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()
	status := commonExecution.ExecutionStatusFailed
	stopReason := ""
	errorMessage := ""
	for {
		select {
		case err := <-runnerDone:
			if err == nil {
				errorMessage = "continuous session ended unexpectedly"
			} else if errors.Is(err, context.Canceled) {
				status, stopReason = commonExecution.ExecutionStatusCancelled, "worker_shutdown"
			} else if reason, cancelled := s.desiredStateCancellationReason(claim.Task.ID); cancelled {
				status, stopReason = commonExecution.ExecutionStatusCancelled, reason
			} else {
				errorMessage = err.Error()
				var schemaErr *SchemaChangeError
				if errors.As(err, &schemaErr) {
					stopReason = "schema_change_blocked"
				}
			}
			goto finish
		case <-ctx.Done():
			cancelRunner()
			<-runnerDone
			status, stopReason = commonExecution.ExecutionStatusCancelled, "worker_shutdown"
			goto finish
		case now := <-ticker.C:
			if err := s.repo.Renew(ctx, claim.Task.ID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken, now, s.cfg.LeaseDuration); err != nil {
				cancelRunner()
				<-runnerDone
				if reason, cancelled := s.desiredStateCancellationReason(claim.Task.ID); cancelled {
					status = commonExecution.ExecutionStatusCancelled
					stopReason = reason
				} else {
					errorMessage = err.Error()
				}
				goto finish
			}
		}
	}

finish:
	if err := s.repo.Finish(context.Background(), claim, status, stopReason, errorMessage, time.Now()); err != nil && !errors.Is(err, repository.ErrRuntimeLeaseLost) {
		s.logger.Error("continuous session finish failed", "task_id", claim.Task.ID, "error", err)
	}
}

func (s *Supervisor) desiredStateCancellationReason(taskID uint) (string, bool) {
	state, err := s.repo.DesiredState(context.Background(), taskID)
	if err != nil || state == models.TaskDesiredStateRunning {
		return "", false
	}
	return string(state), true
}

func (s *Supervisor) activeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.active)
}

func (s *Supervisor) ActiveCount() int { return s.activeCount() }

func (s *Supervisor) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.active {
		cancel()
	}
}
