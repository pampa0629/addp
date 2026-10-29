package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/addp/common/cleanup"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/common/resourcetree"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CleanupService struct {
	db      *gorm.DB
	runtime *cleanup.Runner
}

type QualityCleanupStats struct {
	Rules         int      `json:"rules"`
	DeletedRules  int      `json:"deleted_rules,omitempty"`
	Plans         int      `json:"plans"`
	Issues        int      `json:"issues"`
	IgnoredIssues int      `json:"ignored_issues,omitempty"`
	SkippedIssues int      `json:"skipped_issues,omitempty"`
	DeletedPlans  int      `json:"deleted_plans,omitempty"`
	DeletedIssues int      `json:"deleted_issues,omitempty"`
	Errors        []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, systemClient *commonClient.SystemServiceClient) *CleanupService {
	s := &CleanupService{db: db}
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleQuality, System: systemClient, Log: logger.With("component", "quality_cleanup_service"), Handle: s.runCleanup}
	return s
}
func (s *CleanupService) Start(ctx context.Context) error { return s.runtime.Start(ctx) }
func (s *CleanupService) Stop()                           { s.runtime.Stop() }

func (s *CleanupService) runCleanup(ctx context.Context, run *cleanup.Run) error {
	return run.Effect(ctx, func(tx *gorm.DB) error {
		scoped := &CleanupService{db: tx}
		var stats *QualityCleanupStats
		var err error
		if run.Input.Action == events.CleanupActionScan {
			stats, err = scoped.ScanReclaimCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
		} else {
			stats, err = scoped.ExecuteCleanup(ctx, run.Input.TenantID, run.Input.CleanupMode, run.Input.Context.Map())
		}
		if err != nil {
			return err
		}
		if len(stats.Errors) > 0 {
			return fmt.Errorf("quality cleanup domain transaction failed")
		}
		summary := qualityScanSummary(stats)
		if run.Input.Action == events.CleanupActionExecute {
			summary = qualityExecuteSummary(stats)
		}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			return err
		}
		if run.Input.Action == events.CleanupActionScan && run.Input.CauseEvent == events.CleanupCauseEngineDeleting {
			candidates, e := scoped.listCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
			if e != nil {
				return e
			}
			active, e := scoped.listActiveQualityCleanupTaskIDs(ctx, candidates.plans)
			if e != nil {
				return e
			}
			impact, err = qualityEngineDeletionImpact(candidates, active)
			if err != nil {
				return err
			}
		}
		result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleQuality, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: events.CleanupResultSuccess, Summary: summary, Impact: &impact}
		receipt, err := commonExecution.NewCleanupReceipt(run.Execution, result, "")
		if err != nil {
			return err
		}
		return cleanup.Complete(ctx, tx, run.Lease, receipt, qualityCleanupCounters(stats))
	})
}

func qualityCleanupCounters(s *QualityCleanupStats) map[string]int64 {
	return map[string]int64{"rules": int64(s.Rules), "deleted_rules": int64(s.DeletedRules), "plans": int64(s.Plans), "issues": int64(s.Issues), "ignored_issues": int64(s.IgnoredIssues), "skipped_issues": int64(s.SkippedIssues), "deleted_plans": int64(s.DeletedPlans), "deleted_issues": int64(s.DeletedIssues), "error_count": int64(len(s.Errors))}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*QualityCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*QualityCleanupStats, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return nil, err
	}
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}

	stats := candidates.stats()
	if _, hasEngineID := qualityCleanupContextInt64(cleanupContext, "engine_id"); hasEngineID {
		applied, err := s.disableCandidates(ctx, candidates)
		if err != nil {
			stats.Errors = append(stats.Errors, err.Error())
			return stats, nil
		}
		mergeQualityCleanupStats(stats, &applied)
		return stats, nil
	}
	switch cleanupMode {
	case events.CleanupModeLogical:
		applied, err := s.disableCandidates(ctx, candidates)
		if err != nil {
			stats.Errors = append(stats.Errors, err.Error())
			return stats, nil
		}
		mergeQualityCleanupStats(stats, &applied)
	case events.CleanupModePhysical:
		auditTenant, _ := qualityCleanupContextInt64(cleanupContext, "tenant_id")
		if auditTenant != int64(tenantID) {
			auditTenant = 0
		}
		applied, err := s.deleteCandidates(ctx, candidates, auditTenant)
		if err != nil {
			stats.Errors = append(stats.Errors, err.Error())
			return stats, nil
		}
		mergeQualityCleanupStats(stats, &applied)
	}
	return stats, nil
}

