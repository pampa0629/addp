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

var ErrDenyReleaseConflict = errors.Join(commonapi.ErrConflict, errors.New("deny already released with different parameters"))
var ErrDenyReleaseExpired = errors.Join(commonapi.ErrConflict, errors.New("deny has expired; release is unnecessary"))

// DenyRelease removes only its original Deny basis, never grants access.
// The immutable parent retains target, recipient, action and expiry.
type DenyRelease struct {
	DenyID                 uuid.UUID `gorm:"type:uuid;primaryKey" json:"deny_id"`
	ReleasedByPrincipalID  int64     `json:"released_by_principal_id,string" swaggertype:"string"`
	ReleasedByMembershipID int64     `json:"released_by_membership_id,string" swaggertype:"string"`
	ReleasedAt             time.Time `json:"released_at"`
	Reason                 string    `json:"reason"`
}

func (DenyRelease) TableName() string { return "system.engine_access_deny_releases" }

type ReleaseDenyInput struct {
	Actor    Actor
	EngineID int64
	DenyID   uuid.UUID
	Reason   string
	Audit    iam.AuditMetadata
}

func checkDenyReleaseExpiry(deny SourceDeny, now time.Time) error {
	// Validate immutable data separately from natural expiry; malformed history
	// must never be reported as a legitimate expiration.
	if _, err := shared.NormalizeSharingExpiry(deny.ExpiryMode, deny.ExpiresAt); err != nil {
		return errors.New("invalid immutable Deny expiry")
	}
	if !shared.SharingExpiryFuture(deny.ExpiryMode, deny.ExpiresAt, now) {
		return ErrDenyReleaseExpired
	}
	return nil
}

func sameDenyRelease(row *DenyRelease, input ReleaseDenyInput) bool {
	return row.DenyID == input.DenyID && row.ReleasedByPrincipalID == input.Actor.PrincipalID &&
		row.ReleasedByMembershipID == input.Actor.MembershipID && row.Reason == strings.TrimSpace(input.Reason)
}

func (r *Repository) findDenyRelease(ctx context.Context, id uuid.UUID) (*DenyRelease, error) {
	var row DenyRelease
	err := r.db.WithContext(ctx).Where("deny_id = ?", id).Take(&row).Error
	return &row, err
}

// Release requires current human IAM and engine management qualification on
// every call, before exposing history. Recipient eligibility and Catalog/source
// availability are deliberately not prerequisites for removing a Deny.
func (s *Service) ReleaseDeny(ctx context.Context, input ReleaseDenyInput) (*DenyRelease, error) {
	if input.DenyID == uuid.Nil || !validReason(input.Reason) {
		return nil, commonapi.ErrBadRequest
	}
	input.Reason = strings.TrimSpace(input.Reason)
	var result *DenyRelease
	err := s.withEngineManagementScope(ctx, input.Actor, input.EngineID, authorization.PermissionSystemEngineAccessDenyRelease, false,
		func(tx *Repository, check func() error) error {
			var deny SourceDeny
			err := tx.db.WithContext(ctx).Where("deny_id = ? AND tenant_id = ? AND engine_id = ?",
				input.DenyID, input.Actor.TenantID, input.EngineID).Take(&deny).Error
			if qualificationErr := check(); qualificationErr != nil {
				return qualificationErr
			}
			if err != nil {
				return mapError(err)
			}
			if err := tx.lockFulfillmentTarget(ctx, input.Actor.TenantID, deny.CatalogPath); err != nil {
				return err
			}
			if err := check(); err != nil {
				return err
			}
			prior, err := tx.findDenyRelease(ctx, input.DenyID)
			if err == nil {
				if !sameDenyRelease(prior, input) {
					return ErrDenyReleaseConflict
				}
				result = prior
				return check()
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			checkNew := func() error {
				if err := check(); err != nil {
					return err
				}
				now, err := tx.wallClock(ctx)
				if err != nil {
					return err
				}
				return checkDenyReleaseExpiry(deny, now)
			}
			if err := checkNew(); err != nil {
				return err
			}
			row := &DenyRelease{DenyID: input.DenyID, ReleasedByPrincipalID: input.Actor.PrincipalID,
				ReleasedByMembershipID: input.Actor.MembershipID, Reason: input.Reason}
			if err := tx.db.WithContext(ctx).Clauses(clause.Returning{}).Create(row).Error; err != nil {
				return mapError(err)
			}
			audit := input.Audit
			principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
			audit.PrincipalID, audit.PrincipalType = &input.Actor.PrincipalID, &principalType
			audit.TenantID, audit.ContextType = &input.Actor.TenantID, &contextType
			if err := iam.NewAuditWriter(tx.identity()).Write(ctx, iam.AuditEvent{
				Metadata: audit, EventName: "system.engine_access_deny.released", Result: iam.AuditResultSucceeded,
				RiskLevel: iam.AuditRiskHigh, ModuleName: "system", EntityType: "engine_access_deny", EntityID: row.DenyID.String(),
				Details: map[string]any{"deny_id": row.DenyID, "engine_id": input.EngineID,
					"released_by_membership_id": input.Actor.MembershipID, "reason": row.Reason},
			}); err != nil {
				return err
			}
			if err := checkNew(); err != nil {
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
