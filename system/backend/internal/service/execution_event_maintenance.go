package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/addp/common/execution"
	"gorm.io/gorm"
)

// RunExecutionEventMaintenance owns shared-storage housekeeping, separate from
// platform log pruning and operation audit. Cancellation waits for the current batch.
func RunExecutionEventMaintenance(ctx context.Context, db *gorm.DB, log *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		batchCtx, cancel := context.WithTimeout(ctx, time.Minute)
		total := int64(0)
		failed := false
		for batch := 0; batch < 100; batch++ {
			result, err := execution.PruneEvents(batchCtx, db, time.Now().UTC(), 1000)
			if err != nil {
				if ctx.Err() == nil {
					log.Error("执行过程事件保留清理失败", "deleted", total)
				}
				failed = true
				break
			}
			total += result.Deleted
			if result.Deleted < 1000 {
				break
			}
		}
		cancel()
		if !failed {
			log.Info("执行过程事件保留清理完成", "deleted", total, "retention_days", 30, "budget_exhausted", total == 100000)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