func qualityEngineDeletionImpact(candidates qualityCleanupCandidates, activeTaskIDs map[int64]struct{}) (events.CleanupImpactData, error) {
	items := make([]events.CleanupImpactItem, 0, len(candidates.plans)*2+len(candidates.issues))

	for _, item := range candidates.plans {
		stableRef := fmt.Sprintf("quality_plan:%d", item.ID)
		items = append(items, events.CleanupImpactItem{StableRef: stableRef, Disposition: events.CleanupImpactRebindable})
		if _, active := activeTaskIDs[item.ID]; active {
			items = append(items, events.CleanupImpactItem{StableRef: stableRef, Disposition: events.CleanupImpactRunning})
		}
	}
	for _, item := range candidates.issues {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("quality_issue:%d", item.ID), Disposition: events.CleanupImpactWillDisable})
	}
	return events.BuildCleanupImpactData(items, "/quality/plans")
}

func (s *CleanupService) listActiveQualityCleanupTaskIDs(ctx context.Context, tasks []models.QualityPlan) (map[int64]struct{}, error) {
	activeTaskIDs := make(map[int64]struct{})
	if len(tasks) == 0 {
		return activeTaskIDs, nil
	}
	sourceTaskIDs := make([]string, 0, len(tasks))
	taskIDBySource := make(map[string]int64, len(tasks))
	for _, task := range tasks {
		sourceTaskID := strconv.FormatInt(task.ID, 10)
		sourceTaskIDs = append(sourceTaskIDs, sourceTaskID)
		taskIDBySource[sourceTaskID] = task.ID
	}
	var executions []commonExecution.TaskExecution
	if err := s.db.WithContext(ctx).Select("source_task_id").
		Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id IN ? AND status IN ?",
			tasks[0].TenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, sourceTaskIDs,
			[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
		Find(&executions).Error; err != nil {
		return nil, fmt.Errorf("inspect active quality executions failed: %w", err)
	}
	for _, execution := range executions {
		if execution.SourceTaskID == nil {
			continue
		}
		if taskID, ok := taskIDBySource[*execution.SourceTaskID]; ok {
			activeTaskIDs[taskID] = struct{}{}
		}
	}
	return activeTaskIDs, nil
}

type qualityCleanupCandidates struct {
	rules  []models.QualityRule
	plans  []models.QualityPlan
	issues []models.Issue
}

func (c qualityCleanupCandidates) stats() *QualityCleanupStats {
	return &QualityCleanupStats{
		Rules:  len(c.rules),
		Plans:  len(c.plans),
		Issues: len(c.issues),
	}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (qualityCleanupCandidates, error) {
	if tenantID == 0 {
		return qualityCleanupCandidates{}, fmt.Errorf("quality resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return qualityCleanupCandidates{}, fmt.Errorf("quality resource reclaim database is not configured")
	}
	engineID, hasEngineID := qualityCleanupContextInt64(cleanupContext, "engine_id")
	contextTenantID, hasContextTenantID := qualityCleanupContextInt64(cleanupContext, "tenant_id")
	if !hasEngineID {
		if hasContextTenantID && contextTenantID == int64(tenantID) {
			return s.listTenantCandidates(ctx, int64(tenantID))
		}
		return qualityCleanupCandidates{}, nil
	}
	if engineID <= 0 {
		return qualityCleanupCandidates{}, nil
	}
	return s.listEngineCandidates(ctx, int64(tenantID), engineID)
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID int64) (qualityCleanupCandidates, error) {
	var candidates qualityCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.rules).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.plans).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.issues).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) listEngineCandidates(ctx context.Context, tenantID int64, engineID int64) (qualityCleanupCandidates, error) {
	var candidates qualityCleanupCandidates
	var plans []models.QualityPlan
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&plans).Error; err != nil {
		return candidates, err
	}
	for _, plan := range plans {
		var bindings []PlanTableBinding
		if err := json.Unmarshal(plan.TableBindings, &bindings); err != nil {
			return candidates, err
		}
		for _, binding := range bindings {
			if binding.Locator == "" {
				continue
			}
			locator, err := resourcetree.ParseURI(binding.Locator)
			if err != nil {
				return candidates, err
			}
			if int64(locator.EngineID) == engineID {
				candidates.plans = append(candidates.plans, plan)
				break
			}
		}
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).Find(&candidates.issues).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) disableCandidates(ctx context.Context, candidates qualityCleanupCandidates) (QualityCleanupStats, error) {
	var applied QualityCleanupStats
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		candidates.sortByID()
		if err := lockQualityCleanupTasks(tx, candidates.plans); err != nil {
			return err
		}
		for _, item := range candidates.issues {
			if item.Status != "open" {
				applied.SkippedIssues++
				continue
			}
			now := time.Now().UTC()
			result := tx.Model(&models.Issue{}).Where("id=? AND tenant_id=? AND status='open' AND version=?", item.ID, item.TenantID, item.Version).Updates(map[string]interface{}{"version": gorm.Expr("version + 1"), "status": "ignored", "resolved_at": now, "resolved_by": nil, "resolution_note": "", "updated_at": now, "accepted_keys": nil, "accepted_count": 0, "pending_count": 0})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("issue changed during cleanup")
			}
			if err := tx.Create(&models.IssueAction{TenantID: item.TenantID, IssueID: item.ID, PlanID: item.PlanID, ExecutionID: item.LastExecutionID, Action: "ignored", CreatedAt: now}).Error; err != nil {
				return err
			}
			applied.IgnoredIssues++
		}
		return nil
	})
	if err != nil {
		return QualityCleanupStats{}, err
	}
	return applied, nil
}

