package protection

import (
	"context"
	"fmt"
	"time"

	commonexecution "github.com/addp/common/execution"
	"gorm.io/gorm"
)

type ActiveReader interface {
	HasActiveExecutionsForTenant(int64) bool
}

type ExecutionBarrier struct {
	db     *gorm.DB
	gate   *Gate
	reader ActiveReader
}

func NewExecutionBarrier(db *gorm.DB, gate *Gate, reader ActiveReader) *ExecutionBarrier {
	return &ExecutionBarrier{db: db, gate: gate, reader: reader}
}

// ReadyToAcknowledge waits for reads that may have started under the previous
// cursor. New backend and worker executions gate against the already-durable
// Develop checkpoint before touching data.
func (b *ExecutionBarrier) ReadyToAcknowledge(ctx context.Context, tenantID int64, _ string) error {
	if b == nil || b.db == nil || b.gate == nil || tenantID <= 0 {
		return fmt.Errorf("develop protection acknowledgement barrier is not configured")
	}
	if b.gate.HasActiveExecutionsForTenant(tenantID) || (b.reader != nil && b.reader.HasActiveExecutionsForTenant(tenantID)) {
		return fmt.Errorf("develop still has executions running under the previous protection cursor")
	}
	now := time.Now().UTC()
	var active int64
	if err := b.db.WithContext(ctx).Model(&commonexecution.TaskExecution{}).
		Where("tenant_id = ? AND module = ? AND status = ?", tenantID, commonexecution.ModuleDevelop, commonexecution.ExecutionStatusRunning).
		Where("(lease_expires_at IS NOT NULL AND lease_expires_at >= ?) OR (lease_token IS NULL AND authorization_expires_at IS NOT NULL AND authorization_expires_at >= ?)", now, now).
		Count(&active).Error; err != nil {
		return fmt.Errorf("count active develop executions for protection barrier: %w", err)
	}
	if active > 0 {
		return fmt.Errorf("develop still has executions running under the previous protection cursor")
	}
	return nil
}
