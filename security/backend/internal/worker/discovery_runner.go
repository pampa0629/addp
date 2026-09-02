package worker

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	commonexecution "github.com/addp/common/execution"
	"github.com/addp/security/internal/service"
)

type DiscoveryRunner struct {
	discoveries *service.DiscoveryService
	workerID    string
	active      atomic.Int64
}

func NewDiscoveryRunner(discoveries *service.DiscoveryService, workerID string) (*DiscoveryRunner, error) {
	if discoveries == nil || workerID == "" {
		return nil, fmt.Errorf("security discovery runner dependencies are required")
	}
	return &DiscoveryRunner{discoveries: discoveries, workerID: workerID}, nil
}

func (r *DiscoveryRunner) ActiveCount() int { return int(r.active.Load()) }

func (r *DiscoveryRunner) Run(ctx context.Context, canClaim func() bool) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		worked := false
		if canClaim == nil || canClaim() {
			if count, err := r.discoveries.RecoverExpired(ctx, time.Now().UTC(), 100); err != nil {
				if ctx.Err() == nil {
					log.Printf("Security discovery recovery failed: %v", err)
				}
			} else if count > 0 {
				log.Printf("Recovered %d expired Security discovery executions", count)
			}
			worked = r.processNext(ctx)
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

func (r *DiscoveryRunner) processNext(ctx context.Context) bool {
	item, lease, err := r.discoveries.ClaimNext(ctx, r.workerID, time.Now().UTC(), 2*time.Minute)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("Security discovery claim failed: %v", err)
		}
		return false
	}
	if item == nil || lease == nil {
		return false
	}
	r.active.Add(1)
	defer r.active.Add(-1)
	execCtx, cancel := context.WithCancel(commonexecution.ContextWithLease(ctx, *lease))
	done := make(chan error, 1)
	go r.heartbeat(execCtx, cancel, *lease, done)
	execErr := r.discoveries.Execute(execCtx, item, *lease)
	cancel()
	heartbeatErr := <-done
	if heartbeatErr != nil {
		log.Printf("Security discovery lease lost for execution %s: %v", item.ExecutionID, heartbeatErr)
	} else if execErr != nil {
		log.Printf("Security discovery execution %s failed: %v", item.ExecutionID, execErr)
	}
	return true
}

func (r *DiscoveryRunner) heartbeat(ctx context.Context, cancel context.CancelFunc, lease commonexecution.Lease, done chan<- error) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case now := <-ticker.C:
			if err := r.discoveries.Renew(ctx, lease, now.UTC().Add(2*time.Minute)); err != nil {
				terminal, stateErr := r.discoveries.AttemptIsTerminal(ctx, lease)
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