func lockQualityCleanupTasks(tx *gorm.DB, tasks []models.QualityPlan) error {
	if len(tasks) == 0 {
		return nil
	}
	tenantID := tasks[0].TenantID
	sourceTaskIDs := make([]string, 0, len(tasks))
	for _, item := range tasks {
		if item.TenantID != tenantID {
			return fmt.Errorf("quality cleanup task candidates span multiple tenants")
		}
		var current models.QualityPlan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).First(&current).Error; err != nil {
			return fmt.Errorf("lock check task %d failed: %w", item.ID, err)
		}
		if current.Version != item.Version {
			return fmt.Errorf("plan changed during cleanup")
		}
		sourceTaskIDs = append(sourceTaskIDs, strconv.FormatInt(current.ID, 10))
	}
	var active commonExecution.TaskExecution
	result := tx.Select("source_task_id").
		Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id IN ? AND status IN ?",
			tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, sourceTaskIDs,
			[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
		Order("source_task_id ASC").Limit(1).Find(&active)
	if result.Error != nil {
		return fmt.Errorf("inspect active quality executions failed: %w", result.Error)
	}
	if result.RowsAffected > 0 && active.SourceTaskID != nil {
		return fmt.Errorf("quality check task %s has an active execution", *active.SourceTaskID)
	}
	return nil
}

func mergeQualityCleanupStats(target, applied *QualityCleanupStats) {
	if target == nil || applied == nil {
		return
	}
	target.IgnoredIssues += applied.IgnoredIssues
	target.SkippedIssues += applied.SkippedIssues
	target.DeletedPlans += applied.DeletedPlans
	target.DeletedIssues += applied.DeletedIssues
	target.DeletedRules += applied.DeletedRules
}

