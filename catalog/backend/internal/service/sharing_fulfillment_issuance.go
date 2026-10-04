package service

import (
	"context"
	"net/http"
	"time"

	"github.com/addp/catalog/internal/models"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type sharingFulfillmentIssuanceAuthority interface {
	sharingFulfillmentAuthority
	ReadGrant(context.Context, int64, uuid.UUID, sharingFulfillmentBinding) (*shared.SharingFulfillmentGrantLookup, error)
	IssueGrant(context.Context, int64, uuid.UUID, sharingFulfillmentBinding) (*shared.SharingFulfillmentGrant, error)
}

// continueSharingFulfillment is the single foreground/background continuation.
// The acceptance stage keeps its original protection boundary. Issuance history
// is always read from System and is never stored as an editable Catalog fact.
func continueSharingFulfillment(ctx context.Context, db *gorm.DB, tenantID int64, entryID, requestID uuid.UUID,
	authority sharingFulfillmentIssuanceAuthority,
) (*models.FulfillmentCheck, error) {
	check, err := reconcileSharingFulfillment(ctx, db, tenantID, entryID, requestID, authority)
	if err != nil || check.GrantReconciledAt != nil {
		return check, err
	}
	binding, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil {
		return nil, err
	}
	// Each remote call gets a fresh binding, preserving the comparison baseline.
	callBinding, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil {
		return nil, err
	}
	resolution, err := authority.Read(ctx, tenantID, requestID, callBinding)
	if err != nil {
		return nil, err
	}
	if resolution == nil || resolution.RequestID != requestID || resolution.TenantID != tenantID ||
		!equalSharingFulfillmentBinding(binding, resolution.Binding) ||
		(resolution.Outcome != "accepted" && resolution.Outcome != "closed") {
		return nil, ErrSharingDecisionConflict
	}
	if resolution.Outcome == "accepted" {
		callBinding, err = decodeSharingFulfillmentBinding(check.RequestBinding)
		if err != nil {
			return nil, err
		}
		lookup, err := authority.ReadGrant(ctx, tenantID, requestID, callBinding)
		if err != nil {
			return nil, err
		}
		if lookup == nil || (lookup.Found && !validSharingGrant(requestID, lookup.Grant)) || (!lookup.Found && lookup.Grant != nil) {
			return nil, ErrSharingDecisionConflict
		}
		if !lookup.Found {
			callBinding, err = decodeSharingFulfillmentBinding(check.RequestBinding)
			if err != nil {
				return nil, err
			}
			grant, issueErr := authority.IssueGrant(ctx, tenantID, requestID, callBinding)
			if issueErr != nil {
				status, _ := client.TenantAPIStatusCode(issueErr)
				code, _ := client.TenantAPIErrorCode(issueErr)
				if status != http.StatusConflict || (code != "engine_access_grant_window_expired" && code != "engine_access_fulfillment_closed") {
					return nil, issueErr
				}
			} else if !validSharingGrant(requestID, grant) {
				return nil, ErrSharingDecisionConflict
			}
		}
	}
	var result *models.FulfillmentCheck
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entry models.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, entryID).Take(&entry).Error; err != nil {
			return sharingBasisReadError(err)
		}
		var current models.FulfillmentCheck
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND catalog_entry_id = ? AND request_id = ?", tenantID, entryID, requestID).Take(&current).Error; err != nil {
			return sharingBasisReadError(err)
		}
		stored, err := decodeSharingFulfillmentBinding(current.RequestBinding)
		if err != nil || !equalSharingFulfillmentBinding(binding, stored) || current.ResolvedAt == nil {
			return ErrSharingDecisionConflict
		}
		if current.GrantReconciledAt == nil {
			now := time.Now().UTC()
			if tx.Dialector.Name() == "postgres" {
				if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
					return err
				}
			}
			update := tx.Model(&models.FulfillmentCheck{}).Where("tenant_id = ? AND request_id = ? AND grant_reconciled_at IS NULL", tenantID, requestID).Update("grant_reconciled_at", now)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return ErrSharingDecisionConflict
			}
			current.GrantReconciledAt = &now
		}
		result = &current
		return nil
	})
	return result, err
}

func validSharingGrant(requestID uuid.UUID, grant *shared.SharingFulfillmentGrant) bool {
	return grant != nil && grant.RequestID == requestID && !grant.GrantedAt.IsZero()
}
