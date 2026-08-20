package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/service"
)

type BoundedRunnerConfig struct {
	WorkerID          string
	Concurrency       int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	ClaimInterval     time.Duration
}

type BoundedRunner struct {
	executions *service.ScanExecutionService
	config     BoundedRunnerConfig
	logger     *slog.Logger
}

func NewBoundedRunner(executions *service.ScanExecutionService, config BoundedRunnerConfig, logger *slog.Logger) (*BoundedRunner, error) {
	if executions == nil || logger == nil {
		return nil, fmt.Errorf("meta bounded runner dependencies are required")
	}
	if config.WorkerID == "" || config.Concurrency <= 0 || config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 || config.ClaimInterval <= 0 {
		return nil, fmt.Errorf("meta bounded runner config is invalid")
	}
	if config.HeartbeatInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("meta bounded heartbeat interval must be shorter than the lease duration")
	}
	return &BoundedRunner{executions: executions, config: config, logger: logger}, nil
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
	execution, lease, err := r.executions.ClaimNextBoundedExecution(ctx, workerID, time.Now().UTC(), r.config.LeaseDuration)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("claim meta bounded execution failed", "worker_id", workerID, "error", err)
		}
		return false
	}
	if execution == nil || lease == nil {
		return false
	}

	execCtx, cancel := context.WithCancel(commonExecution.ContextWithLease(ctx, *lease))
	r.executions.BindBoundedLease(execution.ExecutionID, *lease)
	heartbeatDone := make(chan error, 1)
	go r.heartbeat(execCtx, cancel, *lease, heartbeatDone)
	execErr := r.executions.ExecuteScanRun(execCtx, execution.ExecutionID)
	cancel()
	heartbeatErr := <-heartbeatDone
	r.executions.UnbindBoundedLease(execution.ExecutionID)
	if heartbeatErr != nil {
		r.logger.Error("meta bounded execution lease lost", "execution_id", execution.ExecutionID, "error", heartbeatErr)
		return true
	}
	if execErr != nil {
		r.logger.Warn("meta bounded execution finished with error", "execution_id", execution.ExecutionID, "error", execErr)
	}
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
			if err := r.executions.RenewBoundedExecutionLease(ctx, lease, now.UTC().Add(r.config.LeaseDuration)); err != nil {
				if ctx.Err() != nil {
					done <- nil
					return
				}
				terminal, stateErr := r.executions.BoundedExecutionAttemptIsTerminal(ctx, lease)
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
		if count, err := r.executions.FailExpiredBoundedExecutions(ctx, time.Now().UTC(), 100); err != nil {
			if ctx.Err() == nil {
				r.logger.Error("meta bounded execution recovery failed", "error", err)
			}
		} else if count > 0 {
			r.logger.Warn("expired meta executions require explicit retry", "count", count)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
