package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Only original request parameters needed for explicit replay are exposed.
// Machine identity, original IAM versions and purpose text are not public DTOs.
type SharingFulfillmentRequest struct {
	RequestID          uuid.UUID             `json:"request_id"`
	DecisionID         uuid.UUID             `json:"decision_id"`
	RequirementVersion int64                 `json:"requirement_version,string" swaggertype:"string"`
	Target             SharingDecisionTarget `json:"target"`
	RecipientType      string                `json:"recipient_type"`
	RecipientID        int64                 `json:"recipient_id,string" swaggertype:"string"`
	Action             string                `json:"action"`
	ExpiryMode         string                `json:"expiry_mode" enums:"at_time,until_revoked"`
	ExpiresAt          *time.Time            `json:"expires_at" format:"date-time" extensions:"x-nullable"`
	CreatedAt          time.Time             `json:"created_at"`
}

type SharingFulfillmentHistory struct {
	SharingFulfillmentRequest
	State      string     `json:"state" enums:"pending,accepted,closed"`
	RecordedAt *time.Time `json:"recorded_at" format:"date-time" extensions:"x-nullable"`
	Deadline   *time.Time `json:"deadline" format:"date-time" extensions:"x-nullable"`
	GrantedAt  *time.Time `json:"granted_at" format:"date-time" extensions:"x-nullable"`
}

const originalFulfillmentEngine = "CAST(request_binding -> 'path' ->> 'engine_id' AS BIGINT)"

func (s *EntryService) ownFulfillmentQuery(ctx context.Context, tenantID int64, entryID uuid.UUID, principal int64) *gorm.DB {
	return s.db.WithContext(ctx).Model(&models.FulfillmentCheck{}).
		Where("tenant_id = ? AND catalog_entry_id = ? AND request_binding -> 'operator' ->> 'principal_id' = ?",
			tenantID, entryID, strconv.FormatInt(principal, 10))
}

// HTTP is deliberately outside each local read-only snapshot. Historical
// source/owner validity is not a new approval prerequisite; current visibility
// and human permissions remain mandatory even for settled/deprecated history.
func (s *EntryService) readFulfillmentSnapshot(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID,
	auth authorization.AuthContext, read func(*EntryService) error,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		local := *s
		local.db = tx
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
		var entry models.Entry
		if err := local.visibleEntriesQuery(ctx, tenantID, currentAccess).Where("entries.id = ?", entryID).Take(&entry).Error; err != nil {
			return sharingBasisReadError(err)
		}
		return read(&local)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}

func (s *EntryService) checkFulfillmentHistoryScope(ctx context.Context, tenantID, engineID int64,
	operator authorization.SharingFulfillmentOperator, userToken string,
) error {
	if s.sharingHandling == nil {
		return ErrReferenceValidationUnavailable
	}
	scope, err := s.sharingHandling.GetEngineAccessHandlingScope(ctx, engineID, userToken)
	if err != nil {
		if status, ok := commonClient.SystemAPIStatusCode(err); ok && (status == http.StatusForbidden || status == http.StatusNotFound) {
			return ErrSharingHandlingForbidden
		}
		return ErrReferenceValidationUnavailable
	}
	if scope == nil || scope.TenantID != tenantID || scope.EngineID != engineID || scope.Operator != operator || scope.VerifiedAt.IsZero() {
		return ErrReferenceValidationUnavailable
	}
	return nil
}

func fulfillmentRequestView(check models.FulfillmentCheck) (SharingFulfillmentRequest, authorization.SharingFulfillmentBinding, error) {
	binding, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil {
		return SharingFulfillmentRequest{}, binding, err
	}
	return SharingFulfillmentRequest{
		RequestID: check.RequestID, DecisionID: binding.DecisionID, RequirementVersion: binding.RequirementVersion,
		Target:        SharingDecisionTarget{Version: binding.Path.Version, EngineID: strconv.FormatUint(uint64(binding.Path.EngineID), 10), Segments: binding.Path.Segments},
		RecipientType: binding.RecipientType, RecipientID: binding.RecipientID, Action: binding.Action,
		ExpiryMode: binding.ExpiryMode, ExpiresAt: binding.ExpiresAt, CreatedAt: check.CreatedAt,
	}, binding, nil
}

