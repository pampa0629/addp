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
	"github.com/addp/develop/backend/internal/service"
)

type QueryExecutionSupervisorConfig struct {
	InstanceID           string
	Concurrency          int
	PerEngineConcurrency int
	LeaseDuration        time.Duration
	HeartbeatInterval    time.Duration
	ClaimInterval        time.Duration
	IdleMaxInterval      time.Duration
}

type QueryExecutionSupervisor struct {
	queries         *repository.QueryExecutionRepository
	service         *service.QueryExecutionService
	config          QueryExecutionSupervisorConfig
	logger          *slog.Logger
	active          atomic.Int64
	wake            chan struct{}
	activeEnginesMu sync.Mutex
	activeEngines   map[uint]int
}

func NewQueryExecutionSupervisor(
	queries *repository.QueryExecutionRepository,
	queryService *service.QueryExecutionService,
	config QueryExecutionSupervisorConfig,
	logger *slog.Logger,
) (*QueryExecutionSupervisor, error) {
	if queries == nil || queryService == nil {
		return nil, fmt.Errorf("Develop Query Execution Supervisor dependencies are required")
	}
	if config.InstanceID == "" || config.Concurrency <= 0 || config.PerEngineConcurrency <= 0 || config.LeaseDuration <= 0 ||
		config.HeartbeatInterval <= 0 || config.ClaimInterval <= 0 ||
		config.IdleMaxInterval < config.ClaimInterval || config.HeartbeatInterval >= config.LeaseDuration {
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

func (s *QueryExecutionSupervisor) ActiveCount() int { return int(s.active.Load()) }

// Notify interrupts local idle backoff after this Backend commits a pending
// query. PostgreSQL polling remains the cross-instance fallback.
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

func (s *QueryExecutionSupervisor) claimAvailable(ctx context.Context, availableSlots chan int, group *sync.WaitGroup) bool {
	worked := false
	for {
		select {
		case slot := <-availableSlots:
			owner := fmt.Sprintf("%s-%d", s.config.InstanceID, slot)
			execution, lease, err := s.queries.ClaimNext(
				ctx, owner, time.Now().UTC(), s.config.LeaseDuration, s.saturatedEngineIDs(),
			)
			if err != nil {
				availableSlots <- slot
				if ctx.Err() == nil {
					s.logger.Error("claim Develop query execution failed", "owner", owner, "error", err)
				}
				return worked
			}
			if execution == nil || lease == nil {
				availableSlots <- slot
				return worked
			}
			worked = true
			engineID, hasEngineID := queryExecutionEngineID(execution)
			if hasEngineID {
				s.reserveEngine(engineID)
			}
			s.active.Add(1)
			group.Add(1)
			go func(slot int, execution *commonExecution.TaskExecution, lease commonExecution.Lease, engineID uint, hasEngineID bool) {
				defer group.Done()
				defer func() {
					if hasEngineID {
						s.releaseEngine(engineID)
					}
					s.active.Add(-1)
					availableSlots <- slot
					s.Notify()
				}()
				s.processClaimed(ctx, execution, lease)
			}(slot, execution, *lease, engineID, hasEngineID)
		default:
			return worked
		}
	}
}

func (s *QueryExecutionSupervisor) saturatedEngineIDs() []uint {
	s.activeEnginesMu.Lock()
	defer s.activeEnginesMu.Unlock()
	engineIDs := make([]uint, 0, len(s.activeEngines))
	for engineID, count := range s.activeEngines {
		if count >= s.config.PerEngineConcurrency {
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

func nextIdleInterval(current, maximum time.Duration) time.Duration {
	if current >= maximum || current > maximum/2 {
		return maximum
	}
	return current * 2
}
