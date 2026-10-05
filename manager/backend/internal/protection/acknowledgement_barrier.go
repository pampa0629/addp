package protection

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/execution"
	"gorm.io/gorm"
)

// AcknowledgementBarrier runs only after the new protection cursor is durable.
// Pending work will refresh that cursor when it enters the read boundary.
type AcknowledgementBarrier struct {
	db    *gorm.DB
	reads *ReadBoundary
	index interface {
		ReadyToAcknowledge(context.Context, int64, string) error
	}
}

func NewAcknowledgementBarrier(db *gorm.DB, reads *ReadBoundary, index interface {
	ReadyToAcknowledge(context.Context, int64, string) error
}) *AcknowledgementBarrier {
	return &AcknowledgementBarrier{db: db, reads: reads, index: index}
}

func (b *AcknowledgementBarrier) ReadyToAcknowledge(ctx context.Context, tenantID int64, cursor string) error {
	if b == nil || b.db == nil || b.reads == nil || b.reads.store == nil || b.index == nil || tenantID <= 0 || cursor == "" {
		return fmt.Errorf("manager protection acknowledgement barrier is unavailable")
	}
	if b.reads.HasActiveExecutionsForTenant(tenantID) {
		return fmt.Errorf("manager protection reads are still active")
	}
	var active int64
	if err := b.db.WithContext(ctx).Model(&execution.TaskExecution{}).
		Where("tenant_id = ? AND module = ? AND status = ?", tenantID, execution.ModuleManager, execution.ExecutionStatusRunning).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at >= ?", time.Now().UTC()).
		Count(&active).Error; err != nil {
		return fmt.Errorf("count active manager protection executions: %w", err)
	}
	if active > 0 {
		return fmt.Errorf("manager protection executions are still active")
	}
	return b.index.ReadyToAcknowledge(ctx, tenantID, cursor)
}
