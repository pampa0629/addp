package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

// PrivateFileProduction remains inside Develop. The HTTP projection must not
// expose the frozen configuration, Runtime manifest or Provider receipt.
type PrivateFileProduction struct {
	Facts              client.DevelopFileExecutionFacts
	SourceReads        []PrivateFileSourceRead
	OperationID        string
	Attempt            int
	RetryOfExecutionID *string
	CommittedAt        time.Time
}

// ReadPrivateProduction observes the original operation, regardless of the
// parent's terminal state. It performs no source/target I/O and never republishes.
func (r *FileExecutionRepository) ReadPrivateProduction(ctx context.Context, tenant uint, executionID, resultID string) (*PrivateFileProduction, error) {
	var row struct {
		models.FileExecution `gorm:"embedded"`
		ProductionAttempt    int
		ProductionRetry      *string
	}
	err := r.db.WithContext(ctx).Table("develop.file_executions AS f").
		Select("f.*,e.attempt AS production_attempt,e.retry_of_execution_id AS production_retry").
		Joins("JOIN common.task_executions e ON e.execution_id=f.execution_id AND e.tenant_id=f.tenant_id").
		Where("f.tenant_id=? AND f.execution_id=? AND e.module='develop' AND e.source='develop' AND e.task_type='workflow' AND e.execution_boundary='bounded' AND e.attempt>=1", tenant, executionID).
		Where("e.actor_principal_id=f.actor_principal_id AND e.actor_tenant_membership_id=f.actor_membership_id AND e.issued_authorization_version=f.authorization_version AND e.triggered_by=f.actor_principal_id").
		Take(&row).Error
	if err != nil {
		return nil, err
	}
	if len(row.PublicationJSON) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	facts, record, err := privateFileFactsAndPublication(&row.FileExecution)
	if err != nil {
		return nil, err
	}
	if record.Facts.Request.OperationID != resultID {
		return nil, gorm.ErrRecordNotFound
	}
	outcome, observed := record.Outcome, record.OutcomeObservedAt
	if outcome == nil {
		outcome, observed = record.RecoveryOutcome, record.RecoveryObservedAt
	}
	if outcome == nil || outcome.State != plugin.NewFilePublished || observed == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var reads []PrivateFileSourceRead
	if len(row.SourceReadProofsJSON) != 0 {
		if err := json.Unmarshal(row.SourceReadProofsJSON, &reads); err != nil {
			return nil, err
		}
	}
	return &PrivateFileProduction{Facts: *facts, SourceReads: reads, OperationID: resultID,
		Attempt: row.ProductionAttempt, RetryOfExecutionID: row.ProductionRetry, CommittedAt: *observed}, nil
}
