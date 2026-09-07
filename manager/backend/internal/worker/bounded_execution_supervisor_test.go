package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
)

type supervisorTestQueue struct {
	mu               sync.Mutex
	execution        *commonExecution.TaskExecution
	lease            *commonExecution.Lease
	terminal         bool
	failed           chan struct{}
	claimCalls       int
	claimCalled      chan struct{}
	claimedTaskTypes []string
}

func (q *supervisorTestQueue) ClaimNext(_ context.Context, taskTypes []string, _ string, _ time.Time, _ time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claimCalls++
	q.claimedTaskTypes = append([]string(nil), taskTypes...)
	select {
	case q.claimCalled <- struct{}{}:
	default:
	}
	if q.execution == nil {
		return nil, nil, nil
	}
	execution, lease := q.execution, q.lease
	q.execution, q.lease = nil, nil
	return execution, lease, nil
}

func TestBoundedExecutionSupervisorUsesSingleIdleClaimCoordinator(t *testing.T) {
	queue := &supervisorTestQueue{failed: make(chan struct{}), claimCalled: make(chan struct{}, 8)}
	dispatcher := &supervisorTestDispatcher{called: make(chan struct{}), queue: queue}
	supervisor, err := NewBoundedExecutionSupervisor(queue, dispatcher, BoundedExecutionSupervisorConfig{
		InstanceID: "manager-test", Concurrency: 4, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, ClaimInterval: 200 * time.Millisecond,
		IdleMaxInterval: 800 * time.Millisecond,
		TaskTypes:       []string{commonExecution.TaskTypePPTXPDFGeneration, commonExecution.TaskTypePointCloudCOPCGeneration},
	}, nil)
	if err != nil {
		t.Fatalf("new supervisor: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { supervisor.Run(ctx, nil); close(done) }()
	select {
	case <-queue.claimCalled:
	case <-time.After(time.Second):
		t.Fatal("coordinator did not perform initial claim")
	}
	time.Sleep(50 * time.Millisecond)
	queue.mu.Lock()
	claimCalls := queue.claimCalls
	claimedTaskTypes := append([]string(nil), queue.claimedTaskTypes...)
	queue.mu.Unlock()
	if claimCalls != 1 {
		t.Fatalf("idle claim calls = %d, want one coordinator call before base interval", claimCalls)
	}
	if len(claimedTaskTypes) != 2 {
		t.Fatalf("claimed task types = %#v, want unified task type set", claimedTaskTypes)
	}
	cancel()
	<-done
}

func TestBoundedExecutionSupervisorWakeInterruptsIdleBackoff(t *testing.T) {
	queue := &supervisorTestQueue{failed: make(chan struct{}), claimCalled: make(chan struct{}, 8)}
	dispatcher := &supervisorTestDispatcher{complete: true, called: make(chan struct{}), queue: queue}
	supervisor, err := NewBoundedExecutionSupervisor(queue, dispatcher, BoundedExecutionSupervisorConfig{
		InstanceID: "manager-test", Concurrency: 1, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, ClaimInterval: 500 * time.Millisecond,
		IdleMaxInterval: 2 * time.Second,
		TaskTypes:       []string{commonExecution.TaskTypePPTXPDFGeneration},
	}, nil)
	if err != nil {
		t.Fatalf("new supervisor: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { supervisor.Run(ctx, nil); close(done) }()
	select {
	case <-queue.claimCalled:
	case <-time.After(time.Second):
		t.Fatal("coordinator did not enter idle backoff")
	}
	queue.mu.Lock()
	queue.execution = &commonExecution.TaskExecution{ExecutionID: "execution-woken", TenantID: 7, TaskType: commonExecution.TaskTypePPTXPDFGeneration}
	queue.lease = &commonExecution.Lease{ExecutionID: "execution-woken", TenantID: 7, Attempt: 1, Token: "token", Owner: "owner"}
	queue.mu.Unlock()
	started := time.Now()
	supervisor.Notify()
	select {
	case <-dispatcher.called:
		if elapsed := time.Since(started); elapsed >= 400*time.Millisecond {
			t.Fatalf("wake dispatch latency = %s, want below base polling interval", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("wake did not trigger execution claim")
	}
	cancel()
	<-done
}
func (q *supervisorTestQueue) RenewLease(context.Context, commonExecution.Lease, time.Time) error {
	return nil
}
func (q *supervisorTestQueue) AttemptIsTerminal(context.Context, commonExecution.Lease) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.terminal, nil
}
func (q *supervisorTestQueue) FailClaimed(context.Context, *commonExecution.TaskExecution, commonExecution.Lease, string, string, time.Time) error {
	q.mu.Lock()
	q.terminal = true
	q.mu.Unlock()
	close(q.failed)
	return nil
}
func (q *supervisorTestQueue) RecoverUnleased(context.Context, time.Time, int) (int, error) {
	return 0, nil
}
func (q *supervisorTestQueue) RecoverExpired(context.Context, time.Time, int) (int, error) {
	return 0, nil
}

type supervisorTestDispatcher struct {
	err      error
	complete bool
	called   chan struct{}
	queue    *supervisorTestQueue
}

func (d supervisorTestDispatcher) RunClaimedExecution(context.Context, *commonExecution.TaskExecution, commonExecution.Lease) error {
	if d.complete {
		d.queue.mu.Lock()
		d.queue.terminal = true
		d.queue.mu.Unlock()
	}
	close(d.called)
	return d.err
}

func TestBoundedExecutionSupervisorDispatchesClaimedExecution(t *testing.T) {
	queue, dispatcher, supervisor := newSupervisorTest(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { supervisor.Run(ctx, nil); close(done) }()
	select {
	case <-dispatcher.called:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not dispatch claimed execution")
	}
	cancel()
	<-done
	terminal, _ := queue.AttemptIsTerminal(context.Background(), commonExecution.Lease{})
	if !terminal {
		t.Fatal("dispatched execution did not reach terminal state")
	}
}

func TestBoundedExecutionSupervisorConvergesDispatchFailure(t *testing.T) {
	queue, _, supervisor := newSupervisorTest(t, errors.New("dispatch failed"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { supervisor.Run(ctx, nil); close(done) }()
	select {
	case <-queue.failed:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not converge dispatch failure")
	}
	cancel()
	<-done
}

func TestBoundedExecutionSupervisorConvergesIncompleteDispatch(t *testing.T) {
	queue, dispatcher, supervisor := newSupervisorTest(t, nil)
	dispatcher.complete = false
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { supervisor.Run(ctx, nil); close(done) }()
	select {
	case <-queue.failed:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not converge incomplete dispatch")
	}
	cancel()
	<-done
}

func newSupervisorTest(t *testing.T, dispatchErr error) (*supervisorTestQueue, *supervisorTestDispatcher, *BoundedExecutionSupervisor) {
	t.Helper()
	execution := &commonExecution.TaskExecution{ExecutionID: "execution-1", TenantID: 7, TaskType: commonExecution.TaskTypePPTXPDFGeneration}
	lease := &commonExecution.Lease{ExecutionID: execution.ExecutionID, TenantID: execution.TenantID, Attempt: 1, Token: "token", Owner: "owner"}
	queue := &supervisorTestQueue{execution: execution, lease: lease, failed: make(chan struct{}), claimCalled: make(chan struct{}, 8)}
	dispatcher := &supervisorTestDispatcher{err: dispatchErr, complete: dispatchErr == nil, called: make(chan struct{}), queue: queue}
	supervisor, err := NewBoundedExecutionSupervisor(queue, dispatcher, BoundedExecutionSupervisorConfig{
		InstanceID: "manager-test", Concurrency: 1, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, ClaimInterval: time.Millisecond,
		IdleMaxInterval: 5 * time.Millisecond,
		TaskTypes:       []string{commonExecution.TaskTypePPTXPDFGeneration},
	}, nil)
	if err != nil {
		t.Fatalf("new supervisor: %v", err)
	}
	return queue, dispatcher, supervisor
}
