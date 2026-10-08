package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const (
	approvalModeCatalog     = "catalog"
	approvalModeIndependent = "independent"
)

var (
	errApprovalRequirementUnavailable = errors.New("current Catalog approval requirement is unavailable")
	errApprovalRequirementVersion     = errors.New("approval requirement version conflict")
	errApprovalRequirementInput       = errors.New("invalid approval requirement change")
)

// No approver allowlist or editable copy of Catalog responsibility facts.
// A successor is verified only at handoff and recorded in its audit event.
type approvalRequirement struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	TenantID    int64
	EngineID    int64
	CatalogPath json.RawMessage `gorm:"type:jsonb"`
	Mode        string
	Version     int64
	UpdatedAt   time.Time
}

func (approvalRequirement) TableName() string { return "system.engine_access_approval_requirements" }

func (r *Repository) listApprovalRequirements(ctx context.Context, tenantID, engineID int64, page, size int) ([]approvalRequirement, int64, error) {
	query := r.db.WithContext(ctx).Model(&approvalRequirement{}).Where("tenant_id = ? AND engine_id = ?", tenantID, engineID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []approvalRequirement
	if err := query.Order("id ASC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *Repository) getApprovalRequirement(ctx context.Context, tenantID, engineID int64, id uuid.UUID) (*approvalRequirement, error) {
	var row approvalRequirement
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ? AND id = ?", tenantID, engineID, id).Take(&row).Error; err != nil {
		return nil, mapError(err)
	}
	return &row, nil
}

// Private transaction command, not an HTTP DTO. Zero ExpectedVersion denotes
// explicit initialization only; changing an existing fact requires its version.
// The owner service must authenticate the user and lock current IAM and
// engine/delegation qualifications BEFORE entering this target boundary.
type approvalRequirementChange struct {
	TenantID, ExpectedVersion, SuccessorPrincipalID int64
	Path                                            engineplugin.EngineCatalogPath
	Mode, Reason                                    string
	Audit                                           iam.AuditMetadata
}

func (r *Repository) approvalRequirement(ctx context.Context, tenantID int64, engineID int64, path json.RawMessage) (*approvalRequirement, error) {
	var row approvalRequirement
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ? AND target_digest = sha256(convert_to(?::jsonb::text, 'UTF8')) AND catalog_path = ?::jsonb",
		tenantID, engineID, string(path), string(path)).Take(&row).Error
	return &row, err
}

func (r *Repository) requireCatalogApproval(ctx context.Context, request fulfillmentRequest, path json.RawMessage) error {
	row, err := r.approvalRequirement(ctx, request.TenantID, int64(request.Path.EngineID), path)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && row.Mode != approvalModeCatalog) {
		return errApprovalRequirementUnavailable
	}
	if err != nil {
		return err
	}
	if row.Version != request.RequirementVersion {
		return errApprovalRequirementVersion
	}
	return nil
}

// Called only on the owner's transaction repository. verify must recheck local
// prelocked governance/delegation facts and, for handoff to independent mode,
// the current successor qualification. First configuration has no successor
// and does not qualify anyone to approve or read data. No earlier locks or IO.
// nil cannot initialize or change a requirement. Public initialization supplies
// current local IAM/delegation checks; public updates also check the successor.
func (r *Repository) changeApprovalRequirement(ctx context.Context, input approvalRequirementChange, verify func(*Repository) error) (*approvalRequirement, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, errApprovalRequirementInput
	}
	audit := input.Audit
	if input.TenantID <= 0 || input.ExpectedVersion < 0 || !validReason(input.Reason) || verify == nil ||
		(input.Mode != approvalModeCatalog && input.Mode != approvalModeIndependent) ||
		(input.ExpectedVersion == 0 && input.SuccessorPrincipalID != 0) ||
		(input.Mode == approvalModeIndependent && input.ExpectedVersion > 0 && input.SuccessorPrincipalID <= 0) ||
		(input.Mode == approvalModeCatalog && input.SuccessorPrincipalID != 0) ||
		audit.PrincipalID == nil || *audit.PrincipalID <= 0 || audit.PrincipalType == nil || *audit.PrincipalType != iam.PrincipalTypeUser ||
		audit.ContextType == nil || *audit.ContextType != iam.ContextTypeTenant || audit.TenantID == nil || *audit.TenantID != input.TenantID {
		return nil, errApprovalRequirementInput
	}
	path, err := encodeFulfillmentPath(input.Path)
	if err != nil {
		return nil, errApprovalRequirementInput
	}
	if err := r.lockFulfillmentTarget(ctx, input.TenantID, path); err != nil {
		return nil, err
	}
	row, err := r.approvalRequirement(ctx, input.TenantID, int64(input.Path.EngineID), path)
	creating := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !creating {
		return nil, err
	}
	if (creating && input.ExpectedVersion != 0) || (!creating && row.Version != input.ExpectedVersion) {
		return nil, errApprovalRequirementVersion
	}
	now, err := r.wallClock(ctx)
	if err != nil {
		return nil, err
	}
	// Qualification is the final read after target-lock waits. Do not perform
	// another clock read between its expiry check and the write.
	if err := verify(r); err != nil {
		return nil, err
	}
	if !creating && row.Mode == input.Mode {
		return row, nil
	}
	if creating {
		row = &approvalRequirement{ID: uuid.New(), TenantID: input.TenantID, EngineID: int64(input.Path.EngineID),
			CatalogPath: path, Mode: input.Mode, Version: 1, UpdatedAt: now}
		if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
			// Hash collisions never merge different exact paths into one identity.
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return nil, errApprovalRequirementVersion
			}
			return nil, err
		}
	} else {
		result := r.db.WithContext(ctx).Model(&approvalRequirement{}).Where("id = ? AND tenant_id = ? AND version = ?", row.ID, input.TenantID, input.ExpectedVersion).
			Updates(map[string]any{"mode": input.Mode, "version": gorm.Expr("version + 1"), "updated_at": now})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, errApprovalRequirementVersion
		}
		row.Mode, row.Version, row.UpdatedAt = input.Mode, input.ExpectedVersion+1, now
	}
	details := map[string]any{"engine_id": row.EngineID, "catalog_path": input.Path, "mode": row.Mode,
		"previous_version": input.ExpectedVersion, "version": row.Version, "reason": strings.TrimSpace(input.Reason)}
	if input.Mode == approvalModeIndependent && input.ExpectedVersion > 0 {
		details["successor_principal_id"] = input.SuccessorPrincipalID
	}
	err = iam.NewAuditWriter(r.identity()).Write(ctx, iam.AuditEvent{Metadata: audit,
		EventName: "system.engine_access_approval_requirement.changed", Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskHigh,
		ModuleName: "system", EntityType: "engine_access_approval_requirement", EntityID: row.ID.String(), Details: details})
	return row, err
}
