package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
)

type BoundedRunnerConfig struct {
	WorkerID          string
	Concurrency       int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	ClaimInterval     time.Duration
}

type BoundedRunner struct {
	tasks      *repository.TaskRepository
	service    *service.TaskService
	executions *service.ExecutionService
	config     BoundedRunnerConfig
	logger     *slog.Logger
	active     atomic.Int64
}

func (r *BoundedRunner) ActiveCount() int { return int(r.active.Load()) }

func NewBoundedRunner(tasks *repository.TaskRepository, taskService *service.TaskService, executions *service.ExecutionService, config BoundedRunnerConfig, logger *slog.Logger) (*BoundedRunner, error) {
	if tasks == nil || taskService == nil || executions == nil {
		return nil, fmt.Errorf("transfer bounded runner dependencies are required")
	}
	if config.WorkerID == "" || config.Concurrency <= 0 || config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 || config.ClaimInterval <= 0 {
		return nil, fmt.Errorf("transfer bounded runner config is invalid")
	}
	if config.HeartbeatInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("transfer bounded heartbeat interval must be shorter than the lease duration")
	}
	return &BoundedRunner{tasks: tasks, service: taskService, executions: executions, config: config, logger: logger}, nil
}

func (r *BoundedRunner) Run(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		r.recoveryLoop(ctx)
	}()
	for slot := 1; slot <= r.config.Concurrency; slot++ {
		workerID := fmt.Sprintf("%s-%d", r.config.WorkerID, slot)
		workers.Add(1)
		go func() {
			defer workers.Done()
			r.workerLoop(ctx, workerID)
		}()
	}
	workers.Wait()
}

func (r *BoundedRunner) workerLoop(ctx context.Context, workerID string) {
	ticker := time.NewTicker(r.config.ClaimInterval)
	defer ticker.Stop()
	for {
		worked := r.processNext(ctx, workerID)
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

func (r *BoundedRunner) processNext(ctx context.Context, workerID string) bool {
	now := time.Now().UTC()
	execution, lease, task, err := r.tasks.ClaimNextBoundedExecution(ctx, workerID, now, r.config.LeaseDuration)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("claim bounded execution failed", "worker_id", workerID, "error", err)
		}
		return false
	}
	if execution == nil || lease == nil || task == nil {
		return false
	}
	r.active.Add(1)
	defer r.active.Add(-1)

	executionID := uint(execution.ID)
	execCtx, cancel := context.WithCancel(commonExecution.ContextWithLease(ctx, *lease))
	r.executions.BindBoundedLease(executionID, *lease)
	heartbeatDone := make(chan error, 1)
	go r.heartbeat(execCtx, cancel, *lease, heartbeatDone)
	execErr := r.service.ExecuteTask(execCtx, task.ID, executionID)
	cancel()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil {
		r.logger.Error("bounded execution lease lost", "execution_id", execution.ExecutionID, "error", heartbeatErr)
		r.executions.UnbindBoundedLease(executionID)
		return true
	}
	finishCtx := commonExecution.ContextWithLease(ctx, *lease)
	if err := r.executions.FinishIfRunning(finishCtx, executionID, execErr); err != nil {
		r.logger.Error("bounded execution finalization failed", "execution_id", execution.ExecutionID, "error", err)
	}
	r.executions.UnbindBoundedLease(executionID)
	return true
}

func (r *BoundedRunner) heartbeat(ctx context.Context, cancel context.CancelFunc, lease commonExecution.Lease, done chan<- error) {
	ticker := time.NewTicker(r.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case now := <-ticker.C:
			if err := r.tasks.RenewBoundedExecutionLease(ctx, lease, now.UTC().Add(r.config.LeaseDuration)); err != nil {
				if ctx.Err() != nil {
					done <- nil
					return
				}
				terminal, stateErr := r.tasks.BoundedExecutionAttemptIsTerminal(ctx, lease)
				if stateErr == nil && terminal {
					done <- nil
					return
				}
				if stateErr != nil {
					err = fmt.Errorf("renew lease: %w; inspect attempt state: %v", err, stateErr)
				}
				cancel()
				done <- err
				return
			}
		}
	}
}

func (r *BoundedRunner) recoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(r.config.ClaimInterval)
	defer ticker.Stop()
	for {
		if count, err := r.tasks.FailExpiredBoundedExecutions(ctx, time.Now().UTC(), 100); err != nil {
			if ctx.Err() == nil {
				r.logger.Error("bounded execution recovery failed", "error", err)
			}
		} else if count > 0 {
			r.logger.Warn("expired bounded executions require explicit retry", "count", count)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
