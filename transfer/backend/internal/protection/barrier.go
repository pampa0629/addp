package protection

import (
	"context"
	"errors"
	"fmt"

	"github.com/addp/common/dataprotection"
	commonexecution "github.com/addp/common/execution"
	"gorm.io/gorm"
)

// ExecutionBarrier prevents Transfer from acknowledging an installed cursor
// while a bounded or continuous execution that reads a newly managed source
// is still active under an older cursor. New executions are gated from the
// durable owner checkpoint before reading.
type ExecutionBarrier struct {
	db   *gorm.DB
	gate *Gate
}

func NewExecutionBarrier(db *gorm.DB, gate *Gate) *ExecutionBarrier {
	return &ExecutionBarrier{db: db, gate: gate}
}

func (b *ExecutionBarrier) ReadyToAcknowledge(ctx context.Context, tenantID int64, _ string) error {
	if b == nil || b.db == nil || b.gate == nil || tenantID <= 0 {
		return fmt.Errorf("transfer protection acknowledgement barrier is not configured")
	}
	var executions []commonexecution.TaskExecution
	if err := b.db.WithContext(ctx).
		Where("tenant_id = ? AND module = ? AND status IN ?", tenantID, commonexecution.ModuleTransfer,
			[]string{commonexecution.ExecutionStatusPending, commonexecution.ExecutionStatusRunning}).
		Find(&executions).Error; err != nil {
		return fmt.Errorf("list active transfer executions for protection barrier: %w", err)
	}
	for _, execution := range executions {
		if err := b.gate.RequireSourceConfig(ctx, uint(tenantID), execution.ExecutionConfig); err != nil {
			if errors.Is(err, dataprotection.ErrDenied) {
				return fmt.Errorf("transfer execution %s still reads a managed source", execution.ExecutionID)
			}
			return fmt.Errorf("resolve transfer execution %s source for protection barrier: %w", execution.ExecutionID, err)
		}
	}
	return nil
}
