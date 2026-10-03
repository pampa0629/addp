package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Recipient labels are observations, not organization membership or grants.
type SharingRecipientCandidate struct {
	RecipientType string `json:"recipient_type" enums:"user,project_group"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Code          string `json:"code,omitempty"`
	Status        string `json:"status"`
}

type sharingRecipientBasis struct {
	Source models.SourceBinding
	Owner  models.Responsibility
}

// A short read-only snapshot is closed before any owner HTTP call. Reusing the
// original owner UUID prevents transfer and reappointment from reviving scope.
func (s *EntryService) sharingRecipientBasis(ctx context.Context, tenantID, principal int64, access EntryAccess, entryID uuid.UUID, auth authorization.AuthContext) (sharingRecipientBasis, error) {
	var basis sharingRecipientBasis
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				return err
			}
		}
		if !sharingPermissionsAt(auth, now) {
			return ErrSharingConfirmationForbidden
		}
		local := *s
		local.db = tx
		access.Inventory = access.Inventory && sharingUserPermissionsAt(auth, now, "catalog.inventory.read")
		var entry models.Entry
		if err := local.visibleEntriesQuery(ctx, tenantID, access).Where("entries.id = ?", entryID).Take(&entry).Error; err != nil {
			return sharingBasisReadError(err)
		}
		if entry.EntryType != models.EntryTypeDataItem || entry.EntryStatus != models.EntryStatusActive || entry.GovernanceStatus == models.GovernanceStatusDeprecated {
			return ErrSharingTargetUnsupported
		}
		if err := tx.Where("tenant_id = ? AND catalog_entry_id = ? AND is_current = ? AND source_status = ? AND source_module = ? AND source_type = ?",
			tenantID, entryID, true, models.SourceStatusActive, models.SourceModuleMeta, models.SourceTypeDataItem).Take(&basis.Source).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSharingTargetUnsupported
			}
			return err
		}
		if err := tx.Where("tenant_id = ? AND catalog_entry_id = ? AND role = ? AND subject_type = 'user' AND subject_id = ? AND status = ?",
			tenantID, entryID, models.ResponsibilityRoleBusinessOwner, principal, models.ResponsibilityStatusActive).Take(&basis.Owner).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSharingConfirmationForbidden
			}
			return err
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return basis, err
}

func (s *EntryService) ListSharingRecipientCandidates(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID, auth authorization.AuthContext, recipientType, search string, page, size int) ([]SharingRecipientCandidate, int64, error) {
	principal, _, _, err := sharingConfirmer(auth, tenantID)
	if err != nil {
		return nil, 0, err
	}
	search = strings.TrimSpace(search)
	if s == nil || s.db == nil || entryID == uuid.Nil || !oneOf(recipientType, "user", "project_group") || page <= 0 || size <= 0 || size > maxReferenceCandidatePageSize || page-1 > math.MaxInt/size || !utf8.ValidString(search) || strings.ContainsRune(search, '\x00') || utf8.RuneCountInString(search) > 100 {
		return nil, 0, ErrInvalidPage
	}
	before, err := s.sharingRecipientBasis(ctx, tenantID, principal, access, entryID, auth)
	if err != nil {
		return nil, 0, err
	}
	if s.systemCandidates == nil {
		return nil, 0, ErrReferenceValidationUnavailable
	}
	pageResult, err := s.systemCandidates.ListReferenceCandidates(ctx, tenantID, recipientType, search, page, size)
	if err != nil || pageResult == nil || pageResult.Page != page || pageResult.PageSize != size || pageResult.Total < 0 || len(pageResult.Data) > size {
		return nil, 0, ErrReferenceValidationUnavailable
	}
	expectedPages := pageResult.Total / int64(size)
	if pageResult.Total%int64(size) != 0 {
		expectedPages++
	}
	remaining := pageResult.Total - int64((page-1)*size)
	if remaining < 0 {
		remaining = 0
	}
	if int64(pageResult.TotalPages) != expectedPages || int64(len(pageResult.Data)) > remaining {
		return nil, 0, ErrReferenceValidationUnavailable
	}
	result := make([]SharingRecipientCandidate, 0, len(pageResult.Data))
	seen := make(map[string]bool, len(pageResult.Data))
	for _, candidate := range pageResult.Data {
		if candidate.ReferenceType != recipientType || !canonicalPositiveID(candidate.ID) || strings.TrimSpace(candidate.Name) == "" || candidate.Status != "active" || seen[candidate.ID] {
			return nil, 0, ErrReferenceValidationUnavailable
		}
		seen[candidate.ID] = true
		result = append(result, SharingRecipientCandidate{RecipientType: recipientType, ID: candidate.ID, Name: candidate.Name, Code: candidate.Code, Status: candidate.Status})
	}
	after, err := s.sharingRecipientBasis(ctx, tenantID, principal, access, entryID, auth)
	if err != nil {
		return nil, 0, err
	}
	if before.Owner.ID != after.Owner.ID || before.Source.ID != after.Source.ID || before.Source.SourceVersion != after.Source.SourceVersion || before.Source.SourceIdentity != after.Source.SourceIdentity {
		return nil, 0, ErrSharingDecisionConflict
	}
	return result, pageResult.Total, nil
}
