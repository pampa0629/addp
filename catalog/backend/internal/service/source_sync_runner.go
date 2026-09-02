package service

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/addp/catalog/internal/models"
	"gorm.io/gorm"
)

type SourceSyncRunner struct {
	db       *gorm.DB
	syncers  []TenantSourceSynchronizer
	interval time.Duration
	registry TenantRegistry
	wake     chan struct{}

	mu      sync.Mutex
	tenants map[int64]struct{}
}

type TenantSourceSynchronizer interface {
	SourceName() string
	SyncTenant(context.Context, int64) error
}

type TenantRegistry interface {
	ListRuntimeTenantIDs(ctx context.Context) ([]uint, error)
}

func NewSourceSyncRunner(db *gorm.DB, interval time.Duration, registry TenantRegistry, syncers ...TenantSourceSynchronizer) *SourceSyncRunner {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	runner := &SourceSyncRunner{
		db: db, syncers: syncers, interval: interval, registry: registry,
		wake: make(chan struct{}, 1), tenants: make(map[int64]struct{}),
	}
	return runner
}

func (r *SourceSyncRunner) Start(ctx context.Context) {
	if r == nil || len(r.syncers) == 0 {
		return
	}
	var checkpoints []models.SourceCheckpoint
	if err := r.db.WithContext(ctx).Distinct("tenant_id").Find(&checkpoints).Error; err == nil {
		for _, checkpoint := range checkpoints {
			r.ObserveTenant(checkpoint.TenantID)
		}
	}
	go r.run(ctx)
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// ObserveTenant registers a tenant as an asynchronous sync target. It never
// blocks the user request and Meta availability is not part of Catalog Ready.
func (r *SourceSyncRunner) ObserveTenant(tenantID int64) {
	if r == nil || tenantID <= 0 {
		return
	}
	r.mu.Lock()
	_, exists := r.tenants[tenantID]
	r.tenants[tenantID] = struct{}{}
	r.mu.Unlock()
	if exists {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *SourceSyncRunner) TenantIDs() []int64 {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tenantIDs := make([]int64, 0, len(r.tenants))
	for tenantID := range r.tenants {
		tenantIDs = append(tenantIDs, tenantID)
	}
	sort.Slice(tenantIDs, func(i, j int) bool { return tenantIDs[i] < tenantIDs[j] })
	return tenantIDs
}

func (r *SourceSyncRunner) run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.syncObservedTenants(ctx)
		case <-r.wake:
			r.syncObservedTenants(ctx)
		}
	}
}

func (r *SourceSyncRunner) syncObservedTenants(ctx context.Context) {
	if r.registry != nil {
		if tenantIDs, err := r.registry.ListRuntimeTenantIDs(ctx); err != nil {
			if ctx.Err() == nil {
				log.Printf("Catalog tenant discovery delayed: %v", err)
			}
		} else {
			for _, tenantID := range tenantIDs {
				r.ObserveTenant(int64(tenantID))
			}
		}
	}
	for _, tenantID := range r.TenantIDs() {
		for _, synchronizer := range r.syncers {
			if err := synchronizer.SyncTenant(ctx, tenantID); err != nil && ctx.Err() == nil {
				log.Printf("Catalog %s sync delayed for tenant %d: %v", synchronizer.SourceName(), tenantID, err)
			}
		}
	}
}
