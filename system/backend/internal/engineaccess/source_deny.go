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
	"gorm.io/gorm/clause"
)

var ErrDenyConflict = errors.Join(commonapi.ErrConflict, errors.New("deny command parameters differ from history"))
var ErrDenyExpiry = errors.Join(commonapi.ErrConflict, errors.New("deny expiry is no longer future"))

// SourceDeny is immutable rule establishment, not a current access verdict.
// The subject uses the same stable ID contract as ordinary source sharing.
type SourceDeny struct {
	DenyID                    uuid.UUID       `gorm:"type:uuid;primaryKey" json:"deny_id"`
	TenantID                  int64           `json:"-"`
	EngineID                  int64           `json:"engine_id,string" swaggertype:"string"`
	CatalogPath               json.RawMessage `gorm:"type:jsonb" json:"catalog_path" swaggertype:"object"`
	RecipientType             string          `json:"recipient_type"`
	RecipientID               int64           `json:"recipient_id,string" swaggertype:"string"`
	Action                    string          `json:"action"`
	ExpiryMode                string          `json:"expiry_mode"`
	ExpiresAt                 *time.Time      `json:"expires_at"`
	EstablishedByPrincipalID  int64           `json:"established_by_principal_id,string" swaggertype:"string"`
	EstablishedByMembershipID int64           `json:"established_by_membership_id,string" swaggertype:"string"`
	EstablishedAt             time.Time       `json:"established_at"`
	Reason                    string          `json:"reason"`
}

func (SourceDeny) TableName() string { return "system.engine_access_denies" }

type CreateDenyInput struct {
	Actor                      Actor
	EngineID                   int64
	DenyID                     uuid.UUID
	CatalogPath                engineplugin.EngineCatalogPath
	RecipientType              string
	RecipientID                int64
	Action, ExpiryMode, Reason string
	ExpiresAt                  *time.Time
	Audit                      iam.AuditMetadata
}

func prepareSourceDeny(input CreateDenyInput) (*SourceDeny, error) {
	path, err := shared.EncodeSharingTarget(input.CatalogPath)
	expires, expiryErr := shared.NormalizeSharingExpiry(input.ExpiryMode, input.ExpiresAt)
	if err != nil || expiryErr != nil || input.EngineID <= 0 || int64(input.CatalogPath.EngineID) != input.EngineID ||
		input.DenyID == uuid.Nil || input.RecipientID <= 0 || !oneOfRecipient(input.RecipientType) ||
		input.Action != "read" || !validReason(input.Reason) {
		return nil, commonapi.ErrBadRequest
	}
	return &SourceDeny{DenyID: input.DenyID, TenantID: input.Actor.TenantID, EngineID: input.EngineID,
		CatalogPath: path, RecipientType: input.RecipientType, RecipientID: input.RecipientID, Action: input.Action,
		ExpiryMode: input.ExpiryMode, ExpiresAt: expires, Reason: strings.TrimSpace(input.Reason),
		EstablishedByPrincipalID: input.Actor.PrincipalID, EstablishedByMembershipID: input.Actor.MembershipID}, nil
}

func sameSourceDeny(a, b *SourceDeny) bool {
	return a.DenyID == b.DenyID && a.TenantID == b.TenantID && a.EngineID == b.EngineID && equalJSON(a.CatalogPath, b.CatalogPath) &&
		a.RecipientType == b.RecipientType && a.RecipientID == b.RecipientID && a.Action == b.Action && a.Reason == b.Reason &&
		a.EstablishedByPrincipalID == b.EstablishedByPrincipalID && a.EstablishedByMembershipID == b.EstablishedByMembershipID &&
		shared.EqualSharingExpiry(a.ExpiryMode, a.ExpiresAt, b.ExpiryMode, b.ExpiresAt)
}

func (r *Repository) findSourceDeny(ctx context.Context, id uuid.UUID) (*SourceDeny, error) {
	var row SourceDeny
	err := r.db.WithContext(ctx).Where("deny_id = ?", id).Take(&row).Error
	return &row, err
}

func (r *Repository) insertSourceDeny(ctx context.Context, row *SourceDeny) error {
	return mapError(r.db.WithContext(ctx).Clauses(clause.Returning{}).Create(row).Error)
}

// History requires current operator qualification, but not current recipient
// eligibility or a refreshed expiry. New rules recheck both through commit.
func (s *Service) CreateDeny(ctx context.Context, input CreateDenyInput) (*SourceDeny, error) {
	row, err := prepareSourceDeny(input)
	if err != nil {
		return nil, err
	}
	recipient := &fulfillmentRequest{TenantID: input.Actor.TenantID, RecipientType: input.RecipientType, RecipientID: input.RecipientID}
	var result *SourceDeny
	err = s.withEngineManagementRecipientScope(ctx, input.Actor, input.EngineID, authorization.PermissionSystemEngineAccessDenyCreate,
		false, recipient, func(tx *Repository, check func() error, subject *lockedFulfillmentRecipient) error {
			// Command ID before target, consistent with existing fulfillment writes.
			if err := tx.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "engine_access_deny|"+input.DenyID.String()).Error; err != nil {
				return err
			}
			if err := check(); err != nil {
				return err
			}
			prior, err := tx.findSourceDeny(ctx, input.DenyID)
			if err == nil {
				if prior.TenantID != row.TenantID || prior.EngineID != row.EngineID {
					return commonapi.ErrNotFound
				}
				if !sameSourceDeny(prior, row) {
					return ErrDenyConflict
				}
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
				now, err := tx.wallClock(ctx)
				if err != nil {
					return err
				}
				if err := subject.check(now); err != nil {
					return err
				}
				if !shared.SharingExpiryFuture(row.ExpiryMode, row.ExpiresAt, now) {
					return ErrDenyExpiry
				}
				return nil
			}
			if err := checkNew(); err != nil {
				return err
			}
			if err := tx.insertSourceDeny(ctx, row); err != nil {
				return err
			}
			audit := input.Audit
			principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
			audit.PrincipalID, audit.PrincipalType = &input.Actor.PrincipalID, &principalType
			audit.TenantID, audit.ContextType = &input.Actor.TenantID, &contextType
			if err := iam.NewAuditWriter(tx.identity()).Write(ctx, iam.AuditEvent{
				Metadata: audit, EventName: "system.engine_access_deny.created", Result: iam.AuditResultSucceeded,
				RiskLevel: iam.AuditRiskHigh, ModuleName: "system", EntityType: "engine_access_deny", EntityID: row.DenyID.String(),
				Details: map[string]any{"deny_id": row.DenyID, "engine_id": row.EngineID, "recipient_type": row.RecipientType,
					"recipient_id": row.RecipientID, "action": row.Action, "expiry_mode": row.ExpiryMode, "expires_at": row.ExpiresAt, "reason": row.Reason},
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
