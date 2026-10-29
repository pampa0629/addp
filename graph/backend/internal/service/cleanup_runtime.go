package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/gorm"
)

// runCleanup never holds G while waiting on a build execution. Closing admission
// is durable even if C expires, the process dies or an external effect is unknown.
func (s *CleanupService) runCleanup(ctx context.Context, run *cleanup.Run) error {
	if run.Input.Action == events.CleanupActionScan {
		return run.Effect(ctx, func(tx *gorm.DB) error {
			scoped := &CleanupService{db: tx}
			candidates, err := scoped.listCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
			if err != nil {
				return err
			}
			stats := candidates.stats()
			impact, err := graphEngineDeletionImpact(candidates)
			if err != nil {
				return err
			}
			return completeGraphCleanup(ctx, tx, run, stats, impact, "success", "")
		})
	}
	var graphs []models.KnowledgeGraph
	// Phase 1: C -> G -> graph rows. No target execution is locked here.
	if err := run.Effect(ctx, func(tx *gorm.DB) error {
		candidates, err := (&CleanupService{db: tx}).listCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
		if err != nil {
			return err
		}
		if len(candidates.knowledgeGraphs) == 0 {
			return nil
		}
		if err := repository.LockAdmission(tx, run.Input.TenantID, true); err != nil {
			return err
		}
		// Repeat scope discovery under G; mutation paths also hold G.
		candidates, err = (&CleanupService{db: tx}).listCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
		if err != nil {
			return err
		}
		graphs = candidates.knowledgeGraphs
		for _, kg := range graphs {
			if kg.AdmissionClosed && kg.AdmissionExecutionID != run.Execution.ExecutionID {
				if err := repository.CheckCleanupQuiescence(tx, &kg, ""); err != nil {
					return err
				}
			}
			if err := tx.Model(&models.KnowledgeGraph{}).Where("id = ? AND tenant_id = ?", kg.ID, kg.TenantID).Updates(map[string]interface{}{"admission_closed": true, "admission_cause": "cleanup", "admission_execution_id": run.Execution.ExecutionID, "admission_reconciled_at": nil, "admission_reconciled_by": 0, "admission_reconciliation_digest": "", "version": gorm.Expr("version + 1")}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// Phase 2: no G is held across cancellation calls or polling. Each short
	// cancellation commit is freshly authorized/fenced by C, then E -> G -> T.
	ids := knowledgeGraphIDs(graphs)
	var controls []models.BuildExecutionControl
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND graph_id IN ?", run.Input.TenantID, ids).Find(&controls).Error; err != nil {
			return err
		}
	}
	var cancellationIDs []string
	for _, control := range controls {
		var item execution.TaskExecution
		if err := s.db.WithContext(ctx).Where("execution_id = ? AND tenant_id = ?", control.ExecutionID, run.Input.TenantID).First(&item).Error; err != nil {
			return err
		}
		if item.Status != "pending" && item.Status != "running" {
			continue
		}
		if err := run.Effect(ctx, func(tx *gorm.DB) error {
			_, err := repository.NewBuildRepository(tx).CancelExecution(ctx, control.TaskID, control.GraphID, control.TenantID, 0, item.ExecutionID, item.Attempt)
			return err
		}); err != nil {
			return err
		}
		cancellationIDs = append(cancellationIDs, item.ExecutionID)
	}
	waitUntil := time.Now().Add(30 * time.Second)
	for {
		err := run.Effect(ctx, func(tx *gorm.DB) error { return checkCleanupGraphs(tx, run, ids) })
		if err == nil {
			break
		}
		if !errors.Is(err, repository.ErrGraphReconciliationIncomplete) {
			return err
		}
		if !time.Now().Before(waitUntil) {
			return s.completeBlockedCleanup(ctx, run, ids, "active_execution")
		}
		// Unknown provider operations have no evidence-based automatic resolution.
		var unresolved int64
		if len(ids) > 0 {
			if err := s.db.WithContext(ctx).Model(&models.GraphOperation{}).Where("tenant_id = ? AND graph_id IN ? AND state = ?", run.Input.TenantID, ids, models.GraphOperationUncertain).Count(&unresolved).Error; err != nil {
				return err
			}
		}
		if unresolved > 0 {
			return s.completeBlockedCleanup(ctx, run, ids, "external_effect_uncertain")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	// Phase 3: C -> G -> domain rows. Quiescence uses ordinary E reads only.
	return run.Effect(ctx, func(tx *gorm.DB) error {
		if err := checkCleanupGraphs(tx, run, ids); err != nil {
			return err
		}
		scoped := &CleanupService{db: tx}
		candidates, err := scoped.listCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
		if err != nil {
			return err
		}
		if len(candidates.knowledgeGraphs) != len(ids) {
			return commonapi.ErrConflict
		}
		owned := map[uint]bool{}
		for _, id := range ids {
			owned[id] = true
		}
		for _, kg := range candidates.knowledgeGraphs {
			if !owned[kg.ID] || kg.AdmissionExecutionID != run.Execution.ExecutionID {
				return commonapi.ErrConflict
			}
		}
		stats, err := scoped.applyCleanup(ctx, run.Input.TenantID, run.Input.CleanupMode, run.Input.Context.Map())
		if err != nil {
			return err
		}
		if len(stats.Errors) > 0 {
			return fmt.Errorf("graph cleanup domain transaction failed")
		}
		var cancelled int64
		if len(cancellationIDs) > 0 {
			if err := tx.Model(&execution.TaskExecution{}).Where("tenant_id = ? AND execution_id IN ? AND status = ?", run.Input.TenantID, cancellationIDs, execution.ExecutionStatusCancelled).Count(&cancelled).Error; err != nil {
				return err
			}
		}
		stats.CancelledBuildTasks = int(cancelled)
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			return err
		}
		return completeGraphCleanup(ctx, tx, run, stats, impact, "success", "")
	})
}

func checkCleanupGraphs(tx *gorm.DB, run *cleanup.Run, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	if err := repository.LockAdmission(tx, run.Input.TenantID, true); err != nil {
		return err
	}
	var graphs []models.KnowledgeGraph
	if err := tx.Where("tenant_id = ? AND id IN ?", run.Input.TenantID, ids).Find(&graphs).Error; err != nil {
		return err
	}
	if len(graphs) != len(ids) {
		return commonapi.ErrConflict
	}
	for _, kg := range graphs {
		if !kg.AdmissionClosed || kg.AdmissionExecutionID != run.Execution.ExecutionID {
			return commonapi.ErrConflict
		}
		if err := repository.CheckCleanupQuiescence(tx, &kg, run.Execution.ExecutionID); err != nil {
			return err
		}
	}
	return nil
}

func (s *CleanupService) completeBlockedCleanup(ctx context.Context, run *cleanup.Run, ids []uint, reason string) error {
	return run.Effect(ctx, func(tx *gorm.DB) error {
		stats := &GraphCleanupStats{KnowledgeGraphs: len(ids), Errors: []string{reason}}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			return err
		}
		return completeGraphCleanup(ctx, tx, run, stats, impact, "failed", reason)
	})
}
func completeGraphCleanup(ctx context.Context, tx *gorm.DB, run *cleanup.Run, stats *GraphCleanupStats, impact events.CleanupImpactData, status, reason string) error {
	summary := graphScanSummary(stats)
	if run.Input.Action == events.CleanupActionExecute {
		summary = graphExecuteSummary(stats)
	}
	result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleGraph, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: status, Summary: summary, Impact: &impact}
	receipt, err := execution.NewCleanupReceipt(run.Execution, result, reason)
	if err != nil {
		return err
	}
	return cleanup.Complete(ctx, tx, run.Lease, receipt, graphCleanupCounters(stats))
}
