package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	catalogauthorization "github.com/addp/catalog/internal/authorization"
	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SharingDecisionInput struct {
	DecisionID    uuid.UUID
	Version       int64
	RecipientType string
	RecipientID   int64
	ExpiryMode    string
	ExpiresAt     *time.Time
	Reason        string
}

// The API projects EngineID as a string without changing the provider contract.
type SharingDecisionTarget struct {
	Version  string                        `json:"version"`
	EngineID string                        `json:"engine_id"`
	Segments []plugin.EngineCatalogSegment `json:"segments"`
}

type SharingDecisionResult struct {
	models.SharingDecision
	Target SharingDecisionTarget `json:"target"`
}

func sharingDecisionResult(row models.SharingDecision) (*SharingDecisionResult, error) {
	var path plugin.EngineCatalogPath
	if err := json.Unmarshal(row.CatalogPath, &path); err != nil {
		return nil, err
	}
	return &SharingDecisionResult{SharingDecision: row, Target: SharingDecisionTarget{
		Version: path.Version, EngineID: strconv.FormatInt(row.EngineID, 10), Segments: path.Segments,
	}}, nil
}

func (s *EntryService) WithSharingTargetResolver(resolver SharingTargetResolver) *EntryService {
	s.sharingTargets = resolver
	return s
}

// No browser-supplied identity fields are accepted by this command. Its
// AuthContext must come from the existing System-authenticated middleware.
func sharingConfirmer(auth authorization.AuthContext, tenantID int64) (principal, membership, version int64, err error) {
	return sharingUserProvenance(auth, tenantID, catalogauthorization.PermissionCatalogEntryRead, catalogauthorization.PermissionCatalogSharingDecisionCreate)
}

func sharingUserProvenance(auth authorization.AuthContext, tenantID int64, permissions ...string) (principal, membership, version int64, err error) {
	if auth.Principal.Type != "user" || auth.Context.Type != "tenant" || auth.Context.TenantID == nil ||
		*auth.Context.TenantID != strconv.FormatInt(tenantID, 10) || auth.Context.TenantMembershipID == nil || auth.Delegation != nil ||
		(auth.Token.Type != "first_party_access_token" && auth.Token.Type != "oauth_access_token") ||
		!sharingUserPermissionsAt(auth, time.Now().UTC(), permissions...) {
		return 0, 0, 0, ErrSharingConfirmationForbidden
	}
	values := []string{auth.Principal.ID, *auth.Context.TenantMembershipID, auth.Authorization.AuthorizationVersion}
	parsed := make([]int64, len(values))
	for i, value := range values {
		id, e := strconv.ParseInt(value, 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
			return 0, 0, 0, ErrSharingConfirmationForbidden
		}
		parsed[i] = id
	}
	return parsed[0], parsed[1], parsed[2], nil
}

// AuthContext is verified by System. A role can nevertheless expire while this
// command waits for the aggregate lock; never extend its validity locally.
func sharingPermissionsAt(auth authorization.AuthContext, now time.Time) bool {
	return sharingUserPermissionsAt(auth, now, catalogauthorization.PermissionCatalogEntryRead, catalogauthorization.PermissionCatalogSharingDecisionCreate)
}

func sharingUserPermissionsAt(auth authorization.AuthContext, now time.Time, permissions ...string) bool {
	current := auth
	current.Authorization.RoleAssignments = nil
	for _, assignment := range auth.Authorization.RoleAssignments {
		if assignment.ValidFrom.After(now) || (assignment.ValidUntil != nil && !assignment.ValidUntil.After(now)) {
			continue
		}
		current.Authorization.RoleAssignments = append(current.Authorization.RoleAssignments, assignment)
	}
	return auth.Token.ExpiresAt.After(now) && len(permissions) > 0 && authorization.HasContextPermissions(current, permissions...)
}

