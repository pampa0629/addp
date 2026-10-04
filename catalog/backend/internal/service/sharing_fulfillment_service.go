package service

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SharingFulfillmentInput struct {
	RequestID          uuid.UUID
	DecisionID         uuid.UUID
	RequirementVersion int64
}

type SharingFulfillmentResult struct {
	RequestID  uuid.UUID                                   `json:"request_id"`
	State      string                                      `json:"state"`
	Resolution *authorization.SharingFulfillmentResolution `json:"resolution"`
}

func (s *EntryService) WithSharingFulfillmentClient(client *commonClient.SystemFulfillmentClient) *EntryService {
	s.sharingFulfillment = client
	return s
}

func (s *EntryService) PrepareSharingFulfillment(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID,
	input SharingFulfillmentInput, auth authorization.AuthContext, userToken string,
) (*SharingFulfillmentResult, error) {
	p, m, v, err := sharingUserProvenance(auth, tenantID, "catalog.entry.read", sourceHandlingPermission)
	if err != nil {
		return nil, ErrSharingHandlingForbidden
	}
	if input.RequestID == uuid.Nil || input.DecisionID == uuid.Nil || input.RequirementVersion <= 0 {
		return nil, ErrInvalidEntryUpdate
	}
	if s.sharingFulfillment == nil || s.sharingHandling == nil {
		return nil, ErrReferenceValidationUnavailable
	}
	var entry models.Entry
	if err := s.visibleEntriesQuery(ctx, tenantID, access).Where("entries.id = ?", entryID).Take(&entry).Error; err != nil {
		return nil, sharingBasisReadError(err)
	}
	var decision models.SharingDecision
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ? AND id = ?", tenantID, entryID, input.DecisionID).Take(&decision).Error; err != nil {
		return nil, sharingBasisReadError(err)
	}
	scope, err := s.sharingHandling.GetEngineAccessHandlingScope(ctx, decision.EngineID, userToken)
	operator := authorization.SharingFulfillmentOperator{PrincipalID: p, MembershipID: m, AuthorizationVersion: v}
	if err != nil {
		if status, ok := commonClient.SystemAPIStatusCode(err); ok && (status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound) {
			return nil, ErrSharingHandlingForbidden
		}
		return nil, ErrReferenceValidationUnavailable
	}
	if scope == nil || scope.TenantID != tenantID || scope.EngineID != decision.EngineID || scope.Operator != operator || scope.VerifiedAt.IsZero() {
		return nil, ErrReferenceValidationUnavailable
	}
	remote := s.sharingFulfillment.WithTenantID(uint(tenantID))
	caller, err := remote.CurrentPrincipal(ctx)
	if err != nil {
		return nil, ErrReferenceValidationUnavailable
	}
	var path plugin.EngineCatalogPath
	if err := json.Unmarshal(decision.CatalogPath, &path); err != nil {
		return nil, err
	}
	var check *models.FulfillmentCheck
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		local := *s
		local.db = tx
		// Entry serialization precedes the final visibility/expiry checks.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, entryID).Take(&entry).Error; err != nil {
			return sharingBasisReadError(err)
		}
		now := time.Now().UTC()
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				return err
			}
		}
		if !sharingUserPermissionsAt(auth, now, "catalog.entry.read", sourceHandlingPermission) {
			return ErrSharingHandlingForbidden
		}
		currentAccess := access
		currentAccess.Inventory = access.Inventory && sharingUserPermissionsAt(auth, now, "catalog.inventory.read")
		if err := local.visibleEntriesQuery(ctx, tenantID, currentAccess).Where("entries.id = ?", entryID).Take(&entry).Error; err != nil {
			return sharingBasisReadError(err)
		}
		var err error
		check, _, err = prepareSharingFulfillment(ctx, tx, tenantID, entryID, input.DecisionID,
			sharingFulfillmentPreparation{input.RequestID, caller, operator, input.RequirementVersion, path})
		return err
	})
	if err != nil {
		return nil, err
	}
	binding, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil {
		return nil, err
	}
	result := &SharingFulfillmentResult{RequestID: input.RequestID, State: "pending"}
	// Commit-before-send: a transport error is uncertain, not a local rollback.
	resolution, err := remote.Accept(ctx, input.RequestID, binding)
	if err != nil {
		return result, nil
	}
	if _, err := runtimeFulfillmentResolution(resolution); err != nil || resolution.RequestID != input.RequestID || resolution.TenantID != tenantID || !equalSharingFulfillmentBinding(binding, resolution.Binding) {
		return result, nil
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := resolveSharingFulfillment(ctx, tx, tenantID, entryID, input.RequestID, binding)
		return err
	})
	if err != nil {
		return result, nil
	}
	result.State, result.Resolution = resolution.Outcome, resolution
	// A crash or uncertain issuance leaves the durable recovery marker open.
	// The acceptance response never claims that content access is effective.
	callContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, _ = continueSharingFulfillment(callContext, s.db, tenantID, entryID, input.RequestID,
		&systemSharingFulfillmentAuthority{client: s.sharingFulfillment})
	return result, nil
}

// ReadSharingFulfillmentBasis is a local transaction only. A dedicated runtime
// guard must authenticate addp-system before this owner method is called.
func (s *EntryService) ReadSharingFulfillmentBasis(ctx context.Context, tenantID int64, requestID uuid.UUID,
	binding authorization.SharingFulfillmentBinding,
) (*authorization.SharingFulfillmentBasis, error) {
	if tenantID <= 0 || requestID == uuid.Nil || binding.Validate() != nil {
		return nil, ErrInvalidEntryUpdate
	}
	var result *authorization.SharingFulfillmentBasis
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var check models.FulfillmentCheck
		if err := tx.Where("tenant_id = ? AND request_id = ? AND resolved_at IS NULL", tenantID, requestID).Take(&check).Error; err != nil {
			return sharingBasisReadError(err)
		}
		stored, err := decodeSharingFulfillmentBinding(check.RequestBinding)
		if err != nil {
			return err
		}
		if !equalSharingFulfillmentBinding(stored, binding) {
			return ErrSharingDecisionConflict
		}
		decision, err := lockCurrentSharingDecisionBasis(ctx, tx, tenantID, check.CatalogEntryID, binding.DecisionID)
		if err != nil {
			return err
		}
		// Resolver uses entry -> check order; recheck after waiting for that entry.
		if err := tx.Where("tenant_id = ? AND request_id = ? AND resolved_at IS NULL", tenantID, requestID).Take(&check).Error; err != nil {
			return sharingBasisReadError(err)
		}
		result = &authorization.SharingFulfillmentBasis{RequestID: requestID, TenantID: tenantID, Binding: stored,
			Confirmation: authorization.SharingFulfillmentOperator{PrincipalID: decision.ConfirmedBy, MembershipID: decision.ConfirmerMembershipID, AuthorizationVersion: decision.AuthorizationVersion}}
		return nil
	})
	return result, err
}
