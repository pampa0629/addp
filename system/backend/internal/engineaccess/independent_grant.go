package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrIndependentGrantConflict          = errors.Join(commonapi.ErrConflict, errors.New("independent grant command conflict"))
	ErrIndependentGrantBasis             = errors.Join(commonapi.ErrConflict, errors.New("independent approval basis changed or missing"))
	ErrIndependentGrantExpiry            = errors.Join(commonapi.ErrConflict, errors.New("grant expiry is no longer future"))
	ErrIndependentGrantSourceUnavailable = errors.New("independent grant source structure unavailable")
)

// The verifier reads structure only, outside IAM locks, and returns the Engine
// aggregate version used for discovery. No sample read, Catalog or Meta RPC.
type IndependentTargetVerifier interface {
	VerifyIndependentTable(context.Context, int64, engineplugin.EngineCatalogPath) (int64, error)
}

func (s *Service) WithIndependentTargetVerifier(verifier IndependentTargetVerifier) *Service {
	s.independentTarget = verifier
	return s
}

// SourceGrantView exposes issuance/revocation history, not an access verdict.
// Do not expose the operator's historical authorization version as a credential.
type SourceGrantView struct {
	RequestID            uuid.UUID        `json:"request_id"`
	GrantedAt            time.Time        `json:"granted_at"`
	ApprovalMode         string           `json:"approval_mode"`
	CatalogRequestID     *uuid.UUID       `json:"catalog_request_id"`
	EngineID             int64            `json:"engine_id,string" swaggertype:"string"`
	CatalogPath          json.RawMessage  `json:"catalog_path" swaggertype:"object"`
	RecipientType        string           `json:"recipient_type"`
	RecipientID          int64            `json:"recipient_id,string" swaggertype:"string"`
	Action               string           `json:"action"`
	ExpiryMode           string           `json:"expiry_mode"`
	ExpiresAt            *time.Time       `json:"expires_at"`
	RequirementVersion   int64            `json:"requirement_version,string" swaggertype:"string"`
	OperatorPrincipalID  int64            `json:"operator_principal_id,string" swaggertype:"string"`
	OperatorMembershipID int64            `json:"operator_membership_id,string" swaggertype:"string"`
	Reason               *string          `json:"reason"`
	Revocation           *GrantRevocation `json:"revocation"`
}

func sourceGrantView(row *sourceGrant) SourceGrantView {
	return SourceGrantView{RequestID: row.RequestID, GrantedAt: row.GrantedAt, ApprovalMode: row.ApprovalMode,
		CatalogRequestID: row.CatalogRequestID, EngineID: row.EngineID, CatalogPath: row.CatalogPath,
		RecipientType: row.RecipientType, RecipientID: row.RecipientID, Action: row.Action,
		ExpiryMode: row.ExpiryMode, ExpiresAt: row.ExpiresAt, RequirementVersion: row.RequirementVersion,
		OperatorPrincipalID: row.OperatorPrincipalID, OperatorMembershipID: row.OperatorMembershipID, Reason: row.Reason}
}

type CreateIndependentGrantInput struct {
	Actor                      Actor
	EngineID                   int64
	RequestID                  uuid.UUID
	CatalogPath                engineplugin.EngineCatalogPath
	RequirementVersion         int64
	RecipientType              string
	RecipientID                int64
	Action, ExpiryMode, Reason string
	ExpiresAt                  *time.Time
	Audit                      iam.AuditMetadata
}

func prepareIndependentGrant(input CreateIndependentGrantInput) (*sourceGrant, error) {
	path, err := shared.EncodeSharingTarget(input.CatalogPath)
	expires, expiryErr := shared.NormalizeSharingExpiry(input.ExpiryMode, input.ExpiresAt)
	segments := input.CatalogPath.Segments
	if err != nil || expiryErr != nil || input.RequestID == uuid.Nil || input.RequirementVersion <= 0 ||
		input.EngineID <= 0 || int64(input.CatalogPath.EngineID) != input.EngineID || len(segments) < 2 ||
		segments[len(segments)-1].Term != "table" || segments[len(segments)-1].Kind != "table" ||
		input.RecipientID <= 0 || !oneOfRecipient(input.RecipientType) || input.Action != "read" || !validReason(input.Reason) {
		return nil, commonapi.ErrBadRequest
	}
	reason := strings.TrimSpace(input.Reason)
	return &sourceGrant{RequestID: input.RequestID, ApprovalMode: approvalModeIndependent,
		TenantID: input.Actor.TenantID, EngineID: input.EngineID, CatalogPath: path,
		RecipientType: input.RecipientType, RecipientID: input.RecipientID, Action: input.Action,
		ExpiryMode: input.ExpiryMode, ExpiresAt: expires, RequirementVersion: input.RequirementVersion,
		OperatorPrincipalID: input.Actor.PrincipalID, OperatorMembershipID: input.Actor.MembershipID,
		OperatorAuthorizationVersion: input.Actor.AuthorizationVersion, Reason: &reason}, nil
}

