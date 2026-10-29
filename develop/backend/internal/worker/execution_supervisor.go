package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
)

type ExecutionSupervisorConfig struct {
	InstanceID         string
	ResolveConcurrency func(context.Context) (models.ExecutionConcurrency, error)
	LeaseDuration      time.Duration
	HeartbeatInterval  time.Duration
	ClaimInterval      time.Duration
}

type ExecutionSupervisor struct {
	queue   *repository.ExecutionRepository
	service interface {
		Execute(context.Context, *commonExecution.TaskExecution, commonExecution.Lease) error
	}
	config          ExecutionSupervisorConfig
	logger          *slog.Logger
	active          map[string]*atomic.Int64
	capacity        map[string]*atomic.Int64
	nextOwner       uint64
	wake            chan struct{}
	activeEnginesMu sync.Mutex
	activeEngines   map[uint]int
}

func NewExecutionSupervisor(
	queue *repository.ExecutionRepository,
	executor interface {
		Execute(context.Context, *commonExecution.TaskExecution, commonExecution.Lease) error
	},
	config ExecutionSupervisorConfig,
	logger *slog.Logger,
) (*ExecutionSupervisor, error) {
	if queue == nil || executor == nil {
		return nil, fmt.Errorf("Develop Execution Supervisor dependencies are required")
	}
	if config.InstanceID == "" || config.ResolveConcurrency == nil || config.LeaseDuration <= 0 ||
		config.HeartbeatInterval <= 0 || config.ClaimInterval <= 0 ||
		config.HeartbeatInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("Develop Execution Supervisor config is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ExecutionSupervisor{
		queue: queue, service: executor, config: config, logger: logger,
		wake: make(chan struct{}, 1), activeEngines: make(map[uint]int),
		active: executionCounters(), capacity: executionCounters(),
	}, nil
}

func executionCounters() map[string]*atomic.Int64 {
	counters := make(map[string]*atomic.Int64)
	for _, kind := range []string{"query", "workflow", "script"} {
		counters[kind] = &atomic.Int64{}
	}
	return counters
}
func (s *ExecutionSupervisor) Capacity(taskType string) int    { return int(s.capacity[taskType].Load()) }
func (s *ExecutionSupervisor) ActiveCount(taskType string) int { return int(s.active[taskType].Load()) }

// Notify wakes scheduling after a local execution or policy change. PostgreSQL
// polling discovers changes committed by other Backend instances.
func (s *ExecutionSupervisor) Notify() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *ExecutionSupervisor) Run(ctx context.Context, canClaim func() bool) {
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

// Limits are read as one version before each scheduling pass. Running queue
// keep their slots when limits shrink; only subsequent claims are throttled.
func (s *ExecutionSupervisor) claimAvailable(ctx context.Context, group *sync.WaitGroup) {
	limits, err := s.config.ResolveConcurrency(ctx)
	if err != nil || !limits.Valid() {
		if ctx.Err() == nil {
			s.logger.Error("read Develop execution concurrency failed", "error", err)
		}
		return
	}
	// Each type has an independent budget. Query backlog cannot starve Workflow or Notebook.
	for _, taskType := range []string{"query", "workflow", "script"} {
		concurrency := limits.For(taskType)
		s.capacity[taskType].Store(int64(concurrency))
		for claimed := 0; claimed < concurrency && ctx.Err() == nil && s.ActiveCount(taskType) < concurrency; claimed++ {
			s.nextOwner++
			owner := fmt.Sprintf("%s-%d", s.config.InstanceID, s.nextOwner)
			var excluded []uint
			if taskType == commonExecution.TaskTypeQuery {
				excluded = s.saturatedEngineIDs(limits.QueryPerEngine)
			}
			execution, lease, err := s.queue.ClaimNext(taskType, ctx, owner, time.Now().UTC(), s.config.LeaseDuration, excluded)
			if err != nil {
				if ctx.Err() == nil {
					s.logger.Error("claim Develop execution failed", "task_type", taskType, "error", err)
				}
				break
			}
			if execution == nil || lease == nil {
				break
			}
			engineID, hasEngineID := queryExecutionEngineID(execution)
			hasEngineID = hasEngineID && taskType == commonExecution.TaskTypeQuery
			if hasEngineID {
				s.reserveEngine(engineID)
			}
			s.active[taskType].Add(1)
			group.Add(1)
			go func() {
				defer group.Done()
				defer func() {
					if hasEngineID {
						s.releaseEngine(engineID)
					}
					s.active[taskType].Add(-1)
					s.Notify()
				}()
				s.processClaimed(ctx, execution, *lease)
			}()
		}
	}
}

func (s *ExecutionSupervisor) saturatedEngineIDs(limit int) []uint {
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

func (s *ExecutionSupervisor) reserveEngine(engineID uint) {
	s.activeEnginesMu.Lock()
	defer s.activeEnginesMu.Unlock()
	s.activeEngines[engineID]++
}

func (s *ExecutionSupervisor) releaseEngine(engineID uint) {
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

func (s *ExecutionSupervisor) processClaimed(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease) {
	execCtx, cancel := context.WithCancel(commonExecution.ContextWithLease(ctx, lease))
	heartbeatDone := make(chan error, 1)
	go s.heartbeat(execCtx, cancel, lease, heartbeatDone)
	runErr := s.service.Execute(execCtx, execution, lease)

	terminal, terminalErr := s.queue.AttemptIsTerminal(context.Background(), lease)
	if terminalErr != nil {
		s.logger.Error("read Develop execution terminal state failed", "execution_id", execution.ExecutionID, "error", terminalErr)
	} else if !terminal {
		code := "develop." + execution.TaskType + ".execution_incomplete"
		message := "Develop execution execution returned without reaching a terminal state"
		if runErr != nil {
			code = "develop." + execution.TaskType + ".execution_failed"
			message = runErr.Error()
		}
		if err := s.queue.FailClaimed(context.Background(), execution, lease, code, message, time.Now().UTC()); err != nil {
			s.logger.Error("converge Develop execution failure failed", "execution_id", execution.ExecutionID, "error", err)
		}
	}
	cancel()
	if heartbeatErr := <-heartbeatDone; heartbeatErr != nil {
		s.logger.Error("Develop execution lease lost", "execution_id", execution.ExecutionID, "error", heartbeatErr)
	}
}

func (s *ExecutionSupervisor) heartbeat(ctx context.Context, cancel context.CancelFunc, lease commonExecution.Lease, done chan<- error) {
	ticker := time.NewTicker(s.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			if err := s.queue.Renew(ctx, lease, s.config.LeaseDuration); err != nil {
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

func (s *ExecutionSupervisor) recoveryLoop(ctx context.Context, canRecover func() bool) {
	interval := s.config.ClaimInterval
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if canRecover == nil || canRecover() {
			now := time.Now().UTC()
			if count, err := s.queue.RecoverUnleased(ctx, now, 100); err != nil {
				if ctx.Err() == nil {
					s.logger.Error("recover unleased Develop executions failed", "error", err)
				}
			} else if count > 0 {
				s.logger.Warn("unleased Develop executions converged", "count", count)
			}
			if count, err := s.queue.RecoverExpired(ctx, now, 100); err != nil {
				if ctx.Err() == nil {
					s.logger.Error("recover expired Develop executions failed", "error", err)
				}
			} else if count > 0 {
				s.logger.Warn("expired Develop executions converged", "count", count)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
