package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Explicit permission for future direct issuance, never a content Grant.
type directPermission struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	TenantID        int64
	EngineID        int64
	CatalogPath     json.RawMessage `gorm:"type:jsonb"`
	ItemFingerprint string
	Allowed         bool
	Version         int64
	UpdatedAt       time.Time
}

func (directPermission) TableName() string { return "system.engine_access_direct_permissions" }

var ErrDirectPermissionVersion = errors.Join(commonapi.ErrConflict, errors.New("direct permission observation changed"))

func (r *Repository) directPermission(ctx context.Context, tenantID, engineID int64, fingerprint string) (*directPermission, error) {
	var row directPermission
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ? AND item_fingerprint = ?", tenantID, engineID, fingerprint).Take(&row).Error
	return &row, err
}

// No process instance, health, heartbeat or URL participates in this fact.
// The direct issuance transaction holds this SHARE lock until commit.
func (r *Repository) catalogDeployment(ctx context.Context, lock bool) (*catalogDeploymentDeclaration, error) {
	var row catalogDeploymentDeclaration
	query := r.db.WithContext(ctx).Table("system.module_definitions").Select("enabled, version").Where("module_name = ?", "catalog")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "SHARE"})
	}
	err := query.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func (r *Repository) effectiveDirectPermission(ctx context.Context, tenantID, engineID int64, fingerprint string, lock bool) (directPermissionDecision, error) {
	row, err := r.directPermission(ctx, tenantID, engineID, fingerprint)
	if err == nil {
		return resolveDirectPermission(&directPermissionSetting{Allowed: row.Allowed, Version: row.Version}, nil)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return directPermissionDecision{}, err
	}
	deployment, err := r.catalogDeployment(ctx, lock)
	if err != nil {
		return directPermissionDecision{}, err
	}
	return resolveDirectPermission(nil, deployment)
}

type directPermissionObservation struct {
	Settings   map[string]directPermissionSetting
	Deployment *catalogDeploymentDeclaration
}

// Both subqueries belong to one PostgreSQL statement snapshot. The observation
// is compared again after the owner RPC; no IAM/engine lock crosses that RPC.
func (r *Repository) directPermissionObservation(ctx context.Context, tenantID, engineID int64) (*directPermissionObservation, error) {
	var raw []byte
	err := r.db.WithContext(ctx).Raw(`SELECT jsonb_build_object(
 'Settings',COALESCE((SELECT jsonb_object_agg(item_fingerprint,jsonb_build_object('Allowed',allowed,'Version',version))
 FROM system.engine_access_direct_permissions WHERE tenant_id=? AND engine_id=?),'{}'::jsonb),
 'Deployment',(SELECT jsonb_build_object('Enabled',enabled,'Version',version) FROM system.module_definitions WHERE module_name='catalog'))`, tenantID, engineID).Row().Scan(&raw)
	if err != nil {
		return nil, err
	}
	var observation directPermissionObservation
	if err := json.Unmarshal(raw, &observation); err != nil {
		return nil, err
	}
	return &observation, nil
}

func (r *Repository) setDirectPermission(ctx context.Context, input SetDirectPermissionInput, verify func() error) (*directPermission, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok || verify == nil {
		return nil, commonapi.ErrBadRequest
	}
	path, err := encodeFulfillmentPath(input.CatalogPath)
	if err != nil {
		return nil, err
	}
	if err = r.lockFulfillmentTarget(ctx, input.Actor.TenantID, path); err != nil {
		return nil, err
	}
	decision, err := r.effectiveDirectPermission(ctx, input.Actor.TenantID, input.EngineID, input.ItemFingerprint, true)
	if err != nil {
		return nil, err
	}
	if decision.Source != input.ExpectedSource || decision.Version != input.ExpectedVersion {
		return nil, ErrDirectPermissionVersion
	}
	row, err := r.directPermission(ctx, input.Actor.TenantID, input.EngineID, input.ItemFingerprint)
	creating := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !creating {
		return nil, err
	}
	now, err := r.wallClock(ctx)
	if err != nil {
		return nil, err
	}
	if err = verify(); err != nil {
		return nil, err
	}
	if !creating && row.Allowed == input.Allowed {
		return row, nil
	}
	if creating {
		row = &directPermission{ID: uuid.New(), TenantID: input.Actor.TenantID, EngineID: input.EngineID, CatalogPath: path,
			ItemFingerprint: input.ItemFingerprint, Allowed: input.Allowed, Version: 1, UpdatedAt: now}
		if err = r.db.WithContext(ctx).Create(row).Error; err != nil {
			return nil, err
		}
	} else {
		result := r.db.WithContext(ctx).Model(&directPermission{}).Where("id=? AND version=?", row.ID, row.Version).
			Updates(map[string]any{"allowed": input.Allowed, "version": gorm.Expr("version + 1"), "updated_at": now})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, ErrDirectPermissionVersion
		}
		row.Allowed, row.Version, row.UpdatedAt = input.Allowed, row.Version+1, now
	}
	audit := input.Audit
	actor := input.Actor
	principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
	audit.PrincipalID, audit.PrincipalType = &actor.PrincipalID, &principalType
	audit.TenantID, audit.ContextType = &actor.TenantID, &contextType
	err = iam.NewAuditWriter(r.identity()).Write(ctx, iam.AuditEvent{Metadata: audit,
		EventName: "system.engine_access_direct_permission.changed", Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskHigh,
		ModuleName: "system", EntityType: "engine_access_direct_permission", EntityID: row.ID.String(), Details: map[string]any{
			"engine_id": row.EngineID, "item_fingerprint": row.ItemFingerprint, "allowed": row.Allowed, "version": row.Version,
			"previous_source": decision.Source, "previous_version": decision.Version, "reason": strings.TrimSpace(input.Reason)}})
	if err != nil {
		return nil, err
	}
	if err = verify(); err != nil {
		return nil, err
	}
	return row, nil
}