func sameIndependentGrant(a, b *sourceGrant) bool {
	return a.ApprovalMode == approvalModeIndependent && a.CatalogRequestID == nil &&
		a.RequestID == b.RequestID && a.TenantID == b.TenantID && a.EngineID == b.EngineID && equalJSON(a.CatalogPath, b.CatalogPath) &&
		a.RequirementVersion == b.RequirementVersion && a.RecipientType == b.RecipientType && a.RecipientID == b.RecipientID &&
		a.Action == b.Action && a.OperatorPrincipalID == b.OperatorPrincipalID && a.OperatorMembershipID == b.OperatorMembershipID &&
		a.Reason != nil && b.Reason != nil && *a.Reason == *b.Reason &&
		shared.EqualSharingExpiry(a.ExpiryMode, a.ExpiresAt, b.ExpiryMode, b.ExpiresAt)
}

func (r *Repository) findSourceGrant(ctx context.Context, id uuid.UUID) (*sourceGrant, error) {
	var row sourceGrant
	err := r.db.WithContext(ctx).Where("request_id = ?", id).Take(&row).Error
	return &row, err
}

func (r *Repository) sourceGrantHistory(ctx context.Context, row *sourceGrant) (*SourceGrantView, error) {
	view := sourceGrantView(row)
	revocation, err := r.findGrantRevocation(ctx, row.RequestID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		view.Revocation = revocation
	}
	return &view, nil
}

func (r *Repository) matchIndependentGrant(ctx context.Context, wanted *sourceGrant) (*SourceGrantView, error) {
	prior, err := r.findSourceGrant(ctx, wanted.RequestID)
	if err != nil {
		return nil, err
	}
	if prior.TenantID != wanted.TenantID || prior.EngineID != wanted.EngineID {
		return nil, commonapi.ErrNotFound
	}
	if !sameIndependentGrant(prior, wanted) {
		return nil, ErrIndependentGrantConflict
	}
	return r.sourceGrantHistory(ctx, prior)
}

