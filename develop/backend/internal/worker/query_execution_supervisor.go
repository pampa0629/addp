package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/repository"
)

type QueryExecutionSupervisorConfig struct {
	InstanceID         string
	ResolveConcurrency func(context.Context) (int, int, error)
	LeaseDuration      time.Duration
	HeartbeatInterval  time.Duration
	ClaimInterval      time.Duration
}

type QueryExecutionSupervisor struct {
	queries *repository.QueryExecutionRepository
	service interface {
		Execute(context.Context, *commonExecution.TaskExecution, commonExecution.Lease) error
	}
	config          QueryExecutionSupervisorConfig
	logger          *slog.Logger
	active          atomic.Int64
	capacity        atomic.Int64
	nextOwner       uint64
	wake            chan struct{}
	activeEnginesMu sync.Mutex
	activeEngines   map[uint]int
}

func NewQueryExecutionSupervisor(
	queries *repository.QueryExecutionRepository,
	queryService interface {
		Execute(context.Context, *commonExecution.TaskExecution, commonExecution.Lease) error
	},
	config QueryExecutionSupervisorConfig,
	logger *slog.Logger,
) (*QueryExecutionSupervisor, error) {
	if queries == nil || queryService == nil {
		return nil, fmt.Errorf("Develop Query Execution Supervisor dependencies are required")
	}
	if config.InstanceID == "" || config.ResolveConcurrency == nil || config.LeaseDuration <= 0 ||
		config.HeartbeatInterval <= 0 || config.ClaimInterval <= 0 ||
		config.HeartbeatInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("Develop Query Execution Supervisor config is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &QueryExecutionSupervisor{
		queries: queries, service: queryService, config: config, logger: logger,
		wake: make(chan struct{}, 1), activeEngines: make(map[uint]int),
	}, nil
}

func (s *QueryExecutionSupervisor) Capacity() int { return int(s.capacity.Load()) }

func (s *QueryExecutionSupervisor) ActiveCount() int { return int(s.active.Load()) }

// Notify wakes scheduling after a local query or policy change. PostgreSQL
// polling discovers changes committed by other Backend instances.
func (s *QueryExecutionSupervisor) Notify() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *QueryExecutionSupervisor) Run(ctx context.Context, canClaim func() bool) {
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		s.recoveryLoop(ctx, canClaim)
	}()

	for ctx.Err() == nil {
		if canClaim == nil || canClaim() {
			s.claimAvailable(ctx, &group)
		}
		timer := time.NewTimer(s.config.ClaimInterval)
		select {
		case <-ctx.Done():
		case <-s.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
	group.Wait()
}

// Limits are read as one version before each scheduling pass. Running queries
// keep their slots when limits shrink; only subsequent claims are throttled.
func (s *QueryExecutionSupervisor) claimAvailable(ctx context.Context, group *sync.WaitGroup) {
	concurrency, perEngine, err := s.config.ResolveConcurrency(ctx)
	if err != nil || concurrency <= 0 || perEngine <= 0 || perEngine > concurrency {
		if ctx.Err() == nil {
			s.logger.Error("read Develop query concurrency failed", "error", err)
		}
		return
	}
	s.capacity.Store(int64(concurrency))
	// Bound each pass so a continuous stream of fast queries cannot postpone
	// reloading a changed policy indefinitely.
	for claimed := 0; claimed < concurrency && ctx.Err() == nil && s.ActiveCount() < concurrency; claimed++ {
		s.nextOwner++
		owner := fmt.Sprintf("%s-%d", s.config.InstanceID, s.nextOwner)
		execution, lease, err := s.queries.ClaimNext(ctx, owner, time.Now().UTC(), s.config.LeaseDuration, s.saturatedEngineIDs(perEngine))
		if err != nil {
			if ctx.Err() == nil {
				s.logger.Error("claim Develop query execution failed", "error", err)
			}
			return
		}
		if execution == nil || lease == nil {
			return
		}
		engineID, hasEngineID := queryExecutionEngineID(execution)
		if hasEngineID {
			s.reserveEngine(engineID)
		}
		s.active.Add(1)
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() {
				if hasEngineID {
					s.releaseEngine(engineID)
				}
				s.active.Add(-1)
				s.Notify()
			}()
			s.processClaimed(ctx, execution, *lease)
		}()
	}
}

