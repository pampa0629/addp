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
	assessmentCursor := int64(0)
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
		if ctx.Err() == nil {
			assessmentCtx, assessmentCancel := context.WithTimeout(ctx, 15*time.Second)
			report, err := execution.AssessRetention(assessmentCtx, db, execution.RetentionAssessmentOptions{AfterID: assessmentCursor})
			assessmentCancel()
			if err != nil {
				if ctx.Err() == nil {
					// Do not publish partial facts or database error text.
					log.Warn("执行概览只读保留评估失败")
				}
			} else {
				logExecutionRetentionAssessment(log, report)
				assessmentCursor = report.NextAfterID
				if !report.HasMore {
					assessmentCursor = 0
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func logExecutionRetentionAssessment(log *slog.Logger, report *execution.RetentionAssessment) {
	log.Info("执行概览只读保留评估完成", "read_only", report.ReadOnly,
		"retention_days", 180, "seeds_evaluated", report.SeedsEvaluated,
		"trees_observed", len(report.Trees), "complete_trees", report.CompleteTrees,
		"age_eligible_trees", report.AgeEligibleTrees, "rows_read", report.RowsRead,
		"queries", report.Queries, "reason_counts", report.ReasonCounts,
		"next_after_id", report.NextAfterID, "has_more", report.HasMore,
		"budget_exhausted", report.BudgetExhausted)
}
