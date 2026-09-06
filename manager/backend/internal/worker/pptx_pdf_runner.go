package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
)

type PPTXPDFRunnerConfig struct {
	WorkerID          string
	Concurrency       int
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	ClaimInterval     time.Duration
}

type PPTXPDFExecutionService interface {
	ClaimPendingExecution(context.Context, string, time.Time, time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, *models.PPTXPDFTask, error)
	RenewExecutionLease(context.Context, commonExecution.Lease, time.Time) error
	ExecutionAttemptIsTerminal(context.Context, commonExecution.Lease) (bool, error)
	RecoverExpiredExecutions(context.Context, time.Time, int) (int, error)
	RunClaimedExecution(context.Context, *commonExecution.TaskExecution, commonExecution.Lease, *models.PPTXPDFTask) error
}

type PPTXPDFRunner struct {
	service PPTXPDFExecutionService
	config  PPTXPDFRunnerConfig
	logger  *slog.Logger
	active  atomic.Int64
}

func NewPPTXPDFRunner(service PPTXPDFExecutionService, config PPTXPDFRunnerConfig, logger *slog.Logger) (*PPTXPDFRunner, error) {
	if service == nil {
		return nil, fmt.Errorf("Manager PPTX PDF execution service is required")
	}
	if config.WorkerID == "" || config.Concurrency <= 0 || config.LeaseDuration <= 0 ||
		config.HeartbeatInterval <= 0 || config.ClaimInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("Manager PPTX PDF runner config is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PPTXPDFRunner{service: service, config: config, logger: logger}, nil
}

func (r *PPTXPDFRunner) ActiveCount() int { return int(r.active.Load()) }

func (r *PPTXPDFRunner) Run(ctx context.Context, canClaim func() bool) {
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		r.recoveryLoop(ctx, canClaim)
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

func (r *PPTXPDFRunner) workerLoop(ctx context.Context, workerID string, canClaim func() bool) {
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

func (r *PPTXPDFRunner) processNext(ctx context.Context, workerID string) bool {
	execution, lease, task, err := r.service.ClaimPendingExecution(ctx, workerID, time.Now().UTC(), r.config.LeaseDuration)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("claim Manager PPTX PDF execution failed", "worker_id", workerID, "error", err)
		}
		return false
	}
	if execution == nil || lease == nil || task == nil {
		return false
	}
	r.active.Add(1)
	defer r.active.Add(-1)

	execCtx, cancel := context.WithCancel(commonExecution.ContextWithLease(ctx, *lease))
	heartbeatDone := make(chan error, 1)
	go r.heartbeat(execCtx, cancel, *lease, heartbeatDone)
	execErr := r.service.RunClaimedExecution(execCtx, execution, *lease, task)
	cancel()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil {
		r.logger.Error("Manager PPTX PDF execution lease lost", "execution_id", execution.ExecutionID, "error", heartbeatErr)
		return true
	}
	if execErr != nil {
		r.logger.Error("Manager PPTX PDF execution failed to converge", "execution_id", execution.ExecutionID, "error", execErr)
	}
	return true
}

func (r *PPTXPDFRunner) heartbeat(ctx context.Context, cancel context.CancelFunc, lease commonExecution.Lease, done chan<- error) {
	ticker := time.NewTicker(r.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case now := <-ticker.C:
			if err := r.service.RenewExecutionLease(ctx, lease, now.UTC().Add(r.config.LeaseDuration)); err != nil {
				if ctx.Err() != nil {
					done <- nil
					return
				}
				terminal, stateErr := r.service.ExecutionAttemptIsTerminal(ctx, lease)
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

func (r *PPTXPDFRunner) recoveryLoop(ctx context.Context, canRecover func() bool) {
	ticker := time.NewTicker(r.config.ClaimInterval)
	defer ticker.Stop()
	for {
		if canRecover == nil || canRecover() {
			if count, err := r.service.RecoverExpiredExecutions(ctx, time.Now().UTC(), 100); err != nil {
				if ctx.Err() == nil {
					r.logger.Error("Manager PPTX PDF execution recovery failed", "error", err)
				}
			} else if count > 0 {
				r.logger.Warn("recovered expired Manager PPTX PDF executions", "count", count)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
