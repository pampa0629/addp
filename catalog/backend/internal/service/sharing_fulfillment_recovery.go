package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Internal transport boundary, not a public DTO or proof of trusted System
// identity. A production adapter must authenticate the authority and translate
// only its explicit missing outcome into errSharingFulfillmentNotFound; never
// translate timeouts, permission failures or an unrelated HTTP 404 into it.
var errSharingFulfillmentNotFound = errors.New("authority fulfillment outcome not found")

type sharingFulfillmentResolution struct {
	RequestID uuid.UUID
	TenantID  int64
	Binding   sharingFulfillmentBinding
	Outcome   string
}

type sharingFulfillmentAuthority interface {
	Read(context.Context, int64, uuid.UUID, sharingFulfillmentBinding) (*sharingFulfillmentResolution, error)
	Close(context.Context, int64, uuid.UUID, sharingFulfillmentBinding) (*sharingFulfillmentResolution, error)
}

// reconcileSharingFulfillment consumes committed preparation history only.
// Network IO cannot occur under Catalog locks or within a caller transaction.
// It does not accept, approve, send a new request, grant access or authenticate
// its adapter. No production scheduler/API is wired to this internal command.
func reconcileSharingFulfillment(ctx context.Context, db *gorm.DB, tenantID int64, entryID, requestID uuid.UUID,
	authority sharingFulfillmentAuthority,
) (*models.FulfillmentCheck, error) {
	if db == nil || tenantID <= 0 || entryID == uuid.Nil || requestID == uuid.Nil || authority == nil {
		return nil, ErrInvalidEntryUpdate
	}
	if _, ok := db.Statement.ConnPool.(gorm.TxCommitter); ok {
		return nil, ErrInvalidEntryUpdate
	}
	var check models.FulfillmentCheck
	if err := db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ? AND request_id = ?", tenantID, entryID, requestID).Take(&check).Error; err != nil {
		return nil, sharingBasisReadError(err)
	}
	if check.ResolvedAt != nil {
		return &check, nil
	}
	binding, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil {
		return nil, err
	}
	// Give each adapter call an independent binding: an implementation must not
	// mutate the persisted comparison baseline through slices/time pointers.
	readBinding, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil {
		return nil, err
	}
	resolution, err := authority.Read(ctx, tenantID, requestID, readBinding)
	if errors.Is(err, errSharingFulfillmentNotFound) {
		closeBinding, decodeErr := decodeSharingFulfillmentBinding(check.RequestBinding)
		if decodeErr != nil {
			return nil, decodeErr
		}
		resolution, err = authority.Close(ctx, tenantID, requestID, closeBinding)
	}
	if err != nil {
		return nil, err
	}
	if resolution == nil || resolution.RequestID != requestID || resolution.TenantID != tenantID ||
		(resolution.Outcome != "accepted" && resolution.Outcome != "closed") || !equalSharingFulfillmentBinding(binding, resolution.Binding) {
		return nil, ErrSharingDecisionConflict
	}
	// The authority result is immutable. There is no network IO in the local
	// commit; a failed local commit can retry the same authority result safely.
	var result *models.FulfillmentCheck
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = resolveSharingFulfillment(ctx, tx, tenantID, entryID, requestID, binding)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// resolveSharingFulfillment is the local completion of a validated immutable
// authority result, not a permission-free way to clear unresolved history.
func resolveSharingFulfillment(ctx context.Context, tx *gorm.DB, tenantID int64, entryID, requestID uuid.UUID,
	binding sharingFulfillmentBinding,
) (*models.FulfillmentCheck, error) {
	if tx == nil || tenantID <= 0 || entryID == uuid.Nil || requestID == uuid.Nil {
		return nil, ErrInvalidEntryUpdate
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, ErrInvalidEntryUpdate
	}
	var entry models.Entry
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, entryID).Take(&entry).Error; err != nil {
		return nil, sharingBasisReadError(err)
	}
	var check models.FulfillmentCheck
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND catalog_entry_id = ? AND request_id = ?", tenantID, entryID, requestID,
	).Take(&check).Error; err != nil {
		return nil, sharingBasisReadError(err)
	}
	stored, err := decodeSharingFulfillmentBinding(check.RequestBinding)
	if err != nil {
		return nil, err
	}
	if !equalSharingFulfillmentBinding(stored, binding) {
		return nil, ErrSharingDecisionConflict
	}
	if check.ResolvedAt != nil {
		return &check, nil
	}
	now := time.Now().UTC()
	if tx.Dialector.Name() == "postgres" {
		if err := tx.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return nil, err
		}
	}
	updated := tx.WithContext(ctx).Model(&models.FulfillmentCheck{}).Where(
		"tenant_id = ? AND catalog_entry_id = ? AND request_id = ? AND resolved_at IS NULL", tenantID, entryID, requestID,
	).Update("resolved_at", now)
	if updated.Error != nil {
		return nil, updated.Error
	}
	if updated.RowsAffected != 1 {
		return nil, ErrSharingDecisionConflict
	}
	check.ResolvedAt = &now
	return &check, nil
}

func decodeSharingFulfillmentBinding(raw json.RawMessage) (sharingFulfillmentBinding, error) {
	var binding sharingFulfillmentBinding
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&binding); err != nil {
		return binding, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return binding, ErrSharingDecisionConflict
	}
	if binding.CallerPrincipalID <= 0 || binding.Operator.PrincipalID <= 0 || binding.Operator.MembershipID <= 0 ||
		binding.Operator.AuthorizationVersion <= 0 || binding.DecisionID == uuid.Nil || binding.RequirementVersion <= 0 ||
		binding.RecipientID <= 0 || (binding.RecipientType != "user" && binding.RecipientType != "project_group") || binding.Action != "read" {
		return binding, ErrSharingDecisionConflict
	}
	if _, err := authorization.EncodeSharingTarget(binding.Path); err != nil {
		return binding, ErrSharingDecisionConflict
	}
	expiresAt, err := authorization.NormalizeSharingExpiry(binding.ExpiryMode, binding.ExpiresAt)
	if err != nil {
		return binding, ErrSharingDecisionConflict
	}
	binding.ExpiresAt = expiresAt
	return binding, nil
}

func equalSharingFulfillmentBinding(left, right sharingFulfillmentBinding) bool {
	var err error
	left.ExpiresAt, err = authorization.NormalizeSharingExpiry(left.ExpiryMode, left.ExpiresAt)
	if err != nil {
		return false
	}
	right.ExpiresAt, err = authorization.NormalizeSharingExpiry(right.ExpiryMode, right.ExpiresAt)
	if err != nil {
		return false
	}
	a, err := json.Marshal(left)
	if err != nil {
		return false
	}
	b, err := json.Marshal(right)
	return err == nil && bytes.Equal(a, b)
}