func (s *EntryService) GetSharingDecision(ctx context.Context, tenantID int64, access EntryAccess, entryID, decisionID uuid.UUID, auth authorization.AuthContext) (*SharingDecisionResult, error) {
	principal, _, _, err := sharingConfirmer(auth, tenantID)
	if err != nil {
		return nil, err
	}
	if s == nil || s.db == nil || entryID == uuid.Nil || decisionID == uuid.Nil {
		return nil, ErrInvalidEntryUpdate
	}
	var row models.SharingDecision
	err = s.readSharingHistorySnapshot(ctx, tenantID, access, entryID, auth, func(local *EntryService, owner bool) error {
		return sharingBasisReadError(local.sharingHistoryQuery(ctx, tenantID, entryID, principal, owner).Where("id = ?", decisionID).Take(&row).Error)
	})
	if err != nil {
		return nil, err
	}
	return sharingDecisionResult(row)
}

// History review is distinct from a new confirmation or fulfillment command.
// Count and page share a read-only snapshot; ownership is re-read, not inferred
// from an old decision, a browser flag, or the curation permission.
func (s *EntryService) readSharingHistorySnapshot(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID,
	auth authorization.AuthContext, read func(*EntryService, bool) error,
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
		if !sharingPermissionsAt(auth, now) {
			return ErrSharingConfirmationForbidden
		}
		current := access
		current.Inventory = access.Inventory && sharingUserPermissionsAt(auth, now, catalogauthorization.PermissionCatalogInventoryRead)
		var entry models.Entry
		if err := local.visibleEntriesQuery(ctx, tenantID, current).Where("entries.id = ?", entryID).Take(&entry).Error; err != nil {
			return sharingBasisReadError(err)
		}
		var owners int64
		if err := tx.Model(&models.Responsibility{}).Where("tenant_id = ? AND catalog_entry_id = ? AND role = ? AND subject_type = ? AND subject_id = ? AND status = ?",
			tenantID, entryID, "business_owner", "user", auth.Principal.ID, "active").Count(&owners).Error; err != nil {
			return err
		}
		return read(&local, owners > 0)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}

func (s *EntryService) sharingHistoryQuery(ctx context.Context, tenantID int64, entryID uuid.UUID, principal int64, owner bool) *gorm.DB {
	query := s.db.WithContext(ctx).Model(&models.SharingDecision{}).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID)
	if !owner {
		query = query.Where("confirmed_by = ?", principal)
	}
	return query
}

func (s *EntryService) ListSharingDecisions(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID,
	auth authorization.AuthContext, page, size int,
) ([]SharingDecisionResult, int64, error) {
	principal, _, _, err := sharingConfirmer(auth, tenantID)
	if err != nil {
		return nil, 0, err
	}
	if s == nil || s.db == nil || entryID == uuid.Nil || page <= 0 || size <= 0 || size > 100 || page-1 > math.MaxInt/size {
		return nil, 0, ErrInvalidEntryUpdate
	}
	rows := make([]SharingDecisionResult, 0)
	var total int64
	err = s.readSharingHistorySnapshot(ctx, tenantID, access, entryID, auth, func(local *EntryService, owner bool) error {
		query := func() *gorm.DB { return local.sharingHistoryQuery(ctx, tenantID, entryID, principal, owner) }
		if err := query().Count(&total).Error; err != nil {
			return err
		}
		var decisions []models.SharingDecision
		if err := query().Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&decisions).Error; err != nil {
			return err
		}
		for _, decision := range decisions {
			result, err := sharingDecisionResult(decision)
			if err != nil {
				return err
			}
			rows = append(rows, *result)
		}
		return nil
	})
	return rows, total, err
}