func (s *Service) CreateIndependentGrant(ctx context.Context, input CreateIndependentGrantInput) (*SourceGrantView, error) {
	row, err := prepareIndependentGrant(input)
	if err != nil {
		return nil, err
	}
	var result *SourceGrantView
	// Authenticate before source IO or history lookup. Historical replay needs
	// current operator qualification, not a running source or current recipient.
	err = s.withEngineManagementScope(ctx, input.Actor, input.EngineID, authorization.PermissionSystemEngineAccessGrantCreate, false,
		func(tx *Repository, check func() error) error {
			prior, err := tx.matchIndependentGrant(ctx, row)
			if qualificationErr := check(); qualificationErr != nil {
				return qualificationErr
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			result = prior
			return nil
		})
	if err != nil || result != nil {
		return result, err
	}
	if s.independentTarget == nil {
		return nil, ErrIndependentGrantSourceUnavailable
	}
	engineVersion, err := s.independentTarget.VerifyIndependentTable(ctx, input.Actor.TenantID, input.CatalogPath)
	if err != nil {
		return nil, err
	}
	if engineVersion <= 0 {
		return nil, ErrIndependentGrantSourceUnavailable
	}
	recipient := &fulfillmentRequest{TenantID: row.TenantID, RecipientType: row.RecipientType, RecipientID: row.RecipientID}
	err = s.withEngineManagementRecipientScope(ctx, input.Actor, input.EngineID, authorization.PermissionSystemEngineAccessGrantCreate, true, recipient,
		func(tx *Repository, check func() error, subject *lockedFulfillmentRecipient) error {
			if err := tx.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "engine_access_request|"+row.RequestID.String()).Error; err != nil {
				return err
			}
			if err := check(); err != nil {
				return err
			}
			prior, err := tx.matchIndependentGrant(ctx, row)
			if err == nil {
				result = prior
				return check()
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.lockFulfillmentTarget(ctx, row.TenantID, row.CatalogPath); err != nil {
				return err
			}
			checkNew := func() error {
				if err := check(); err != nil {
					return err
				}
				engine, err := tx.engine(ctx, row.TenantID, row.EngineID, false)
				if err != nil {
					return err
				}
				if engine.Version != engineVersion {
					return ErrIndependentGrantBasis
				}
				requirement, err := tx.approvalRequirement(ctx, row.TenantID, row.EngineID, row.CatalogPath)
				if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (requirement.Mode != approvalModeIndependent || requirement.Version != row.RequirementVersion)) {
					return ErrIndependentGrantBasis
				}
				if err != nil {
					return err
				}
				now, err := tx.wallClock(ctx)
				if err != nil {
					return err
				}
				if err := subject.check(now); err != nil {
					return err
				}
				if !shared.SharingExpiryFuture(row.ExpiryMode, row.ExpiresAt, now) {
					return ErrIndependentGrantExpiry
				}
				return nil
			}
			if err := checkNew(); err != nil {
				return err
			}
			if err := tx.insertSourceGrant(ctx, row); err != nil {
				return err
			}
			audit := input.Audit
			principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
			audit.PrincipalID, audit.PrincipalType = &input.Actor.PrincipalID, &principalType
			audit.TenantID, audit.ContextType = &input.Actor.TenantID, &contextType
			if err := iam.NewAuditWriter(tx.identity()).Write(ctx, iam.AuditEvent{Metadata: audit,
				EventName: "system.engine_access_grant.issued", Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskHigh,
				ModuleName: "system", EntityType: "engine_access_grant", EntityID: row.RequestID.String(),
				Details: map[string]any{"request_id": row.RequestID, "engine_id": row.EngineID, "approval_mode": row.ApprovalMode,
					"catalog_path": input.CatalogPath, "recipient_type": row.RecipientType, "recipient_id": row.RecipientID,
					"action": row.Action, "expiry_mode": row.ExpiryMode, "expires_at": row.ExpiresAt, "reason": *row.Reason},
			}); err != nil {
				return err
			}
			if err := checkNew(); err != nil {
				return err
			}
			result, err = tx.sourceGrantHistory(ctx, row)
			return err
		})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) listSourceGrants(ctx context.Context, tenantID, engineID int64, page, size int) ([]SourceGrantView, int64, error) {
	query := r.db.WithContext(ctx).Model(&sourceGrant{}).Where("tenant_id = ? AND engine_id = ?", tenantID, engineID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []sourceGrant
	if err := query.Order("granted_at DESC, request_id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	views := make([]SourceGrantView, 0, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		views = append(views, sourceGrantView(&rows[i]))
		ids = append(ids, rows[i].RequestID)
	}
	if len(ids) > 0 {
		var revocations []GrantRevocation
		if err := r.db.WithContext(ctx).Where("request_id IN ?", ids).Find(&revocations).Error; err != nil {
			return nil, 0, err
		}
		byID := make(map[uuid.UUID]*GrantRevocation, len(revocations))
		for i := range revocations {
			byID[revocations[i].RequestID] = &revocations[i]
		}
		for i := range views {
			views[i].Revocation = byID[views[i].RequestID]
		}
	}
	return views, total, nil
}

func (s *Service) ListSourceGrants(ctx context.Context, actor Actor, engineID int64, page, size int) ([]SourceGrantView, int64, error) {
	if page <= 0 || size <= 0 || size > 100 || page > int(^uint(0)>>1)/size {
		return nil, 0, commonapi.ErrBadRequest
	}
	var rows []SourceGrantView
	var total int64
	err := s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessGrantRead, false,
		func(tx *Repository, check func() error) error {
			var err error
			rows, total, err = tx.listSourceGrants(ctx, actor.TenantID, engineID, page, size)
			if err != nil {
				return err
			}
			return check()
		})
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
