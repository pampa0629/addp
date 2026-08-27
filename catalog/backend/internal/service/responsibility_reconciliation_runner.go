package service

import (
	"context"
	"log"
	"time"
)

type TenantIDProvider interface {
	TenantIDs() []int64
}

type ResponsibilityReconciliationRunner struct {
	service  *GovernanceTaskService
	tenants  TenantIDProvider
	interval time.Duration
}

func NewResponsibilityReconciliationRunner(service *GovernanceTaskService, tenants TenantIDProvider, interval time.Duration) *ResponsibilityReconciliationRunner {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &ResponsibilityReconciliationRunner{service: service, tenants: tenants, interval: interval}
}

func (r *ResponsibilityReconciliationRunner) Start(ctx context.Context) {
	if r == nil || r.service == nil || r.tenants == nil {
		return
	}
	go r.run(ctx)
}

func (r *ResponsibilityReconciliationRunner) run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, tenantID := range r.tenants.TenantIDs() {
				if err := r.service.ReconcileTenant(ctx, tenantID); err != nil && ctx.Err() == nil {
					log.Printf("Catalog responsibility reconciliation delayed for tenant %d: %v", tenantID, err)
				}
			}
		}
	}
}