func (s *CleanupService) deleteCandidates(ctx context.Context, candidates qualityCleanupCandidates, tenantID int64) (QualityCleanupStats, error) {
	var applied QualityCleanupStats
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		candidates.sortByID()
		if err := lockQualityCleanupTasks(tx, candidates.plans); err != nil {
			return err
		}
		if tenantID > 0 {
			if err := tx.Where("tenant_id=?", tenantID).Delete(&models.IssueAction{}).Error; err != nil {
				return err
			}
		}
		for _, item := range candidates.issues {
			result := tx.Where("id=? AND tenant_id=?", item.ID, item.TenantID).Delete(&models.Issue{})
			if result.Error != nil {
				return result.Error
			}
			applied.DeletedIssues += int(result.RowsAffected)
		}
		for _, item := range candidates.plans {
			if err := tx.Where("tenant_id=? AND plan_id=?", item.TenantID, item.ID).Delete(&models.PlanCheckItem{}).Error; err != nil {
				return err
			}
			result := tx.Where("id=? AND tenant_id=? AND version=?", item.ID, item.TenantID, item.Version).Delete(&models.QualityPlan{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("plan changed during cleanup")
			}
			applied.DeletedPlans++
		}
		for _, item := range candidates.rules {
			var current models.QualityRule
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", item.TenantID, item.ID).First(&current).Error; err != nil {
				return err
			}
			if current.Version != item.Version {
				return fmt.Errorf("rule changed during cleanup")
			}
			var refs int64
			if err := tx.Model(&models.PlanCheckItem{}).Where("tenant_id=? AND rule_id=?", item.TenantID, item.ID).Count(&refs).Error; err != nil {
				return err
			}
			if refs > 0 {
				return fmt.Errorf("rule acquired new plan references during cleanup")
			}
			if err := tx.Where("tenant_id=? AND rule_id=?", item.TenantID, item.ID).Delete(&models.RuleRevision{}).Error; err != nil {
				return err
			}
			if err := tx.Delete(&current).Error; err != nil {
				return err
			}
			applied.DeletedRules++
		}
		return nil
	})
	if err != nil {
		return QualityCleanupStats{}, err
	}
	return applied, nil
}

func (c *qualityCleanupCandidates) sortByID() {
	if c == nil {
		return
	}
	sort.Slice(c.plans, func(i, j int) bool { return c.plans[i].ID < c.plans[j].ID })
	sort.Slice(c.rules, func(i, j int) bool { return c.rules[i].ID < c.rules[j].ID })
	sort.Slice(c.issues, func(i, j int) bool { return c.issues[i].ID < c.issues[j].ID })
}

func qualityCleanupContextInt64(cleanupContext map[string]interface{}, key string) (int64, bool) {
	if cleanupContext == nil {
		return 0, false
	}
	raw, ok := cleanupContext[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case int64:
		if value > 0 {
			return value, true
		}
	case int:
		if value > 0 {
			return int64(value), true
		}
	case uint:
		if value > 0 {
			return int64(value), true
		}
	case float64:
		if value > 0 {
			return int64(value), true
		}
	case json.Number:
		parsed, err := strconv.ParseInt(string(value), 10, 64)
		return parsed, err == nil && parsed > 0
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return parsed, err == nil && parsed > 0
	}
	return 0, false
}

func qualityScanSummary(stats *QualityCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	scanned := stats.Plans + stats.Issues + stats.Rules
	return events.CleanupResultSummary{
		ScannedItems: scanned,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    qualityRiskLevelForCount(scanned),
	}
}

func qualityExecuteSummary(stats *QualityCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.IgnoredIssues + stats.DeletedPlans + stats.DeletedIssues + stats.DeletedRules
	return events.CleanupResultSummary{
		AffectedRecords:         affected,
		DisabledTaskDefinitions: 0,
		SkippedItems:            stats.SkippedIssues,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               "low",
	}
}

func qualityRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func qualityIntPtr(value int) *int {
	return &value
}
