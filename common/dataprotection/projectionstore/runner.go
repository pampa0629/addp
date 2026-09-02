package projectionstore

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/addp/common/dataprotection"
)

const changeBatchSize = 200

type ChangeSource interface {
	ListProtectionProjectionChangesForTenant(context.Context, uint, string, int) (*dataprotection.ProjectionChangesResponse, error)
	AcknowledgeProtectionProjectionCursorForTenant(context.Context, uint, string) error
}

type TenantRegistry interface {
	ListRuntimeTenantIDs(context.Context) ([]uint, error)
}

// AcknowledgementBarrier runs after projection rows and the owner checkpoint
// are durably committed, but before the owner acknowledges the cursor to
// Security. Owners with long-lived reads use it to quiesce or isolate work
// that started under an older projection before claiming the change is fully
// installed.
type AcknowledgementBarrier interface {
	ReadyToAcknowledge(context.Context, int64, string) error
}

type Runner struct {
	store    *Store
	source   ChangeSource
	registry TenantRegistry
	barrier  AcknowledgementBarrier
	interval time.Duration
	wake     chan struct{}
	mu       sync.Mutex
	tenants  map[int64]struct{}
}

func NewRunner(store *Store, source ChangeSource, registry TenantRegistry, interval time.Duration, barrier AcknowledgementBarrier) *Runner {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Runner{store: store, source: source, registry: registry, barrier: barrier, interval: interval, wake: make(chan struct{}, 1), tenants: make(map[int64]struct{})}
}

func (r *Runner) Start(ctx context.Context) {
	if r == nil || r.store == nil || r.source == nil {
		return
	}
	go r.run(ctx)
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runner) ObserveTenant(tenantID int64) {
	if r == nil || tenantID <= 0 {
		return
	}
	r.mu.Lock()
	_, exists := r.tenants[tenantID]
	r.tenants[tenantID] = struct{}{}
	r.mu.Unlock()
	if !exists {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

func (r *Runner) run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sync(ctx)
		case <-r.wake:
			r.sync(ctx)
		}
	}
}

func (r *Runner) sync(ctx context.Context) {
	if r.registry != nil {
		if tenantIDs, err := r.registry.ListRuntimeTenantIDs(ctx); err != nil {
			if ctx.Err() == nil {
				log.Printf("Protection projection tenant discovery delayed: %v", err)
			}
		} else {
			for _, tenantID := range tenantIDs {
				r.ObserveTenant(int64(tenantID))
			}
		}
	}
	for _, tenantID := range r.tenantIDs() {
		if err := r.syncTenant(ctx, tenantID); err != nil && ctx.Err() == nil {
			log.Printf("Protection projection sync delayed for tenant %d: %v", tenantID, err)
		}
	}
}

func (r *Runner) syncTenant(ctx context.Context, tenantID int64) error {
	for {
		cursor, err := r.store.CurrentCursor(ctx, tenantID)
		if err != nil {
			return err
		}
		batch, err := r.source.ListProtectionProjectionChangesForTenant(ctx, uint(tenantID), cursor, changeBatchSize)
		if err != nil {
			return err
		}
		if err := r.store.ApplyBatch(ctx, tenantID, cursor, batch, time.Now().UTC()); err != nil {
			return err
		}
		cursorToAcknowledge := batch.NextCursor
		if cursorToAcknowledge == "" {
			cursorToAcknowledge = cursor
		}
		if cursorToAcknowledge != "" {
			if r.barrier != nil {
				if err := r.barrier.ReadyToAcknowledge(ctx, tenantID, cursorToAcknowledge); err != nil {
					return err
				}
			}
			if err := r.source.AcknowledgeProtectionProjectionCursorForTenant(ctx, uint(tenantID), cursorToAcknowledge); err != nil {
				return err
			}
		}
		if !batch.HasMore {
			return nil
		}
	}
}

func (r *Runner) tenantIDs() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]int64, 0, len(r.tenants))
	for tenantID := range r.tenants {
		result = append(result, tenantID)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
