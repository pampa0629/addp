package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const sourceHandlingPermission = "system.engine_access_fulfillment.create"

type SharingHandlingScopeReader interface {
	GetEngineAccessHandlingScope(context.Context, int64, string) (*authorization.EngineAccessHandlingScope, error)
}

func (s *EntryService) WithSharingHandlingScopeReader(reader SharingHandlingScopeReader) *EntryService {
	s.sharingHandling = reader
	return s
}

// Deliberately not an embedded SharingDecision: purpose text, owner relationship
// and source-history internals are not part of another person's summary.
type SharingDecisionCandidate struct {
	ID                   uuid.UUID             `json:"id"`
	Target               SharingDecisionTarget `json:"target"`
	RecipientType        string                `json:"recipient_type"`
	RecipientID          int64                 `json:"recipient_id,string" swaggertype:"string"`
	RecipientName        string                `json:"recipient_name,omitempty"`
	RecipientCode        string                `json:"recipient_code,omitempty"`
	RecipientLabelStatus string                `json:"recipient_label_status" enums:"resolved,not_found"`
	Action               string                `json:"action"`
	ExpiryMode           string                `json:"expiry_mode" enums:"at_time,until_revoked"`
	ExpiresAt            *time.Time            `json:"expires_at" format:"date-time" extensions:"x-nullable"`
	ConfirmedBy          int64                 `json:"confirmed_by,string" swaggertype:"string"`
	ConfirmedAt          time.Time             `json:"confirmed_at"`
}

// Query the same current facts for engine selection, count and page. No network
// call holds Catalog locks, and no candidate becomes a fulfillment basis.
func (s *EntryService) sharingCandidateQuery(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID, now time.Time) *gorm.DB {
	visible := s.visibleEntriesQuery(ctx, tenantID, access).Select("entries.id").Where("entries.id = ?", entryID).
		Where("entries.entry_type = ? AND entries.entry_status = ? AND entries.governance_status <> ?",
			models.EntryTypeDataItem, models.EntryStatusActive, models.GovernanceStatusDeprecated)
	return s.db.WithContext(ctx).Model(&models.SharingDecision{}).
		Joins(`JOIN catalog.entries basis_entry ON basis_entry.id = sharing_decisions.catalog_entry_id AND basis_entry.tenant_id = sharing_decisions.tenant_id AND basis_entry.version >= sharing_decisions.entry_version`).
		Joins(`JOIN catalog.source_bindings source ON source.id = sharing_decisions.source_binding_id AND source.tenant_id = sharing_decisions.tenant_id AND source.catalog_entry_id = sharing_decisions.catalog_entry_id AND source.source_version = sharing_decisions.source_version AND source.is_current = ? AND source.source_status = ? AND source.source_module = ? AND source.source_type = ?`,
			true, models.SourceStatusActive, models.SourceModuleMeta, models.SourceTypeDataItem).
		Joins(`JOIN catalog.responsibilities owner ON owner.id = sharing_decisions.responsibility_id AND owner.tenant_id = sharing_decisions.tenant_id AND owner.catalog_entry_id = sharing_decisions.catalog_entry_id AND owner.role = ? AND owner.subject_type = 'user' AND owner.subject_id = sharing_decisions.confirmed_by AND owner.status = ?`,
			models.ResponsibilityRoleBusinessOwner, models.ResponsibilityStatusActive).
		Where("sharing_decisions.tenant_id = ? AND sharing_decisions.catalog_entry_id IN (?)", tenantID, visible).
		Where("sharing_decisions.action = ? AND (sharing_decisions.expiry_mode = ? OR (sharing_decisions.expiry_mode = ? AND sharing_decisions.expires_at > ?))",
			"read", authorization.SharingExpiryUntilRevoked, authorization.SharingExpiryAtTime, now)
}

