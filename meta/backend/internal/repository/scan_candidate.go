package repository

import (
	"context"
	"errors"

	"github.com/addp/common/execution"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NextScanCandidate applies Meta's dependency rules in the caller's transaction.
func NextScanCandidate(ctx context.Context, tx *gorm.DB) (*execution.PendingIdentity, error) {
	var candidate execution.PendingIdentity
	query := tx.WithContext(ctx).Model(&execution.TaskExecution{}).
		Select("execution_id", "tenant_id", "module", "task_type", "source").
		Where("module=? AND task_type=? AND execution_boundary=? AND status=?", execution.ModuleMeta, execution.TaskTypeScan, execution.ExecutionBoundaryBounded, execution.ExecutionStatusPending).
		Where(`NOT EXISTS (SELECT 1 FROM common.task_executions AS basic
 WHERE basic.execution_id = task_executions.execution_config->>'production_basic_execution_id'
 AND basic.tenant_id = task_executions.tenant_id AND basic.module = 'meta' AND basic.task_type = 'scan'
 AND basic.status IN ('pending','running'))`).
		Order("created_at ASC, id ASC").
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	if err := query.Take(&candidate).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &candidate, nil
}
