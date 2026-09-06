package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
)

type fakePPTXPDFExecutionService struct {
	claimCalls   atomic.Int64
	recoverCalls atomic.Int64
	claim        func(context.Context, string, time.Time, time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, *models.PPTXPDFTask, error)
	renew        func(context.Context, commonExecution.Lease, time.Time) error
	terminal     func(context.Context, commonExecution.Lease) (bool, error)
	recover      func(context.Context, time.Time, int) (int, error)
	run          func(context.Context, *commonExecution.TaskExecution, commonExecution.Lease, *models.PPTXPDFTask) error
}

func (f *fakePPTXPDFExecutionService) ClaimPendingExecution(ctx context.Context, workerID string, now time.Time, lease time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, *models.PPTXPDFTask, error) {
	f.claimCalls.Add(1)
	if f.claim == nil {
		return nil, nil, nil, nil
	}
	return f.claim(ctx, workerID, now, lease)
}

func (f *fakePPTXPDFExecutionService) RenewExecutionLease(ctx context.Context, lease commonExecution.Lease, expiresAt time.Time) error {
	if f.renew == nil {
		return nil
	}
	return f.renew(ctx, lease, expiresAt)
}

func (f *fakePPTXPDFExecutionService) ExecutionAttemptIsTerminal(ctx context.Context, lease commonExecution.Lease) (bool, error) {
	if f.terminal == nil {
		return false, nil
	}
	return f.terminal(ctx, lease)
}

func (f *fakePPTXPDFExecutionService) RecoverExpiredExecutions(ctx context.Context, now time.Time, limit int) (int, error) {
	f.recoverCalls.Add(1)
	if f.recover == nil {
		return 0, nil
	}
	return f.recover(ctx, now, limit)
}

func (f *fakePPTXPDFExecutionService) RunClaimedExecution(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease, task *models.PPTXPDFTask) error {
	if f.run == nil {
		return nil
	}
	return f.run(ctx, execution, lease, task)
}

func testPPTXPDFRunnerConfig() PPTXPDFRunnerConfig {
	return PPTXPDFRunnerConfig{
		WorkerID: "worker", Concurrency: 1, LeaseDuration: 100 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond, ClaimInterval: 5 * time.Millisecond,
	}
}

func testPPTXPDFRunnerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPPTXPDFRunnerClaimsAndRunsExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lease := commonExecution.Lease{ExecutionID: "execution-1", TenantID: 7, Attempt: 1, Token: "token", Owner: "worker-1"}
	var claimed atomic.Bool
	service := &fakePPTXPDFExecutionService{}
	service.claim = func(_ context.Context, _ string, _ time.Time, _ time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, *models.PPTXPDFTask, error) {
		if !claimed.CompareAndSwap(false, true) {
			return nil, nil, nil, nil
		}
		return &commonExecution.TaskExecution{ExecutionID: lease.ExecutionID, TenantID: lease.TenantID}, &lease, &models.PPTXPDFTask{ID: 3, TenantID: 7}, nil
	}
	ran := make(chan struct{})
	service.run = func(runCtx context.Context, execution *commonExecution.TaskExecution, gotLease commonExecution.Lease, task *models.PPTXPDFTask) error {
		if execution.ExecutionID != lease.ExecutionID || gotLease.Token != lease.Token || task.ID != 3 {
			t.Fatalf("runner forwarded unexpected claim: execution=%+v lease=%+v task=%+v", execution, gotLease, task)
		}
		if contextLease, ok := commonExecution.LeaseFromContext(runCtx); !ok || contextLease.Token != lease.Token {
			t.Fatalf("execution context does not carry claimed lease: %+v, %v", contextLease, ok)
		}
		close(ran)
		cancel()
		return nil
	}
	runner, err := NewPPTXPDFRunner(service, testPPTXPDFRunnerConfig(), testPPTXPDFRunnerLogger())
	if err != nil {
		t.Fatalf("NewPPTXPDFRunner() error = %v", err)
	}
	runner.Run(ctx, func() bool { return true })
	select {
	case <-ran:
	default:
		t.Fatal("claimed execution was not run")
	}
	if runner.ActiveCount() != 0 {
		t.Fatalf("ActiveCount() = %d, want 0", runner.ActiveCount())
	}
}

func TestPPTXPDFRunnerDoesNotClaimBeforeRegistration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	service := &fakePPTXPDFExecutionService{}
	runner, err := NewPPTXPDFRunner(service, testPPTXPDFRunnerConfig(), testPPTXPDFRunnerLogger())
	if err != nil {
		t.Fatalf("NewPPTXPDFRunner() error = %v", err)
	}
	runner.Run(ctx, func() bool { return false })
	if got := service.claimCalls.Load(); got != 0 {
		t.Fatalf("claim calls before registration = %d, want 0", got)
	}
	if got := service.recoverCalls.Load(); got != 0 {
		t.Fatalf("recovery calls before registration = %d, want 0", got)
	}
}

func TestPPTXPDFRunnerCancelsExecutionWhenLeaseIsLost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lease := commonExecution.Lease{ExecutionID: "execution-2", TenantID: 8, Attempt: 1, Token: "token", Owner: "worker-1"}
	var claimed atomic.Bool
	service := &fakePPTXPDFExecutionService{}
	service.claim = func(_ context.Context, _ string, _ time.Time, _ time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, *models.PPTXPDFTask, error) {
		if !claimed.CompareAndSwap(false, true) {
			return nil, nil, nil, nil
		}
		return &commonExecution.TaskExecution{ExecutionID: lease.ExecutionID, TenantID: lease.TenantID}, &lease, &models.PPTXPDFTask{ID: 4, TenantID: 8}, nil
	}
	service.renew = func(context.Context, commonExecution.Lease, time.Time) error { return errors.New("lease lost") }
	service.terminal = func(context.Context, commonExecution.Lease) (bool, error) { return false, nil }
	executionCancelled := make(chan struct{})
	service.run = func(runCtx context.Context, _ *commonExecution.TaskExecution, _ commonExecution.Lease, _ *models.PPTXPDFTask) error {
		<-runCtx.Done()
		close(executionCancelled)
		cancel()
		return runCtx.Err()
	}
	runner, err := NewPPTXPDFRunner(service, testPPTXPDFRunnerConfig(), testPPTXPDFRunnerLogger())
	if err != nil {
		t.Fatalf("NewPPTXPDFRunner() error = %v", err)
	}
	runner.Run(ctx, func() bool { return true })
	select {
	case <-executionCancelled:
	default:
		t.Fatal("execution was not cancelled after lease loss")
	}
}

func TestNewPPTXPDFRunnerRejectsHeartbeatNotShorterThanLease(t *testing.T) {
	config := testPPTXPDFRunnerConfig()
	config.HeartbeatInterval = config.LeaseDuration
	if _, err := NewPPTXPDFRunner(&fakePPTXPDFExecutionService{}, config, testPPTXPDFRunnerLogger()); err == nil {
		t.Fatal("NewPPTXPDFRunner() error = nil, want invalid config error")
	}
}
