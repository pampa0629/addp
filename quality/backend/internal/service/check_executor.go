package service

import (
	"context"
	"errors"
	"fmt"
	commonClient "github.com/addp/common/client"
	"github.com/addp/quality/internal/repository"
	"github.com/google/uuid"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	qualityExecutionFailedCode = "quality.execution.failed"
	qualityExecutionTimeout    = "quality.execution.timeout"
	qualityWorkerLease         = 30 * time.Minute
	qualityWorkerPoll          = 500 * time.Millisecond
)

type executionFailure struct {
	code string
	err  error
}

func (e *executionFailure) Error() string { return e.err.Error() }
func (e *executionFailure) Unwrap() error { return e.err }
func failExecution(code string, err error) error {
	if err == nil {
		return nil
	}
	return &executionFailure{code: code, err: err}
}
func executionFailureCode(err error) string {
	var failure *executionFailure
	if errors.As(err, &failure) && failure.code != "" {
		return failure.code
	}
	return qualityExecutionFailedCode
}

type CheckExecutor struct {
	systemClient      *commonClient.SystemServiceClient
	planRepo          *repository.PlanRepository
	workerConcurrency int
	workerLease       time.Duration
	workerPoll        time.Duration
	workerID          string
	workerCancel      context.CancelFunc
	workerDone        chan struct{}
	workerStartOnce   sync.Once
	workerActive      atomic.Int64
}

func NewCheckExecutor(system *commonClient.SystemServiceClient, repo *repository.PlanRepository, concurrency int) *CheckExecutor {
	return &CheckExecutor{systemClient: system, planRepo: repo, workerConcurrency: concurrency, workerLease: qualityWorkerLease, workerPoll: qualityWorkerPoll, workerID: "quality-" + uuid.NewString(), workerDone: make(chan struct{})}
}
func (e *CheckExecutor) WorkerID() string { return e.workerID }
func (e *CheckExecutor) ActiveCount() int { return int(e.workerActive.Load()) }
func (e *CheckExecutor) ConfigureWorker(lease, poll time.Duration) error {
	if lease <= 0 || poll <= 0 || poll >= lease {
		return fmt.Errorf("invalid worker configuration")
	}
	e.workerLease = lease
	e.workerPoll = poll
	return nil
}

// StartWorker starts the single durable execution route inside the independent
// quality-worker process. Multiple workers coordinate through PostgreSQL leases.
func (e *CheckExecutor) StartWorker(ctx context.Context, canClaim func() bool) {
	e.workerStartOnce.Do(func() {
		workerCtx, cancel := context.WithCancel(ctx)
		e.workerCancel = cancel
		go e.workerSupervisor(workerCtx, canClaim)
	})
}

func (e *CheckExecutor) StopWorker() {
	if e.workerCancel != nil {
		e.workerCancel()
		<-e.workerDone
	}
}

func (e *CheckExecutor) workerSupervisor(ctx context.Context, canClaim func() bool) {
	defer close(e.workerDone)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		e.recoveryLoop(ctx)
	}()
	for slot := 1; slot <= e.workerConcurrency; slot++ {
		workerID := fmt.Sprintf("%s-%d", e.workerID, slot)
		workers.Add(1)
		go func(workerID string) {
			defer workers.Done()
			e.executionWorkerLoop(ctx, workerID, canClaim)
		}(workerID)
	}
	workers.Wait()
}

func (e *CheckExecutor) recoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(e.workerPoll)
	defer ticker.Stop()
	e.processExpired(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.processExpired(ctx)
		}
	}
}

func (e *CheckExecutor) executionWorkerLoop(ctx context.Context, workerID string, canClaim func() bool) {
	ticker := time.NewTicker(e.workerPoll)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if canClaim == nil || !canClaim() {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			}
		}
		if e.processPending(ctx, workerID) {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *CheckExecutor) processExpired(ctx context.Context) {
	if err := e.planRepo.RecoverExpiredExecutions(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
		log.Printf("quality lease recovery: %v", err)
	}
}
func (e *CheckExecutor) processPending(ctx context.Context, workerID string) bool {
	return e.processPendingPlan(ctx, workerID)
}
func executionErrorForDeadline(err error, timedOut bool) error {
	if timedOut && err == nil {
		return failExecution(qualityExecutionTimeout, context.DeadlineExceeded)
	}
	return err
}
