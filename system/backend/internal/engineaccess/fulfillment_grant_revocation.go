package engineaccess

import (
	"context"
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrGrantRevocationConflict = errors.Join(commonapi.ErrConflict, errors.New("grant already revoked with different parameters"))
var ErrGrantRevocationExpired = errors.Join(commonapi.ErrConflict, errors.New("grant has expired; revocation is unnecessary"))

func checkGrantRevocationExpiry(grant sourceGrant, now time.Time) error {
	switch grant.ExpiryMode {
	case shared.SharingExpiryUntilRevoked:
		if grant.ExpiresAt == nil {
			return nil
		}
	case shared.SharingExpiryAtTime:
		if grant.ExpiresAt != nil {
			if !now.Before(*grant.ExpiresAt) {
				return ErrGrantRevocationExpired
			}
			return nil
		}
	}
	return errors.New("invalid immutable Grant expiry")
}

func (r *Repository) checkGrantRevocationExpiry(ctx context.Context, grant sourceGrant) error {
	now, err := r.wallClock(ctx)
	if err != nil {
		return err
	}
	return checkGrantRevocationExpiry(grant, now)
}

// GrantRevocation is withdrawal history, not a Deny or a current access verdict.
// Target, recipient and expiry remain in the canonical immutable Grant.
type GrantRevocation struct {
	RequestID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"request_id"`
	RevokedByPrincipalID  int64     `json:"revoked_by_principal_id,string" swaggertype:"string"`
	RevokedByMembershipID int64     `json:"revoked_by_membership_id,string" swaggertype:"string"`
	RevokedAt             time.Time `json:"revoked_at"`
	Reason                string    `json:"reason"`
}

func (GrantRevocation) TableName() string { return "system.engine_access_grant_revocations" }

type RevokeGrantInput struct {
	Actor     Actor
	EngineID  int64
	RequestID uuid.UUID
	Reason    string
	Audit     iam.AuditMetadata
}

func (r *Repository) findGrantRevocation(ctx context.Context, id uuid.UUID) (*GrantRevocation, error) {
	var row GrantRevocation
	if err := r.db.WithContext(ctx).Where("request_id = ?", id).Take(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// Unlike new fulfillment, withdrawal requires neither a running engine nor
// remote Catalog/source access. Current human IAM and management scope still
// gate every call, including retries, before looking up any Grant history.
func (s *Service) RevokeGrant(ctx context.Context, input RevokeGrantInput) (*GrantRevocation, error) {
	if input.RequestID == uuid.Nil || !validReason(input.Reason) {
		return nil, commonapi.ErrBadRequest
	}
	input.Reason = strings.TrimSpace(input.Reason)
	var result *GrantRevocation
	err := s.withEngineManagementScope(ctx, input.Actor, input.EngineID, authorization.PermissionSystemEngineAccessGrantRevoke, false,
		func(tx *Repository, check func() error) error {
			var grant sourceGrant
			// Missing, not yet issued and other-tenant/engine requests all stay hidden.
			err := tx.db.WithContext(ctx).Where("request_id = ? AND tenant_id = ? AND engine_id = ?",
				input.RequestID, input.Actor.TenantID, input.EngineID).Take(&grant).Error
			if qualificationErr := check(); qualificationErr != nil {
				return qualificationErr
			}
			if err != nil {
				return mapError(err)
			}
			if err := tx.lockFulfillmentTarget(ctx, input.Actor.TenantID, grant.CatalogPath); err != nil {
				return err
			}
			if err := check(); err != nil {
				return err
			}
			prior, err := tx.findGrantRevocation(ctx, input.RequestID)
			if err == nil {
				if prior.RevokedByPrincipalID != input.Actor.PrincipalID || prior.RevokedByMembershipID != input.Actor.MembershipID || prior.Reason != input.Reason {
					return ErrGrantRevocationConflict
				}
				result = prior
				return check()
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			// Recover committed withdrawal history above before checking the Grant's
			// natural expiry. The original five-minute acceptance window is irrelevant.
			if err := tx.checkGrantRevocationExpiry(ctx, grant); err != nil {
				return err
			}
			row := &GrantRevocation{RequestID: input.RequestID, RevokedByPrincipalID: input.Actor.PrincipalID,
				RevokedByMembershipID: input.Actor.MembershipID, Reason: input.Reason}
			if err := tx.db.WithContext(ctx).Clauses(clause.Returning{}).Create(row).Error; err != nil {
				return mapError(err)
			}
			audit := input.Audit
			actor := input.Actor
			principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
			audit.PrincipalID, audit.PrincipalType = &actor.PrincipalID, &principalType
			audit.TenantID, audit.ContextType = &actor.TenantID, &contextType
			if err := iam.NewAuditWriter(tx.identity()).Write(ctx, iam.AuditEvent{
				Metadata: audit, EventName: "system.engine_access_grant.revoked", Result: iam.AuditResultSucceeded,
				RiskLevel: iam.AuditRiskHigh, ModuleName: "system", EntityType: "engine_access_grant", EntityID: input.RequestID.String(),
				Details: map[string]any{"request_id": input.RequestID, "engine_id": input.EngineID,
					"revoked_by_membership_id": actor.MembershipID, "reason": row.Reason},
			}); err != nil {
				return err
			}
			if err := check(); err != nil {
				return err
			}
			if err := tx.checkGrantRevocationExpiry(ctx, grant); err != nil {
				return err
			}
			result = row
			return nil
		})
	if err != nil {
		return nil, err
	}
	return result, nil
}