func (s *EntryService) ListSharingFulfillments(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID,
	auth authorization.AuthContext, userToken string, page, size int,
) ([]SharingFulfillmentRequest, int64, error) {
	p, m, v, err := sharingUserProvenance(auth, tenantID, "catalog.entry.read", sourceHandlingPermission)
	if err != nil {
		return nil, 0, ErrSharingHandlingForbidden
	}
	if s == nil || s.db == nil || entryID == uuid.Nil || page <= 0 || size <= 0 || size > 100 || page-1 > math.MaxInt/size {
		return nil, 0, ErrInvalidEntryUpdate
	}
	var engines []int64
	err = s.readFulfillmentSnapshot(ctx, tenantID, access, entryID, auth, func(local *EntryService) error {
		return local.ownFulfillmentQuery(ctx, tenantID, entryID, p).Distinct(originalFulfillmentEngine).Order(originalFulfillmentEngine).Pluck(originalFulfillmentEngine, &engines).Error
	})
	if err != nil {
		return nil, 0, err
	}
	operator := authorization.SharingFulfillmentOperator{PrincipalID: p, MembershipID: m, AuthorizationVersion: v}
	allowed := make([]int64, 0, len(engines))
	for _, engine := range engines {
		err := s.checkFulfillmentHistoryScope(ctx, tenantID, engine, operator, userToken)
		if errors.Is(err, ErrSharingHandlingForbidden) {
			continue // Neither rows nor counts expose a no-longer-managed engine.
		}
		if err != nil {
			return nil, 0, err
		}
		allowed = append(allowed, engine)
	}
	rows := make([]SharingFulfillmentRequest, 0)
	var total int64
	err = s.readFulfillmentSnapshot(ctx, tenantID, access, entryID, auth, func(local *EntryService) error {
		if len(allowed) == 0 {
			return nil
		}
		query := func() *gorm.DB {
			return local.ownFulfillmentQuery(ctx, tenantID, entryID, p).Where(originalFulfillmentEngine+" IN ?", allowed)
		}
		if err := query().Count(&total).Error; err != nil {
			return err
		}
		var checks []models.FulfillmentCheck
		if err := query().Order("created_at DESC, request_id DESC").Offset((page - 1) * size).Limit(size).Find(&checks).Error; err != nil {
			return err
		}
		for _, check := range checks {
			view, _, err := fulfillmentRequestView(check)
			if err != nil {
				return err
			}
			rows = append(rows, view)
		}
		return nil
	})
	return rows, total, err
}

func (s *EntryService) GetSharingFulfillment(ctx context.Context, tenantID int64, access EntryAccess, entryID, requestID uuid.UUID,
	auth authorization.AuthContext, userToken string,
) (*SharingFulfillmentHistory, error) {
	p, m, v, err := sharingUserProvenance(auth, tenantID, "catalog.entry.read", sourceHandlingPermission)
	if err != nil {
		return nil, ErrSharingHandlingForbidden
	}
	if s == nil || s.db == nil || entryID == uuid.Nil || requestID == uuid.Nil {
		return nil, ErrInvalidEntryUpdate
	}
	var check models.FulfillmentCheck
	read := func(local *EntryService) error {
		return sharingBasisReadError(local.ownFulfillmentQuery(ctx, tenantID, entryID, p).Where("request_id = ?", requestID).Take(&check).Error)
	}
	if err := s.readFulfillmentSnapshot(ctx, tenantID, access, entryID, auth, read); err != nil {
		return nil, err
	}
	view, binding, err := fulfillmentRequestView(check)
	if err != nil {
		return nil, err
	}
	operator := authorization.SharingFulfillmentOperator{PrincipalID: p, MembershipID: m, AuthorizationVersion: v}
	if err := s.checkFulfillmentHistoryScope(ctx, tenantID, int64(binding.Path.EngineID), operator, userToken); err != nil {
		return nil, err
	}
	result, err := s.resolveSharingHistory(ctx, tenantID, check)
	if err != nil {
		return nil, err
	}
	// Current qualification is distinct from the immutable original operator.
	// A version change is not permission to replay with altered parameters.
	if err := s.checkFulfillmentHistoryScope(ctx, tenantID, int64(binding.Path.EngineID), operator, userToken); err != nil {
		return nil, err
	}
	if err := s.readFulfillmentSnapshot(ctx, tenantID, access, entryID, auth, read); err != nil {
		return nil, err
	}
	current, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil || !equalSharingFulfillmentBinding(current, binding) || !check.CreatedAt.Equal(view.CreatedAt) {
		return nil, ErrSharingDecisionConflict
	}
	if result.State == "pending" && check.ResolvedAt != nil {
		return nil, ErrReferenceValidationUnavailable
	}
	// No prepare, close, audit, local settlement write or source Grant here.
	return result, nil
}