func (s *EntryService) CreateSharingDecision(ctx context.Context, tenantID int64, access EntryAccess, entryID uuid.UUID, input SharingDecisionInput, auth authorization.AuthContext) (*SharingDecisionResult, bool, error) {
	principal, membership, authVersion, err := sharingConfirmer(auth, tenantID)
	if err != nil {
		return nil, false, err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	// PostgreSQL timestamps have microsecond precision. Canonicalize before both
	// insertion and retry comparison, truncating rather than extending expiry.
	input.ExpiresAt, err = authorization.NormalizeSharingExpiry(input.ExpiryMode, input.ExpiresAt)
	if s == nil || s.db == nil || tenantID <= 0 || entryID == uuid.Nil || input.DecisionID == uuid.Nil || input.Version <= 0 || input.Version == math.MaxInt64 ||
		!oneOf(input.RecipientType, "user", "project_group") || input.RecipientID <= 0 || err != nil ||
		input.Reason == "" || !utf8.ValidString(input.Reason) || strings.ContainsRune(input.Reason, '\x00') || utf8.RuneCountInString(input.Reason) > 2000 {
		return nil, false, ErrInvalidEntryUpdate
	}
	// Check entry visibility before reading any remote resource metadata.
	var entry models.Entry
	if err := s.visibleEntriesQuery(ctx, tenantID, access).Where("entries.id = ?", entryID).Take(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, ErrEntryNotFound
		}
		return nil, false, err
	}
	var existing models.SharingDecision
	err = s.db.WithContext(ctx).Where("id = ?", input.DecisionID).Take(&existing).Error
	if err == nil {
		if !sameSharingInput(existing, tenantID, entryID, principal, membership, input) {
			return nil, false, ErrSharingDecisionConflict
		}
		result, err := sharingDecisionResult(existing)
		return result, false, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	var source models.SourceBinding
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", tenantID, entryID, true).Take(&source).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, ErrSharingTargetUnsupported
		}
		return nil, false, err
	}
	if entry.EntryStatus != models.EntryStatusActive || entry.GovernanceStatus == models.GovernanceStatusDeprecated || entry.EntryType != models.EntryTypeDataItem ||
		source.SourceModule != models.SourceModuleMeta || source.SourceType != models.SourceTypeDataItem || source.SourceStatus != models.SourceStatusActive {
		return nil, false, ErrSharingTargetUnsupported
	}
	itemID, ok := numericInt64(source.ObservedSnapshot["item_id"])
	if !ok || itemID <= 0 {
		return nil, false, ErrSharingTargetUnsupported
	}
	if s.sharingTargets == nil || s.system == nil {
		return nil, false, ErrReferenceValidationUnavailable
	}
	path, err := s.sharingTargets.ResolveSharingTarget(ctx, tenantID, itemID, source.SourceIdentity)
	if err != nil {
		return nil, false, err
	}
	if path.Version != plugin.EngineCatalogPathVersion || path.EngineID == 0 || int64(path.EngineID) <= 0 || len(path.Segments) < 3 || len(path.Segments) > 129 ||
		path.Segments[len(path.Segments)-1].Term != "table" {
		return nil, false, ErrSharingTargetUnsupported
	}
	pathJSON, err := json.Marshal(path)
	if err != nil {
		return nil, false, err
	}
	refs := []commonClient.SystemCatalogReference{{SubjectType: "user", ID: principal}}
	if input.RecipientType != "user" || input.RecipientID != principal {
		refs = append(refs, commonClient.SystemCatalogReference{SubjectType: input.RecipientType, ID: input.RecipientID})
	}
	resolved, err := s.system.ResolveSystemReferences(ctx, tenantID, refs)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrReferenceValidationUnavailable, err)
	}
	if len(resolved) != len(refs) {
		return nil, false, ErrReferenceValidationUnavailable
	}
	for i, ref := range refs {
		if resolved[i].SubjectType != ref.SubjectType || resolved[i].ID != ref.ID || !resolved[i].Found || !resolved[i].Referenceable {
			return nil, false, ErrReferenceNotReferenceable
		}
	}
	inGroup := false
	if input.RecipientType == "project_group" {
		for _, group := range auth.Organization.ProjectGroups {
			inGroup = inGroup || group.ProjectGroupID == strconv.FormatInt(input.RecipientID, 10)
		}
	}
	created := false
	var decision models.SharingDecision
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, entryID).Take(&entry).Error; err != nil {
			return err
		}
		// A concurrent retry waits at the same aggregate before inspecting history.
		err := tx.Where("id = ?", input.DecisionID).Take(&decision).Error
		if err == nil {
			if !sameSharingInput(decision, tenantID, entryID, principal, membership, input) {
				return ErrSharingDecisionConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if entry.Version != input.Version {
			return ErrEntryVersionConflict
		}
		if entry.EntryStatus != models.EntryStatusActive || entry.GovernanceStatus == models.GovernanceStatusDeprecated || entry.EntryType != models.EntryTypeDataItem {
			return ErrSharingTargetUnsupported
		}
		var current models.SourceBinding
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", tenantID, entryID, true).Take(&current).Error; err != nil {
			return err
		}
		if current.ID != source.ID || current.SourceVersion != source.SourceVersion || current.SourceIdentity != source.SourceIdentity || current.SourceStatus != models.SourceStatusActive || current.SourceModule != models.SourceModuleMeta || current.SourceType != models.SourceTypeDataItem {
			return ErrEntryVersionConflict
		}
		var owner models.Responsibility
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND catalog_entry_id = ? AND role = ? AND subject_type = ? AND subject_id = ? AND status = ?", tenantID, entryID,
			models.ResponsibilityRoleBusinessOwner, "user", principal, models.ResponsibilityStatusActive).Take(&owner).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSharingConfirmationForbidden
			}
			return err
		}
		now := time.Now().UTC()
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				return err
			}
		}
		if !authorization.SharingExpiryFuture(input.ExpiryMode, input.ExpiresAt, now) {
			return ErrInvalidEntryUpdate
		}
		if !sharingPermissionsAt(auth, now) {
			return ErrSharingConfirmationForbidden
		}
		decision = models.SharingDecision{ID: input.DecisionID, TenantID: tenantID, CatalogEntryID: entryID, EntryVersion: entry.Version + 1,
			SourceBindingID: current.ID, SourceVersion: current.SourceVersion, ResponsibilityID: owner.ID, EngineID: int64(path.EngineID), CatalogPath: pathJSON,
			ConfirmedBy: principal, ConfirmerMembershipID: membership, AuthorizationVersion: authVersion,
			RecipientType: input.RecipientType, RecipientID: input.RecipientID, Action: "read", SelfBeneficiary: input.RecipientType == "user" && input.RecipientID == principal,
			ConfirmerInProjectGroup: inGroup, Reason: input.Reason, ExpiryMode: input.ExpiryMode, ExpiresAt: input.ExpiresAt, CreatedAt: now}
		if err := tx.Create(&decision).Error; err != nil {
			return err
		}
		if err := tx.Model(&entry).Updates(map[string]any{"version": decision.EntryVersion, "updated_at": now}).Error; err != nil {
			return err
		}
		// Validate the durable record, not just its in-memory construction. This
		// is the same local owner predicate future fulfillment preparation uses;
		// creation still does not freeze responsibility or accept authorization.
		if _, err := lockCurrentSharingDecisionBasis(ctx, tx, tenantID, entryID, decision.ID); err != nil {
			return err
		}
		audit := models.AuditEvent{ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entryID, EventType: "catalog.sharing_decision.created", ActorType: "user", ActorID: auth.Principal.ID,
			Details: commonModels.JSONMap{"decision_id": decision.ID.String(), "entry_version": strconv.FormatInt(decision.EntryVersion, 10)}, CreatedAt: now}
		if err := tx.Create(&audit).Error; err != nil {
			return err
		}
		if err := enqueueProjection(tx, tenantID, entryID); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "23505" && pgError.ConstraintName == "sharing_decisions_pkey" {
			return nil, false, ErrSharingDecisionConflict
		}
		return nil, false, err
	}
	result, err := sharingDecisionResult(decision)
	return result, created, err
}

func sameSharingInput(row models.SharingDecision, tenantID int64, entryID uuid.UUID, principal, membership int64, input SharingDecisionInput) bool {
	return row.TenantID == tenantID && row.CatalogEntryID == entryID && row.ConfirmedBy == principal && row.ConfirmerMembershipID == membership &&
		row.EntryVersion == input.Version+1 && row.RecipientType == input.RecipientType && row.RecipientID == input.RecipientID && row.Action == "read" &&
		authorization.EqualSharingExpiry(row.ExpiryMode, row.ExpiresAt, input.ExpiryMode, input.ExpiresAt) && row.Reason == input.Reason
}
