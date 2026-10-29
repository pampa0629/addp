package service

import (
	"context"
	"fmt"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/meta/internal/metacleanup"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func (s *CleanupService) runCleanupExecution(ctx context.Context, run *cleanup.Run) error {
	if run.Input.Context.TenantID != 0 {
		return fmt.Errorf("historical Tenant cleanup is unsupported")
	}
	cleaner, err := s.dbCleaner.PrepareEngineFacts(ctx, run.Input.TenantID, metacleanup.ScopeFromContext(run.Input.Context.Map()))
	if err != nil {
		return err
	}
	return run.Effect(ctx, func(tx *gorm.DB) error {
		scoped := &CleanupService{db: tx, dbCleaner: cleaner.WithDB(tx), log: s.log}
		result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleMeta, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: events.CleanupResultSuccess}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			return err
		}
		var counters map[string]int64
		if run.Input.Action == events.CleanupActionScan {
			stats, err := scoped.ScanReclaimCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
			if err != nil {
				return err
			}
			result.Summary = metaScanSummary(stats)
			counters = metaScanCounters(stats)
			if run.Input.CauseEvent == events.CleanupCauseEngineDeleting {
				impact, err = scoped.metaEngineDeletionImpact(ctx, run.Input.TenantID, run.Input.Context.Map(), stats)
				if err != nil {
					return err
				}
			}
		} else {
			stats, err := scoped.ExecuteCleanup(ctx, run.Input.TenantID, run.Input.CleanupMode, run.Input.Context.Map())
			if err != nil {
				return err
			}
			if len(stats.Errors) != 0 {
				return fmt.Errorf("Meta cleanup transaction failed")
			}
			result.Summary = metaExecuteSummary(stats)
			counters = metaExecuteCounters(stats)
		}
		result.Impact = &impact
		receipt, err := execution.NewCleanupReceipt(run.Execution, result, "")
		if err != nil {
			return err
		}
		return cleanup.Complete(ctx, tx, run.Lease, receipt, counters)
	})
}
func metaScanCounters(s *models.MetaCleanupStatistics) map[string]int64 {
	return map[string]int64{"invalid_engines.count": int64(s.InvalidEngines.Count), "orphan_items.count": int64(s.OrphanItems.Count), "expired_data.count": int64(s.ExpiredData.Count), "expired_data.threshold_days": int64(s.ExpiredData.ThresholdDays), "logical_cleanup_candidates.nodes": int64(s.LogicalCleanupCandidates.Nodes), "logical_cleanup_candidates.items": int64(s.LogicalCleanupCandidates.Items), "retained_lineage_references.nodes": int64(s.RetainedLineageReferences.Nodes), "retained_lineage_references.items": int64(s.RetainedLineageReferences.Items), "duplicate_fingerprints.count": int64(s.DuplicateFingerprints.Count), "scan_task_definitions.count": int64(s.ScanTaskDefinitions.Count)}
}
func metaExecuteCounters(s *models.MetaCleanupExecuteResult) map[string]int64 {
	return map[string]int64{"deleted_nodes": int64(s.DeletedNodes), "deleted_items": int64(s.DeletedItems), "deleted_fingerprints": int64(s.DeletedFingerprints), "disabled_scan_task_definitions": int64(s.DisabledScanTaskDefinitions), "deleted_scan_task_definitions": int64(s.DeletedScanTaskDefinitions), "retained_lineage_items": int64(s.RetainedLineageItems), "retained_referenced_nodes": int64(s.RetainedReferencedNodes), "error_count": int64(len(s.Errors))}
}
