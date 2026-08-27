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

type QueryRunnerConfig struct {
	WorkerID          string
	Concurrency       int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	ClaimInterval     time.Duration
}

type QueryRunner struct {
	queries *repository.QueryExecutionRepository
	service *service.QueryWorkerService
	config  QueryRunnerConfig
	logger  *slog.Logger
	active  atomic.Int64
}

func NewQueryRunner(
	queries *repository.QueryExecutionRepository,
	queryService *service.QueryWorkerService,
	config QueryRunnerConfig,
	logger *slog.Logger,
) (*QueryRunner, error) {
	if queries == nil || queryService == nil {
		return nil, fmt.Errorf("Develop Query Runner dependencies are required")
	}
	if config.WorkerID == "" || config.Concurrency <= 0 || config.LeaseDuration <= 0 ||
		config.HeartbeatInterval <= 0 || config.ClaimInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("Develop Query Runner config is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &QueryRunner{queries: queries, service: queryService, config: config, logger: logger}, nil
}

func (r *QueryRunner) ActiveCount() int { return int(r.active.Load()) }

func (r *QueryRunner) Run(ctx context.Context, canClaim func() bool) {
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
			r.workerLoop(ctx, workerID, canClaim)
		}()
	}
	workers.Wait()
}

func (r *QueryRunner) workerLoop(ctx context.Context, workerID string, canClaim func() bool) {
	ticker := time.NewTicker(r.config.ClaimInterval)
	defer ticker.Stop()
	for {
		worked := false
		if canClaim == nil || canClaim() {
			worked = r.processNext(ctx, workerID)
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

func (r *QueryRunner) processNext(ctx context.Context, workerID string) bool {
	execution, lease, err := r.queries.ClaimNext(ctx, workerID, time.Now().UTC(), r.config.LeaseDuration)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("claim Develop query execution failed", "worker_id", workerID, "error", err)
		}
		return false
	}
	if execution == nil || lease == nil {
		return false
	}
	r.active.Add(1)
	defer r.active.Add(-1)

	execCtx, cancel := context.WithCancel(commonExecution.ContextWithLease(ctx, *lease))
	heartbeatDone := make(chan error, 1)
	go r.heartbeat(execCtx, cancel, *lease, heartbeatDone)
	execErr := r.service.Execute(execCtx, execution, *lease)
	cancel()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil {
		r.logger.Error("Develop query execution lease lost", "execution_id", execution.ExecutionID, "error", heartbeatErr)
		return true
	}
	if execErr != nil {
		r.logger.Error("Develop query execution failed to converge", "execution_id", execution.ExecutionID, "error", execErr)
	}
	return true
}

func (r *QueryRunner) heartbeat(ctx context.Context, cancel context.CancelFunc, lease commonExecution.Lease, done chan<- error) {
	ticker := time.NewTicker(r.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case now := <-ticker.C:
			if err := r.queries.Renew(ctx, lease, now.UTC().Add(r.config.LeaseDuration)); err != nil {
				if ctx.Err() != nil {
					done <- nil
					return
				}
				terminal, stateErr := r.queries.AttemptIsTerminal(ctx, lease)
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

func (r *QueryRunner) recoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(r.config.ClaimInterval)
	defer ticker.Stop()
	for {
		if count, err := r.queries.RecoverExpired(ctx, time.Now().UTC(), 100); err != nil {
			if ctx.Err() == nil {
				r.logger.Error("Develop query execution recovery failed", "error", err)
			}
		} else if count > 0 {
			r.logger.Warn("recovered expired Develop query executions", "count", count)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
