package engineaccess

import (
	"context"
	"errors"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const productionOwner = "transfer"
const productionApprovalMode = "task_production"

// Only terminal continuation history. It never supplies an access verdict.
type initialProductionOutcome struct {
	TenantID           int64     `gorm:"primaryKey"`
	ProducerOwner      string    `gorm:"primaryKey"`
	ExecutionID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Outcome            string
	RecordedAt         time.Time
	GrantRequestID     *uuid.UUID `gorm:"type:uuid"`
	DenialCode         *string
	IssuerPrincipalID  int64
	IssuerMembershipID int64
}

func (initialProductionOutcome) TableName() string {
	return "system.engine_access_initial_grant_outcomes"
}

func (r *Repository) findInitialProductionOutcome(ctx context.Context, tenantID int64, executionID uuid.UUID) (*initialProductionOutcome, error) {
	var row initialProductionOutcome
	err := r.db.WithContext(ctx).Where("tenant_id=? AND producer_owner=? AND execution_id=?", tenantID, productionOwner, executionID).Take(&row).Error
	return &row, err
}

// Request IDs are derived by System, not supplied by the producer. The outcome
// composite key and Grant's production unique key also enforce one result.
func productionGrantCommand(tenantID int64, executionID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("addp.initial-production|"+strconv.FormatInt(tenantID, 10)+"|"+productionOwner+"|"+executionID.String()))
}

func (r *Repository) insertInitialProductionOutcome(ctx context.Context, actor Actor, executionID uuid.UUID, grantID *uuid.UUID, denial string) (*initialProductionOutcome, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok || actor.TenantID <= 0 || executionID == uuid.Nil {
		return nil, errInitialProductionEvidence
	}
	now, err := r.wallClock(ctx)
	if err != nil {
		return nil, err
	}
	row := &initialProductionOutcome{TenantID: actor.TenantID, ProducerOwner: productionOwner, ExecutionID: executionID,
		Outcome: "issued", RecordedAt: now, GrantRequestID: grantID, IssuerPrincipalID: actor.PrincipalID, IssuerMembershipID: actor.MembershipID}
	if grantID == nil {
		row.Outcome = "denied"
		row.DenialCode = &denial
	} else if denial != "" {
		return nil, commonapi.ErrBadRequest
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (r *Repository) initialProductionGrantHistory(ctx context.Context, row *initialProductionOutcome) (*SourceGrantView, error) {
	if row == nil || row.Outcome != "issued" || row.GrantRequestID == nil {
		return nil, errInitialProductionEvidence
	}
	grant, err := r.findSourceGrant(ctx, *row.GrantRequestID)
	if err != nil {
		return nil, err
	}
	if grant.ApprovalMode != productionApprovalMode || grant.TenantID != row.TenantID || grant.ProducerOwner == nil || *grant.ProducerOwner != row.ProducerOwner ||
		grant.ProductionExecutionID == nil || *grant.ProductionExecutionID != row.ExecutionID || grant.IssuerPrincipalID == nil || *grant.IssuerPrincipalID != row.IssuerPrincipalID ||
		grant.IssuerMembershipID == nil || *grant.IssuerMembershipID != row.IssuerMembershipID {
		return nil, errors.Join(commonapi.ErrConflict, errInitialProductionEvidence)
	}
	return r.sourceGrantHistory(ctx, grant)
}
