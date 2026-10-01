package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockCurrentSharingDecisionBasis consumes owner-persisted history, never a
// caller's copy of a decision. Its locks remain in the caller's transaction;
// a future preparation must persist its pending check before that commit.
// This local predicate proves neither trusted caller provenance nor current IAM
// qualification, remote source path, recipient eligibility or System acceptance.
// Do not expose it as a permission-free runtime API or network inside these locks.
func lockCurrentSharingDecisionBasis(ctx context.Context, tx *gorm.DB, tenantID int64, entryID, decisionID uuid.UUID) (*models.SharingDecision, error) {
	if tx == nil || tenantID <= 0 || entryID == uuid.Nil || decisionID == uuid.Nil {
		return nil, ErrInvalidEntryUpdate
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, ErrInvalidEntryUpdate
	}
	var decision models.SharingDecision
	if err := tx.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ? AND id = ?", tenantID, entryID, decisionID).Take(&decision).Error; err != nil {
		return nil, sharingBasisReadError(err)
	}
	var entry models.Entry
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, entryID).Take(&entry).Error; err != nil {
		return nil, sharingBasisReadError(err)
	}
	if entry.EntryType != models.EntryTypeDataItem || entry.EntryStatus != models.EntryStatusActive ||
		entry.GovernanceStatus == models.GovernanceStatusDeprecated || entry.Version < decision.EntryVersion {
		return nil, ErrSharingTargetUnsupported
	}
	var source models.SourceBinding
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", tenantID, entryID, true).Take(&source).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSharingTargetUnsupported
		}
		return nil, err
	}
	if source.ID != decision.SourceBindingID || source.SourceVersion != decision.SourceVersion ||
		source.SourceModule != models.SourceModuleMeta || source.SourceType != models.SourceTypeDataItem || source.SourceStatus != models.SourceStatusActive {
		return nil, ErrSharingTargetUnsupported
	}
	var owner models.Responsibility
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND catalog_entry_id = ? AND id = ? AND role = ? AND subject_type = ? AND subject_id = ? AND status = ?",
		tenantID, entryID, decision.ResponsibilityID, models.ResponsibilityRoleBusinessOwner, "user", decision.ConfirmedBy, models.ResponsibilityStatusActive,
	).Take(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSharingConfirmationForbidden
		}
		return nil, err
	}
	// Permissions/identity must be rechecked by the trusted consumer too. A
	// cached VerifiedAt/snapshot on this relation never proves current IAM state.
	now := time.Now().UTC()
	if tx.Dialector.Name() == "postgres" {
		if err := tx.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return nil, err
		}
	}
	if !authorization.SharingExpiryFuture(decision.ExpiryMode, decision.ExpiresAt, now) {
		return nil, ErrInvalidEntryUpdate
	}
	return &decision, nil
}

func sharingBasisReadError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrEntryNotFound
	}
	return err
}
