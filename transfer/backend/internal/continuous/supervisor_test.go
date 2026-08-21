package continuous

import (
	"context"
	"sync"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
)

func TestSupervisorCancelsSessionWhenDesiredStateStopsRenewal(t *testing.T) {
	store := &fakeLeaseStore{
		claim: &repository.RuntimeLeaseClaim{
			Task:      models.TransferTask{ID: 42, DesiredState: models.TaskDesiredStateRunning},
			Execution: commonExecution.TaskExecution{ExecutionID: "exec-42", Status: commonExecution.ExecutionStatusRunning},
			Lease:     models.RuntimeLease{TaskID: 42, OwnerInstanceID: "worker-a", FencingToken: 7},
		},
		finished: make(chan finishCall, 1),
	}
	runnerCancelled := make(chan struct{})
	runner := SessionRunnerFunc(func(ctx context.Context, _ repository.RuntimeLeaseClaim) error {
		<-ctx.Done()
		close(runnerCancelled)
		return ctx.Err()
	})
	supervisor, err := NewSupervisor(store, runner, Config{
		OwnerInstanceID: "worker-a", Capacity: 1, LeaseDuration: 100 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond, ClaimInterval: 5 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = supervisor.Run(ctx) }()
	select {
	case finished := <-store.finished:
		if finished.status != commonExecution.ExecutionStatusCancelled || finished.reason != string(models.TaskDesiredStatePaused) {
			t.Fatalf("finish = %#v", finished)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish cancelled session")
	}
	select {
	case <-runnerCancelled:
	case <-time.After(time.Second):
		t.Fatal("session runner was not cancelled")
	}
}

func TestSupervisorClassifiesRunnerFencingAfterPauseAsCancelled(t *testing.T) {
	store := &fakeLeaseStore{
		claim: &repository.RuntimeLeaseClaim{
			Task:      models.TransferTask{ID: 43, DesiredState: models.TaskDesiredStateRunning},
			Execution: commonExecution.TaskExecution{ExecutionID: "exec-43", Status: commonExecution.ExecutionStatusRunning},
			Lease:     models.RuntimeLease{TaskID: 43, OwnerInstanceID: "worker-a", FencingToken: 8},
		},
		finished: make(chan finishCall, 1),
	}
	runner := SessionRunnerFunc(func(context.Context, repository.RuntimeLeaseClaim) error {
		return repository.ErrRuntimeLeaseLost
	})
	supervisor, err := NewSupervisor(store, runner, Config{
		OwnerInstanceID: "worker-a", Capacity: 1, LeaseDuration: 100 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond, ClaimInterval: 5 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = supervisor.Run(ctx) }()
	select {
	case finished := <-store.finished:
		if finished.status != commonExecution.ExecutionStatusCancelled || finished.reason != string(models.TaskDesiredStatePaused) {
			t.Fatalf("finish = %#v, want paused cancellation", finished)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not classify fenced paused session")
	}
}

func TestSupervisorMarksSchemaChangeBlocked(t *testing.T) {
	store := &fakeLeaseStore{
		claim: &repository.RuntimeLeaseClaim{
			Task:      models.TransferTask{ID: 44, DesiredState: models.TaskDesiredStateRunning},
			Execution: commonExecution.TaskExecution{ExecutionID: "exec-44", Status: commonExecution.ExecutionStatusRunning},
			Lease:     models.RuntimeLease{TaskID: 44, OwnerInstanceID: "worker-a", FencingToken: 9},
		},
		desiredState: models.TaskDesiredStateRunning,
		finished:     make(chan finishCall, 1),
	}
	runner := SessionRunnerFunc(func(context.Context, repository.RuntimeLeaseClaim) error {
		return &SchemaChangeError{Scope: "Debezium after", UnexpectedFields: []string{"extra"}}
	})
	supervisor, err := NewSupervisor(store, runner, Config{
		OwnerInstanceID: "worker-a", Capacity: 1, LeaseDuration: 100 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond, ClaimInterval: 5 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = supervisor.Run(ctx) }()
	select {
	case finished := <-store.finished:
		if finished.status != commonExecution.ExecutionStatusFailed || finished.reason != "schema_change_blocked" {
			t.Fatalf("finish = %#v, want schema_change_blocked failure", finished)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish schema-blocked session")
	}
}

func TestSupervisorMarksActiveSessionCancelledOnWorkerShutdown(t *testing.T) {
	store := &fakeLeaseStore{
		claim: &repository.RuntimeLeaseClaim{
			Task:      models.TransferTask{ID: 45, DesiredState: models.TaskDesiredStateRunning},
			Execution: commonExecution.TaskExecution{ExecutionID: "exec-45", Status: commonExecution.ExecutionStatusRunning},
			Lease:     models.RuntimeLease{TaskID: 45, OwnerInstanceID: "worker-a", FencingToken: 10},
		},
		desiredState: models.TaskDesiredStateRunning,
		finished:     make(chan finishCall, 1),
	}
	runnerStarted := make(chan struct{})
	runner := SessionRunnerFunc(func(ctx context.Context, _ repository.RuntimeLeaseClaim) error {
		close(runnerStarted)
		<-ctx.Done()
		return ctx.Err()
	})
	supervisor, err := NewSupervisor(store, runner, Config{
		OwnerInstanceID: "worker-a", Capacity: 1, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, ClaimInterval: 5 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(ctx) }()
	select {
	case <-runnerStarted:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("continuous session did not start")
	}
	select {
	case finished := <-store.finished:
		if finished.status != commonExecution.ExecutionStatusCancelled || finished.reason != "worker_shutdown" {
			t.Fatalf("finish = %#v, want worker_shutdown cancellation", finished)
		}
	case <-time.After(time.Second):
		t.Fatal("worker shutdown did not finish active session")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop after worker shutdown")
	}
}

type finishCall struct {
	status string
	reason string
}

type fakeLeaseStore struct {
	mu           sync.Mutex
	claim        *repository.RuntimeLeaseClaim
	desiredState models.TaskDesiredState
	finished     chan finishCall
}

func (f *fakeLeaseStore) ClaimNext(context.Context, string, time.Time, time.Duration) (*repository.RuntimeLeaseClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	claim := f.claim
	f.claim = nil
	return claim, nil
}

func (f *fakeLeaseStore) Renew(context.Context, uint, string, uint64, time.Time, time.Duration) error {
	return repository.ErrRuntimeLeaseLost
}

func (f *fakeLeaseStore) Finish(_ context.Context, _ repository.RuntimeLeaseClaim, status, reason, _ string, _ time.Time) error {
	f.finished <- finishCall{status: status, reason: reason}
	return nil
}

func (f *fakeLeaseStore) DesiredState(context.Context, uint) (models.TaskDesiredState, error) {
	if f.desiredState == "" {
		return models.TaskDesiredStatePaused, nil
	}
	return f.desiredState, nil
}
