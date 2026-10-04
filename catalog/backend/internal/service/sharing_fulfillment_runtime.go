package service

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/addp/catalog/internal/models"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type systemSharingFulfillmentAuthority struct {
	client *client.SystemFulfillmentClient
}

func (a *systemSharingFulfillmentAuthority) Read(ctx context.Context, tenantID int64, id uuid.UUID, binding sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
	result, err := a.client.WithTenantID(uint(tenantID)).Resolve(ctx, id, binding)
	if err != nil {
		return nil, err
	}
	if !result.Found {
		return nil, errSharingFulfillmentNotFound
	}
	return runtimeFulfillmentResolution(result.Resolution)
}

func (a *systemSharingFulfillmentAuthority) Close(ctx context.Context, tenantID int64, id uuid.UUID, binding sharingFulfillmentBinding) (*sharingFulfillmentResolution, error) {
	result, err := a.client.WithTenantID(uint(tenantID)).Close(ctx, id, binding)
	if err != nil {
		return nil, err
	}
	return runtimeFulfillmentResolution(result)
}

func (a *systemSharingFulfillmentAuthority) ReadGrant(ctx context.Context, tenantID int64, id uuid.UUID, binding sharingFulfillmentBinding) (*shared.SharingFulfillmentGrantLookup, error) {
	return a.client.WithTenantID(uint(tenantID)).ResolveGrant(ctx, id, binding)
}

func (a *systemSharingFulfillmentAuthority) IssueGrant(ctx context.Context, tenantID int64, id uuid.UUID, binding sharingFulfillmentBinding) (*shared.SharingFulfillmentGrant, error) {
	return a.client.WithTenantID(uint(tenantID)).IssueGrant(ctx, id, binding)
}

func runtimeFulfillmentResolution(result *shared.SharingFulfillmentResolution) (*sharingFulfillmentResolution, error) {
	if result == nil || result.RecordedAt.IsZero() || result.Binding.Validate() != nil ||
		(result.Outcome != "accepted" && result.Outcome != "closed") ||
		(result.Outcome == "accepted" && (result.Deadline == nil || !result.Deadline.After(result.RecordedAt))) ||
		(result.Outcome == "closed" && result.Deadline != nil) {
		return nil, ErrSharingDecisionConflict
	}
	return &sharingFulfillmentResolution{RequestID: result.RequestID, TenantID: result.TenantID, Binding: result.Binding, Outcome: result.Outcome}, nil
}

// Recovery consumes committed requests only: reconcile acceptance, then
// continue original issuance. It never prepares or accepts a new request,
// refreshes its window, or drops protection merely because a request is old.
type SharingFulfillmentReconciliationRunner struct {
	db        *gorm.DB
	tenants   TenantIDProvider
	authority sharingFulfillmentIssuanceAuthority
	interval  time.Duration
}

func NewSharingFulfillmentReconciliationRunner(db *gorm.DB, tenants TenantIDProvider, remote *client.SystemFulfillmentClient, interval time.Duration) *SharingFulfillmentReconciliationRunner {
	runner := &SharingFulfillmentReconciliationRunner{db: db, tenants: tenants, interval: interval}
	if remote != nil {
		runner.authority = &systemSharingFulfillmentAuthority{client: remote}
	}
	return runner
}

func (r *SharingFulfillmentReconciliationRunner) Start(ctx context.Context) {
	if r == nil || r.db == nil || r.tenants == nil || r.authority == nil || r.interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, tenant := range r.tenants.TenantIDs() {
					if err := r.reconcileTenant(ctx, tenant); err != nil && ctx.Err() == nil {
						// IDs/status are sufficient; do not log bindings or credentials.
						log.Printf("Catalog fulfillment reconciliation delayed for tenant %d", tenant)
					}
				}
			}
		}
	}()
}

func (r *SharingFulfillmentReconciliationRunner) reconcileTenant(ctx context.Context, tenantID int64) error {
	if r == nil || r.db == nil || r.authority == nil || tenantID <= 0 {
		return ErrInvalidEntryUpdate
	}
	// A one-minute grace lets the foreground perform its first post-commit
	// send; it is not an authorization window or evidence that closure won.
	cutoff := "CURRENT_TIMESTAMP - INTERVAL '1 minute'"
	if r.db.Dialector.Name() == "sqlite" {
		cutoff = "datetime('now', '-1 minute')"
	}
	var failures []error
	var cursor *models.FulfillmentCheck
	for {
		var checks []models.FulfillmentCheck
		query := r.db.WithContext(ctx).Where("tenant_id = ? AND grant_reconciled_at IS NULL AND created_at <= "+cutoff, tenantID)
		if cursor != nil {
			query = query.Where("(created_at, request_id) > (?, ?)", cursor.CreatedAt, cursor.RequestID)
		}
		if err := query.Order("created_at, request_id").Limit(100).Find(&checks).Error; err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, row := range checks {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			callContext, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, err := continueSharingFulfillment(callContext, r.db, row.TenantID, row.CatalogEntryID, row.RequestID, r.authority)
			cancel()
			if err != nil {
				failures = append(failures, err)
			}
		}
		if len(checks) < 100 {
			return errors.Join(failures...)
		}
		// Advance even when every row failed. The next batch must not be
		// starved by an older unresolved or permanently conflicting request.
		cursor = &checks[len(checks)-1]
	}
}
