package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Internal storage command, not an authenticated service. PrepareSharingFulfillment
// derives Operator from verified User AuthContext and checks the current handling
// scope before preparing. System independently checks recipient and IAM at first
// acceptance. These well-formed IDs alone prove none of those qualifications.
type sharingFulfillmentOperator = authorization.SharingFulfillmentOperator

type sharingFulfillmentPreparation struct {
	RequestID          uuid.UUID
	CallerPrincipalID  int64
	Operator           sharingFulfillmentOperator
	RequirementVersion int64
	Path               plugin.EngineCatalogPath
}

type sharingFulfillmentBinding = authorization.SharingFulfillmentBinding

// prepareSharingFulfillment has no network IO or acceptance side effect. The
// caller must commit this owner transaction before sending, and must not treat
// a returned (including resolved) check as approval or a System receipt.
func prepareSharingFulfillment(ctx context.Context, tx *gorm.DB, tenantID int64, entryID, decisionID uuid.UUID,
	input sharingFulfillmentPreparation,
) (*models.FulfillmentCheck, bool, error) {
	if tx == nil || tenantID <= 0 || entryID == uuid.Nil || decisionID == uuid.Nil || input.RequestID == uuid.Nil ||
		input.CallerPrincipalID <= 0 || input.Operator.PrincipalID <= 0 || input.Operator.MembershipID <= 0 ||
		input.Operator.AuthorizationVersion <= 0 || input.RequirementVersion <= 0 {
		return nil, false, ErrInvalidEntryUpdate
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, false, ErrInvalidEntryUpdate
	}
	var entry models.Entry
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, entryID).Take(&entry).Error; err != nil {
		return nil, false, sharingBasisReadError(err)
	}
	var decision models.SharingDecision
	if err := tx.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ? AND id = ?", tenantID, entryID, decisionID).Take(&decision).Error; err != nil {
		return nil, false, sharingBasisReadError(err)
	}
	var originalPath plugin.EngineCatalogPath
	if err := json.Unmarshal(decision.CatalogPath, &originalPath); err != nil {
		return nil, false, err
	}
	expectedPath, err := authorization.EncodeSharingTarget(originalPath)
	if err != nil {
		return nil, false, ErrSharingDecisionConflict
	}
	actualPath, err := authorization.EncodeSharingTarget(input.Path)
	if err != nil || !bytes.Equal(expectedPath, actualPath) {
		return nil, false, ErrSharingDecisionConflict
	}
	// Catalog stores the complete exact request, not a second decision or
	// System outcome. Transport fields come from the future trusted consumer;
	// immutable business parameters come only from owner-persisted history.
	expected := sharingFulfillmentBinding{input.CallerPrincipalID, input.Operator, originalPath, decision.ID, input.RequirementVersion,
		decision.RecipientType, decision.RecipientID, decision.Action, decision.ExpiryMode, decision.ExpiresAt}
	binding, err := json.Marshal(expected)
	if err != nil {
		return nil, false, err
	}
	var existing models.FulfillmentCheck
	err = tx.WithContext(ctx).Where("request_id = ?", input.RequestID).Take(&existing).Error
	if err == nil {
		// Decode the typed stored binding, preserving int64/uint64 values. JSONB
		// changes ordering/whitespace, not the complete names or numeric IDs.
		stored, err := decodeSharingFulfillmentBinding(existing.RequestBinding)
		if err != nil {
			return nil, false, err
		}
		if existing.TenantID != tenantID || existing.CatalogEntryID != entryID || !equalSharingFulfillmentBinding(stored, expected) {
			return nil, false, ErrSharingDecisionConflict
		}
		return &existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	if _, err := lockCurrentSharingDecisionBasis(ctx, tx, tenantID, entryID, decisionID); err != nil {
		return nil, false, err
	}
	check := models.FulfillmentCheck{RequestID: input.RequestID, TenantID: tenantID, CatalogEntryID: entryID, RequestBinding: binding}
	if err := tx.WithContext(ctx).Create(&check).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "fulfillment_checks_pkey" {
			// Concurrent reuse on another aggregate cannot be serialized by this
			// entry lock. Abort the caller transaction, never continue after 23505.
			return nil, false, ErrSharingDecisionConflict
		}
		return nil, false, err
	}
	return &check, true, nil
}
