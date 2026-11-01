package engineaccess

import (
	"context"
	"database/sql"
	"errors"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Delegation is administrative qualification, never a content access rule.
type Delegation struct {
	ID, TenantID, EngineID, TenantMembershipID int64
	Status                                     string
	Version                                    int64
	GrantedByPrincipalID                       int64
	GrantedAt, ExpiresAt                       time.Time
	GrantReason                                string
	RevokedByPrincipalID                       *int64
	RevokedAt                                  *time.Time
	RevokedReason                              *string
}

func (Delegation) TableName() string { return "system.engine_access_delegations" }

type View struct {
	Delegation
	EffectiveState string
	EngineName     string
	PrincipalID    int64
	DisplayName    string
}

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }
func (r *Repository) transaction(ctx context.Context, f func(*Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return f(NewRepository(tx)) })
}
func (r *Repository) identity() *iam.Repository { return iam.NewRepository(r.db) }

// Observations own a read-only transaction. A caller's write transaction must
// never expose its own uncommitted authorization facts through this boundary.
func (r *Repository) readCommitted(ctx context.Context, read func(*Repository) error) error {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); ok {
		return errFulfillmentBinding
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return read(NewRepository(tx))
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}

func (r *Repository) wallClock(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := r.db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now.UTC(), err
}

func (r *Repository) engine(ctx context.Context, tenantID, engineID int64, lock bool) (*models.Engine, error) {
	query := r.db.WithContext(ctx).Table("system.engines").Where("tenant_id = ? AND id = ?", tenantID, engineID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var engine models.Engine
	if err := query.Take(&engine).Error; err != nil {
		return nil, mapError(err)
	}
	return &engine, nil
}
func (r *Repository) membership(ctx context.Context, tenantID, membershipID int64) (*iam.TenantMembership, error) {
	var member iam.TenantMembership
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, membershipID).Take(&member).Error
	return &member, mapError(err)
}
func (r *Repository) get(ctx context.Context, tenantID, engineID, id int64, lock bool) (*Delegation, error) {
	query := r.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ? AND id = ?", tenantID, engineID, id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var delegation Delegation
	if err := query.Take(&delegation).Error; err != nil {
		return nil, mapError(err)
	}
	return &delegation, nil
}

const viewSelect = `d.*, e.name AS engine_name, m.principal_id, u.display_name,
CASE WHEN d.status = 'revoked' THEN 'revoked'
     WHEN d.expires_at <= clock_timestamp() THEN 'expired'
     WHEN t.status <> 'active' OR p.status <> 'active' OR p.principal_type <> 'user'
          OR m.status <> 'active' OR (m.expires_at IS NOT NULL AND m.expires_at <= clock_timestamp())
          OR e.lifecycle_state <> 'active' THEN 'unavailable'
     ELSE 'effective' END AS effective_state`

func (r *Repository) views(ctx context.Context, tenantID, engineID int64) *gorm.DB {
	return r.db.WithContext(ctx).Table("system.engine_access_delegations d").Select(viewSelect).
		Joins("JOIN system.engines e ON e.tenant_id = d.tenant_id AND e.id = d.engine_id").
		Joins("JOIN system.tenant_memberships m ON m.tenant_id = d.tenant_id AND m.id = d.tenant_membership_id").
		Joins("JOIN system.principals p ON p.id = m.principal_id").
		Joins("JOIN system.users u ON u.id = p.id").
		Joins("JOIN system.tenants t ON t.id = d.tenant_id").
		Where("d.tenant_id = ? AND d.engine_id = ?", tenantID, engineID)
}
func (r *Repository) list(ctx context.Context, tenantID, engineID int64, page, size int) ([]View, int64, error) {
	query := r.views(ctx, tenantID, engineID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, mapError(err)
	}
	rows := make([]View, 0)
	if err := query.Order("d.id DESC").Offset((page - 1) * size).Limit(size).Scan(&rows).Error; err != nil {
		return nil, 0, mapError(err)
	}
	return rows, total, nil
}
func (r *Repository) view(ctx context.Context, tenantID, engineID, id int64) (*View, error) {
	var rows []View
	if err := r.views(ctx, tenantID, engineID).Where("d.id = ?", id).Scan(&rows).Error; err != nil {
		return nil, mapError(err)
	}
	if len(rows) != 1 {
		return nil, commonapi.ErrNotFound
	}
	return &rows[0], nil
}
func (r *Repository) insert(ctx context.Context, d *Delegation) error {
	return mapError(r.db.WithContext(ctx).Create(d).Error)
}
func (r *Repository) revoke(ctx context.Context, d *Delegation, version, actorID int64, reason string, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&Delegation{}).
		Where("tenant_id = ? AND engine_id = ? AND id = ? AND version = ? AND status = 'active' AND expires_at > clock_timestamp()", d.TenantID, d.EngineID, d.ID, version).
		Updates(map[string]any{"status": "revoked", "version": gorm.Expr("version + 1"), "revoked_by_principal_id": actorID, "revoked_at": now, "revoked_reason": reason})
	if result.Error != nil {
		return mapError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrVersionConflict
	}
	return nil
}
func mapError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonapi.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "engine_access_deny_release_expiry":
			return ErrDenyReleaseExpired
		case "engine_access_deny_expiry":
			return ErrDenyExpiry
		case "engine_access_grant_revocation_expiry":
			return ErrGrantRevocationExpired
		case "engine_access_delegations_overlap":
			return ErrOverlap
		case "engine_access_delegations_expiry":
			return ErrExpiry
		case "engine_access_delegations_expired_history":
			return ErrExpired
		}
	}
	return err
}
