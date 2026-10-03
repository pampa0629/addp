package engineaccess

import (
	"context"
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrGrantRevocationConflict = errors.Join(commonapi.ErrConflict, errors.New("grant already revoked with different parameters"))

// GrantRevocation is withdrawal history, not a Deny or a current access verdict.
// Target, recipient and expiry remain in the original immutable fulfillment.
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
			var outcome fulfillmentOutcome
			// Missing, not yet issued and other-tenant/engine requests all stay hidden.
			err := tx.db.WithContext(ctx).Table("system.engine_access_fulfillment_outcomes AS o").
				Joins("JOIN system.engine_access_grants AS g ON g.request_id = o.request_id").
				Where("o.request_id = ? AND o.tenant_id = ? AND o.engine_id = ?", input.RequestID, input.Actor.TenantID, input.EngineID).
				Select("o.*").Take(&outcome).Error
			if qualificationErr := check(); qualificationErr != nil {
				return qualificationErr
			}
			if err != nil {
				return mapError(err)
			}
			if err := tx.lockFulfillmentTarget(ctx, input.Actor.TenantID, outcome.CatalogPath); err != nil {
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
			row := &GrantRevocation{RequestID: input.RequestID, RevokedByPrincipalID: input.Actor.PrincipalID,
				RevokedByMembershipID: input.Actor.MembershipID, Reason: input.Reason}
			if err := tx.db.WithContext(ctx).Clauses(clause.Returning{}).Create(row).Error; err != nil {
				return err
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
			result = row
			return nil
		})
	if err != nil {
		return nil, err
	}
	return result, nil
}