// A single read-only authority projection serves both the original operator
// and business review. Its callers enforce their distinct human scopes before
// and after networking. A historical signature is never a current access Allow.
func (s *EntryService) resolveSharingHistory(ctx context.Context, tenantID int64, check models.FulfillmentCheck) (*SharingFulfillmentHistory, error) {
	view, binding, err := fulfillmentRequestView(check)
	if err != nil {
		return nil, err
	}
	if s.sharingFulfillment == nil {
		return nil, ErrReferenceValidationUnavailable
	}
	remote := s.sharingFulfillment.WithTenantID(uint(tenantID))
	lookup, err := remote.Resolve(ctx, check.RequestID, binding)
	if err != nil || lookup == nil || lookup.Found != (lookup.Resolution != nil) {
		return nil, ErrReferenceValidationUnavailable
	}
	result := &SharingFulfillmentHistory{SharingFulfillmentRequest: view, State: "pending"}
	if !lookup.Found {
		if check.ResolvedAt != nil {
			return nil, ErrReferenceValidationUnavailable
		}
		return result, nil
	}
	resolution := lookup.Resolution
	if _, err := runtimeFulfillmentResolution(resolution); err != nil || resolution.RequestID != check.RequestID || resolution.TenantID != tenantID || !equalSharingFulfillmentBinding(binding, resolution.Binding) {
		return nil, ErrReferenceValidationUnavailable
	}
	if resolution.Outcome == "accepted" && (resolution.Deadline.After(resolution.RecordedAt.Add(5*time.Minute)) || (binding.ExpiresAt != nil && resolution.Deadline.After(*binding.ExpiresAt))) {
		return nil, ErrReferenceValidationUnavailable
	}
	result.State, result.RecordedAt, result.Deadline = resolution.Outcome, &resolution.RecordedAt, resolution.Deadline
	if result.State == "accepted" {
		grant, err := remote.ResolveGrant(ctx, check.RequestID, binding)
		if err != nil || grant == nil || grant.Found != (grant.Grant != nil) {
			return nil, ErrReferenceValidationUnavailable
		}
		if grant.Found {
			if grant.Grant.RequestID != check.RequestID || grant.Grant.GrantedAt.Before(resolution.RecordedAt) || grant.Grant.GrantedAt.After(*resolution.Deadline) {
				return nil, ErrReferenceValidationUnavailable
			}
			result.GrantedAt = &grant.Grant.GrantedAt
		}
	}
	return result, nil
}

func (s *EntryService) ListSharingDecisionFulfillments(ctx context.Context, tenantID int64, access EntryAccess, entryID, decisionID uuid.UUID,
	auth authorization.AuthContext, page, size int,
) ([]SharingFulfillmentHistory, int64, error) {
	principal, _, _, err := sharingConfirmer(auth, tenantID)
	if err != nil {
		return nil, 0, err
	}
	if s == nil || s.db == nil || entryID == uuid.Nil || decisionID == uuid.Nil || page <= 0 || size <= 0 || size > 100 || page-1 > math.MaxInt/size {
		return nil, 0, ErrInvalidEntryUpdate
	}
	var checks []models.FulfillmentCheck
	var total int64
	var decision models.SharingDecision
	read := func(local *EntryService, owner bool) error {
		if err := local.sharingHistoryQuery(ctx, tenantID, entryID, principal, owner).Where("id = ?", decisionID).Take(&decision).Error; err != nil {
			return sharingBasisReadError(err)
		}
		query := func() *gorm.DB {
			return local.db.WithContext(ctx).Model(&models.FulfillmentCheck{}).Where("tenant_id = ? AND catalog_entry_id = ? AND request_binding ->> 'decision_id' = ?", tenantID, entryID, decisionID.String())
		}
		if err := query().Count(&total).Error; err != nil {
			return err
		}
		return query().Order("created_at DESC, request_id DESC").Offset((page - 1) * size).Limit(size).Find(&checks).Error
	}
	if err := s.readSharingHistorySnapshot(ctx, tenantID, access, entryID, auth, read); err != nil {
		return nil, 0, err
	}
	results := make([]SharingFulfillmentHistory, 0, len(checks))
	for _, check := range checks {
		result, err := s.resolveSharingHistory(ctx, tenantID, check)
		if err != nil {
			return nil, 0, err
		}
		results = append(results, *result)
	}
	// Do not expose results if ownership or visibility changed during networking.
	if err := s.readSharingHistorySnapshot(ctx, tenantID, access, entryID, auth, func(local *EntryService, owner bool) error {
		return sharingBasisReadError(local.sharingHistoryQuery(ctx, tenantID, entryID, principal, owner).Where("id = ?", decisionID).Take(&decision).Error)
	}); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}
