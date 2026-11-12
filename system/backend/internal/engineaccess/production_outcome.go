package engineaccess

import (
	"context"
	"errors"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/production"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const productionApprovalMode = "task_production"

// Only terminal continuation history. It never supplies an access verdict.
type initialProductionOutcome struct {
	TenantID           int64     `gorm:"primaryKey"`
	ProducerOwner      string    `gorm:"primaryKey"`
	ExecutionID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	ResultID           uuid.UUID `gorm:"type:uuid;primaryKey"`
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

func (r *Repository) findInitialProductionOutcome(ctx context.Context, tenantID int64, ref production.Reference) (*initialProductionOutcome, error) {
	var row initialProductionOutcome
	err := r.db.WithContext(ctx).Where("tenant_id=? AND producer_owner=? AND execution_id=? AND result_id=?", tenantID, ref.Owner, ref.ExecutionID, ref.ResultID).Take(&row).Error
	return &row, err
}

// Request IDs are derived by System, not supplied by the producer. The outcome
// composite key and Grant's production unique key also enforce one result.
func productionGrantCommand(tenantID int64, ref production.Reference) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("addp.initial-production|"+strconv.FormatInt(tenantID, 10)+"|"+ref.Owner+"|"+ref.ExecutionID+"|"+ref.ResultID))
}

func (r *Repository) insertInitialProductionOutcome(ctx context.Context, actor Actor, ref production.Reference, grantID *uuid.UUID, denial string) (*initialProductionOutcome, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok || actor.TenantID <= 0 || ref.Validate() != nil {
		return nil, errInitialProductionEvidence
	}
	now, err := r.wallClock(ctx)
	if err != nil {
		return nil, err
	}
	row := &initialProductionOutcome{TenantID: actor.TenantID, ProducerOwner: ref.Owner, ExecutionID: uuid.MustParse(ref.ExecutionID), ResultID: uuid.MustParse(ref.ResultID),
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
		grant.ProductionResultID == nil || *grant.ProductionResultID != row.ResultID || grant.ProductionExecutionID == nil || *grant.ProductionExecutionID != row.ExecutionID || grant.IssuerPrincipalID == nil || *grant.IssuerPrincipalID != row.IssuerPrincipalID ||
		grant.IssuerMembershipID == nil || *grant.IssuerMembershipID != row.IssuerMembershipID {
		return nil, errors.Join(commonapi.ErrConflict, errInitialProductionEvidence)
	}
	return r.sourceGrantHistory(ctx, grant)
}