func (s *QueryExecutionSupervisor) saturatedEngineIDs(limit int) []uint {
	s.activeEnginesMu.Lock()
	defer s.activeEnginesMu.Unlock()
	engineIDs := make([]uint, 0, len(s.activeEngines))
	for engineID, count := range s.activeEngines {
		if count >= limit {
			engineIDs = append(engineIDs, engineID)
		}
	}
	return engineIDs
}

func (s *QueryExecutionSupervisor) reserveEngine(engineID uint) {
	s.activeEnginesMu.Lock()
	defer s.activeEnginesMu.Unlock()
	s.activeEngines[engineID]++
}

func (s *QueryExecutionSupervisor) releaseEngine(engineID uint) {
	s.activeEnginesMu.Lock()
	defer s.activeEnginesMu.Unlock()
	if s.activeEngines[engineID] <= 1 {
		delete(s.activeEngines, engineID)
		return
	}
	s.activeEngines[engineID]--
}

func queryExecutionEngineID(execution *commonExecution.TaskExecution) (uint, bool) {
	if execution == nil {
		return 0, false
	}
	value, ok := execution.ExecutionConfig.GetInt("engine_id")
	if !ok || value <= 0 {
		return 0, false
	}
	return uint(value), true
}

func (s *QueryExecutionSupervisor) processClaimed(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease) {
	execCtx, cancel := context.WithCancel(commonExecution.ContextWithLease(ctx, lease))
	heartbeatDone := make(chan error, 1)
	go s.heartbeat(execCtx, cancel, lease, heartbeatDone)
	runErr := s.service.Execute(execCtx, execution, lease)

	terminal, terminalErr := s.queries.AttemptIsTerminal(context.Background(), lease)
	if terminalErr != nil {
		s.logger.Error("read Develop query terminal state failed", "execution_id", execution.ExecutionID, "error", terminalErr)
	} else if !terminal {
		code := "develop.query.execution_incomplete"
		message := "Develop query execution returned without reaching a terminal state"
		if runErr != nil {
			code = "develop.query.execution_failed"
			message = runErr.Error()
		}
		if err := s.queries.FailClaimed(context.Background(), execution, lease, code, message, time.Now().UTC()); err != nil {
			s.logger.Error("converge Develop query failure failed", "execution_id", execution.ExecutionID, "error", err)
		}
	}
	cancel()
	if heartbeatErr := <-heartbeatDone; heartbeatErr != nil {
		s.logger.Error("Develop query lease lost", "execution_id", execution.ExecutionID, "error", heartbeatErr)
	}
}

func (s *QueryExecutionSupervisor) heartbeat(ctx context.Context, cancel context.CancelFunc, lease commonExecution.Lease, done chan<- error) {
	ticker := time.NewTicker(s.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case now := <-ticker.C:
			if err := s.queries.Renew(ctx, lease, now.UTC().Add(s.config.LeaseDuration)); err != nil {
				terminal, stateErr := s.queries.AttemptIsTerminal(context.Background(), lease)
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

func (s *QueryExecutionSupervisor) recoveryLoop(ctx context.Context, canRecover func() bool) {
	interval := s.config.ClaimInterval
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if canRecover == nil || canRecover() {
			now := time.Now().UTC()
			if count, err := s.queries.RecoverUnleased(ctx, now, 100); err != nil {
				if ctx.Err() == nil {
					s.logger.Error("recover unleased Develop queries failed", "error", err)
				}
			} else if count > 0 {
				s.logger.Warn("unleased Develop queries converged", "count", count)
			}
			if count, err := s.queries.RecoverExpired(ctx, now, 100); err != nil {
				if ctx.Err() == nil {
					s.logger.Error("recover expired Develop queries failed", "error", err)
				}
			} else if count > 0 {
				s.logger.Warn("expired Develop queries converged", "count", count)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