func (s *EntryService) ListSharingDecisionCandidates(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID,
	auth authorization.AuthContext, userToken string, page, size int) ([]SharingDecisionCandidate, int64, error) {
	principal, membership, version, err := sharingUserProvenance(auth, tenantID, "catalog.entry.read", sourceHandlingPermission)
	if err != nil {
		return nil, 0, ErrSharingHandlingForbidden
	}
	if s == nil || s.db == nil || entryID == uuid.Nil || page <= 0 || size <= 0 || size > 100 || page-1 > math.MaxInt/size {
		return nil, 0, ErrInvalidEntryUpdate
	}
	var entry models.Entry
	if err := s.visibleEntriesQuery(ctx, tenantID, access).Where("entries.id = ?", entryID).Take(&entry).Error; err != nil {
		return nil, 0, sharingBasisReadError(err)
	}
	result := make([]SharingDecisionCandidate, 0)
	var first models.SharingDecision
	err = s.sharingCandidateQuery(ctx, tenantID, access, entryID, time.Now().UTC()).Select("sharing_decisions.engine_id").Order("sharing_decisions.created_at DESC, sharing_decisions.id DESC").Take(&first).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if s.sharingHandling == nil {
		return nil, 0, ErrReferenceValidationUnavailable
	}
	scope, err := s.sharingHandling.GetEngineAccessHandlingScope(ctx, first.EngineID, userToken)
	if err != nil {
		if status, ok := commonClient.SystemAPIStatusCode(err); ok && (status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound) {
			return nil, 0, ErrSharingHandlingForbidden
		}
		return nil, 0, ErrReferenceValidationUnavailable
	}
	if scope == nil || scope.TenantID != tenantID || scope.EngineID != first.EngineID || scope.VerifiedAt.IsZero() ||
		scope.Operator != (authorization.SharingFulfillmentOperator{PrincipalID: principal, MembershipID: membership, AuthorizationVersion: version}) {
		return nil, 0, ErrReferenceValidationUnavailable
	}
	// Count and page share a fresh snapshot after remote qualification. The
	// second visibility check prevents changes during that call leaking entries.
	var total int64
	readSnapshot := func() error {
		result = make([]SharingDecisionCandidate, 0)
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
			if err := local.visibleEntriesQuery(ctx, tenantID, currentAccess).Where("entries.id = ?", entryID).Take(&entry).Error; err != nil {
				return sharingBasisReadError(err)
			}
			query := func() *gorm.DB {
				return local.sharingCandidateQuery(ctx, tenantID, currentAccess, entryID, now).Where("sharing_decisions.engine_id = ?", scope.EngineID)
			}
			if err := query().Count(&total).Error; err != nil {
				return err
			}
			var rows []models.SharingDecision
			if err := query().Select("sharing_decisions.id, sharing_decisions.catalog_path, sharing_decisions.engine_id, sharing_decisions.recipient_type, sharing_decisions.recipient_id, sharing_decisions.action, sharing_decisions.expiry_mode, sharing_decisions.expires_at, sharing_decisions.confirmed_by, sharing_decisions.created_at").
				Order("sharing_decisions.created_at DESC, sharing_decisions.id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				view, err := sharingDecisionResult(row)
				if err != nil {
					return err
				}
				result = append(result, SharingDecisionCandidate{ID: row.ID, Target: view.Target, RecipientType: row.RecipientType, RecipientID: row.RecipientID,
					Action: row.Action, ExpiryMode: row.ExpiryMode, ExpiresAt: row.ExpiresAt, ConfirmedBy: row.ConfirmedBy, ConfirmedAt: row.CreatedAt})
			}
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	}
	if err := readSnapshot(); err != nil {
		return nil, 0, err
	}
	if len(result) == 0 {
		return result, total, nil
	}
	if s.system == nil {
		return nil, 0, ErrReferenceValidationUnavailable
	}
	// Only identities already present in this visible page may be resolved.
	// Close the read transaction before HTTP, then discard all labels if the
	// permission, visibility or current decision page changed during resolution.
	refs := make([]commonClient.SystemCatalogReference, 0, len(result))
	for _, row := range result {
		refs = append(refs, commonClient.SystemCatalogReference{SubjectType: row.RecipientType, ID: row.RecipientID})
	}
	labels, err := s.system.ResolveSystemReferences(ctx, tenantID, refs)
	if err != nil || len(labels) != len(refs) {
		return nil, 0, ErrReferenceValidationUnavailable
	}
	for i, label := range labels {
		if label.SubjectType != refs[i].SubjectType || label.ID != refs[i].ID || (label.Found && strings.TrimSpace(label.Name) == "") {
			return nil, 0, ErrReferenceValidationUnavailable
		}
	}
	before, beforeTotal := result, total
	if err := readSnapshot(); err != nil {
		return nil, 0, err
	}
	if total != beforeTotal || len(result) != len(before) {
		return nil, 0, ErrSharingDecisionConflict
	}
	for i := range result {
		if result[i].ID != before[i].ID {
			return nil, 0, ErrSharingDecisionConflict
		}
		result[i].RecipientLabelStatus = "not_found"
		if labels[i].Found {
			result[i].RecipientLabelStatus = "resolved"
			result[i].RecipientName, result[i].RecipientCode = labels[i].Name, labels[i].Code
		}
	}
	return result, total, nil
}
