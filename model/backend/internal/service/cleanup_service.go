package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/addp/common/cleanup"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CleanupService struct {
	db      *gorm.DB
	runtime *cleanup.Runner
}

type ModelCleanupStats struct {
	DWLayers                 int      `json:"dw_layers"`
	Entities                 int      `json:"entities"`
	EntityAttributes         int      `json:"entity_attributes"`
	EntityRelations          int      `json:"entity_relations"`
	LogicalTables            int      `json:"logical_tables"`
	LogicalFields            int      `json:"logical_fields"`
	DimensionHierarchies     int      `json:"dimension_hierarchies"`
	DimensionHierarchyLevels int      `json:"dimension_hierarchy_levels"`
	TableRelations           int      `json:"table_relations"`
	MetricImplementations    int      `json:"metric_implementations"`
	DraftedEntities          int      `json:"drafted_entities,omitempty"`
	DraftedTables            int      `json:"drafted_tables,omitempty"`
	DeletedRecords           int      `json:"deleted_records,omitempty"`
	SkippedItems             int      `json:"skipped_items,omitempty"`
	Errors                   []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, systemClient *commonClient.SystemServiceClient) *CleanupService {
	s := &CleanupService{db: db}
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleModel, System: systemClient, Log: logger.With("component", "model_cleanup_service"), Handle: s.runCleanup}
	return s
}
func (s *CleanupService) Start(ctx context.Context) error { return s.runtime.Start(ctx) }
func (s *CleanupService) Stop()                           { s.runtime.Stop() }

func (s *CleanupService) runCleanup(ctx context.Context, run *cleanup.Run) error {
	return run.Effect(ctx, func(tx *gorm.DB) error {
		scoped := &CleanupService{db: tx}
		var stats *ModelCleanupStats
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
			return fmt.Errorf("model cleanup domain transaction failed")
		}
		summary := modelScanSummary(stats)
		if run.Input.Action == events.CleanupActionExecute {
			summary = modelExecuteSummary(stats)
		}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			return err
		}
		result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleModel, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: events.CleanupResultSuccess, Summary: summary, Impact: &impact}
		receipt, err := commonExecution.NewCleanupReceipt(run.Execution, result, "")
		if err != nil {
			return err
		}
		return cleanup.Complete(ctx, tx, run.Lease, receipt, modelCleanupCounters(stats))
	})
}

func modelCleanupCounters(s *ModelCleanupStats) map[string]int64 {
	return map[string]int64{"dw_layers": int64(s.DWLayers), "entities": int64(s.Entities), "entity_attributes": int64(s.EntityAttributes), "entity_relations": int64(s.EntityRelations), "logical_tables": int64(s.LogicalTables), "logical_fields": int64(s.LogicalFields), "dimension_hierarchies": int64(s.DimensionHierarchies), "dimension_hierarchy_levels": int64(s.DimensionHierarchyLevels), "table_relations": int64(s.TableRelations), "metric_implementations": int64(s.MetricImplementations), "drafted_entities": int64(s.DraftedEntities), "drafted_tables": int64(s.DraftedTables), "deleted_records": int64(s.DeletedRecords), "skipped_items": int64(s.SkippedItems), "error_count": int64(len(s.Errors))}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*ModelCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*ModelCleanupStats, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return nil, err
	}
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}

	stats := candidates.stats()
	switch cleanupMode {
	case events.CleanupModeLogical:
		s.logicalCleanup(ctx, candidates, stats)
	case events.CleanupModePhysical:
		s.physicalCleanup(ctx, candidates, stats)
	}
	return stats, nil
}

type modelCleanupCandidates struct {
	TenantID                 int64
	dwLayers                 []models.DWLayer
	entities                 []models.Entity
	entityAttributes         []models.EntityAttribute
	entityRelations          []models.EntityRelation
	logicalTables            []models.LogicalTable
	logicalFields            []models.LogicalField
	dimensionHierarchies     []models.DimensionHierarchy
	dimensionHierarchyLevels []models.DimensionHierarchyLevel
	tableRelations           []models.TableRelation
	metricImplementations    []models.MetricImplementation
}

func (c modelCleanupCandidates) stats() *ModelCleanupStats {
	return &ModelCleanupStats{
		DWLayers:                 len(c.dwLayers),
		Entities:                 len(c.entities),
		EntityAttributes:         len(c.entityAttributes),
		EntityRelations:          len(c.entityRelations),
		LogicalTables:            len(c.logicalTables),
		LogicalFields:            len(c.logicalFields),
		DimensionHierarchies:     len(c.dimensionHierarchies),
		DimensionHierarchyLevels: len(c.dimensionHierarchyLevels),
		TableRelations:           len(c.tableRelations),
		MetricImplementations:    len(c.metricImplementations),
	}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (modelCleanupCandidates, error) {
	if tenantID == 0 {
		return modelCleanupCandidates{}, fmt.Errorf("model resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return modelCleanupCandidates{}, fmt.Errorf("model resource reclaim database is not configured")
	}
	contextTenantID, hasContextTenantID := modelCleanupContextUint(cleanupContext, "tenant_id")
	if !hasContextTenantID || contextTenantID != tenantID {
		return modelCleanupCandidates{}, nil
	}
	return s.listTenantCandidates(ctx, int64(tenantID))
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID int64) (modelCleanupCandidates, error) {
	candidates := modelCleanupCandidates{TenantID: tenantID}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.dwLayers).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.entities).Error; err != nil {
		return candidates, err
	}
	entityIDs := make([]int64, 0, len(candidates.entities))
	for _, item := range candidates.entities {
		entityIDs = append(entityIDs, item.ID)
	}
	if len(entityIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("entity_id IN ?", entityIDs).Find(&candidates.entityAttributes).Error; err != nil {
			return candidates, err
		}
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.entityRelations).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.logicalTables).Error; err != nil {
		return candidates, err
	}
	tableIDs := make([]int64, 0, len(candidates.logicalTables))
	for _, item := range candidates.logicalTables {
		tableIDs = append(tableIDs, item.ID)
	}
	if len(tableIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("table_id IN ?", tableIDs).Find(&candidates.logicalFields).Error; err != nil {
			return candidates, err
		}
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND table_id IN ?", tenantID, tableIDs).Find(&candidates.dimensionHierarchies).Error; err != nil {
			return candidates, err
		}
	}
	hierarchyIDs := dimensionHierarchyIDs(candidates.dimensionHierarchies)
	if len(hierarchyIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("hierarchy_id IN ?", hierarchyIDs).Find(&candidates.dimensionHierarchyLevels).Error; err != nil {
			return candidates, err
		}
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.tableRelations).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.metricImplementations).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) logicalCleanup(ctx context.Context, candidates modelCleanupCandidates, stats *ModelCleanupStats) {
	var draftedEntities, draftedTables, skippedItems int
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var revision *models.EntityModelRevision
		var err error
		if len(candidates.entities) > 0 {
			revision, err = repository.LockEntityModelRevision(tx, candidates.TenantID)
			if err != nil {
				return err
			}
		}

		var entities []models.Entity
		if ids := entityIDs(candidates.entities); len(ids) > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("tenant_id = ? AND id IN ?", candidates.TenantID, ids).
				Order("id ASC").Find(&entities).Error; err != nil {
				return err
			}
		}
		for _, entity := range entities {
			if entity.Status == "draft" {
				skippedItems++
				continue
			}
			result := tx.Model(&models.Entity{}).
				Where("id = ? AND tenant_id = ? AND version = ?", entity.ID, candidates.TenantID, entity.Version).
				Updates(map[string]interface{}{"status": "draft", "version": gorm.Expr("version + 1")})
			if result.Error != nil {
				return fmt.Errorf("draft entity %d failed: %w", entity.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("draft entity %d failed: resource changed while locked", entity.ID)
			}
			draftedEntities++
		}
		if draftedEntities > 0 {
			if _, err := repository.AdvanceEntityModelRevision(tx, candidates.TenantID, revision.Revision); err != nil {
				return err
			}
		}

		var tables []models.LogicalTable
		if ids := logicalTableIDs(candidates.logicalTables); len(ids) > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("tenant_id = ? AND id IN ?", candidates.TenantID, ids).
				Order("id ASC").Find(&tables).Error; err != nil {
				return err
			}
		}
		for _, table := range tables {
			if table.Status == "draft" {
				skippedItems++
				continue
			}
			result := tx.Model(&models.LogicalTable{}).
				Where("id = ? AND tenant_id = ? AND version = ?", table.ID, candidates.TenantID, table.Version).
				Updates(map[string]interface{}{"status": "draft", "version": gorm.Expr("version + 1")})
			if result.Error != nil {
				return fmt.Errorf("draft logical table %d failed: %w", table.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("draft logical table %d failed: resource changed while locked", table.ID)
			}
			draftedTables++
		}
		return nil
	})
	if err != nil {
		stats.Errors = append(stats.Errors, err.Error())
		return
	}
	stats.DraftedEntities += draftedEntities
	stats.DraftedTables += draftedTables
	stats.SkippedItems += skippedItems
}

func (s *CleanupService) physicalCleanup(ctx context.Context, candidates modelCleanupCandidates, stats *ModelCleanupStats) {
	batches := []struct {
		model        interface{}
		ids          []int64
		name         string
		tenantScoped bool
	}{
		{model: &models.MetricImplementation{}, ids: metricImplementationIDs(candidates.metricImplementations), name: "metric implementations", tenantScoped: true},
		{model: &models.DimensionHierarchyLevel{}, ids: dimensionHierarchyLevelIDs(candidates.dimensionHierarchyLevels), name: "dimension hierarchy levels"},
		{model: &models.DimensionHierarchy{}, ids: dimensionHierarchyIDs(candidates.dimensionHierarchies), name: "dimension hierarchies", tenantScoped: true},
		{model: &models.TableRelation{}, ids: tableRelationIDs(candidates.tableRelations), name: "table relations", tenantScoped: true},
		{model: &models.LogicalField{}, ids: logicalFieldIDs(candidates.logicalFields), name: "logical fields"},
		{model: &models.LogicalTable{}, ids: logicalTableIDs(candidates.logicalTables), name: "logical tables", tenantScoped: true},
		{model: &models.EntityRelation{}, ids: entityRelationIDs(candidates.entityRelations), name: "entity relations", tenantScoped: true},
		{model: &models.EntityAttribute{}, ids: entityAttributeIDs(candidates.entityAttributes), name: "entity attributes"},
		{model: &models.Entity{}, ids: entityIDs(candidates.entities), name: "entities", tenantScoped: true},
		{model: &models.DWLayer{}, ids: dwLayerIDs(candidates.dwLayers), name: "dw layers", tenantScoped: true},
	}
	deletedRecords := 0
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		entityModelChanged := len(candidates.entities) > 0 || len(candidates.entityAttributes) > 0 || len(candidates.entityRelations) > 0
		var revision *models.EntityModelRevision
		var err error
		if entityModelChanged {
			revision, err = repository.LockEntityModelRevision(tx, candidates.TenantID)
			if err != nil {
				return err
			}
		}
		if err := lockCleanupRoots(tx, candidates); err != nil {
			return err
		}
		for _, batch := range batches {
			if len(batch.ids) == 0 {
				continue
			}
			query := tx.Unscoped()
			if batch.tenantScoped {
				query = query.Where("tenant_id = ?", candidates.TenantID)
			}
			result := query.Delete(batch.model, batch.ids)
			if result.Error != nil {
				return fmt.Errorf("delete %s failed: %w", batch.name, result.Error)
			}
			deletedRecords += int(result.RowsAffected)
		}
		if entityModelChanged {
			if _, err := repository.AdvanceEntityModelRevision(tx, candidates.TenantID, revision.Revision); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		stats.Errors = append(stats.Errors, err.Error())
		return
	}
	stats.DeletedRecords += deletedRecords
}

func lockCleanupRoots(tx *gorm.DB, candidates modelCleanupCandidates) error {
	locks := []struct {
		model interface{}
		ids   []int64
		name  string
	}{
		{model: &[]models.Entity{}, ids: entityIDs(candidates.entities), name: "entities"},
		{model: &[]models.EntityRelation{}, ids: entityRelationIDs(candidates.entityRelations), name: "entity relations"},
		{model: &[]models.LogicalTable{}, ids: logicalTableIDs(candidates.logicalTables), name: "logical tables"},
		{model: &[]models.DWLayer{}, ids: dwLayerIDs(candidates.dwLayers), name: "dw layers"},
	}
	for _, lock := range locks {
		if len(lock.ids) == 0 {
			continue
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id IN ?", candidates.TenantID, lock.ids).
			Order("id ASC").Find(lock.model).Error; err != nil {
			return fmt.Errorf("lock %s failed: %w", lock.name, err)
		}
	}
	return nil
}

func modelCleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
	if cleanupContext == nil {
		return 0, false
	}
	raw, ok := cleanupContext[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case uint:
		return value, value > 0
	case int:
		if value > 0 {
			return uint(value), true
		}
	case int64:
		if value > 0 {
			return uint(value), true
		}
	case float64:
		if value > 0 {
			return uint(value), true
		}
	case json.Number:
		parsed, err := strconv.ParseUint(string(value), 10, 32)
		return uint(parsed), err == nil && parsed > 0
	case string:
		parsed, err := strconv.ParseUint(value, 10, 32)
		return uint(parsed), err == nil && parsed > 0
	}
	return 0, false
}

func modelScanSummary(stats *ModelCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	count := modelCandidateRecordCount(stats)
	return events.CleanupResultSummary{
		ScannedItems: count,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    modelRiskLevelForCount(count),
	}
}

func modelExecuteSummary(stats *ModelCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.DraftedEntities + stats.DraftedTables + stats.DeletedRecords
	return events.CleanupResultSummary{
		AffectedRecords: affected,
		MarkedOutdated:  stats.DraftedEntities + stats.DraftedTables,
		SkippedItems:    stats.SkippedItems,
		ErrorCount:      len(stats.Errors),
		RiskLevel:       "low",
	}
}

func modelCandidateRecordCount(stats *ModelCleanupStats) int {
	return stats.DWLayers +
		stats.Entities +
		stats.EntityAttributes +
		stats.EntityRelations +
		stats.LogicalTables +
		stats.LogicalFields +
		stats.DimensionHierarchies +
		stats.DimensionHierarchyLevels +
		stats.TableRelations +
		stats.MetricImplementations
}

func modelRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func modelIntPtr(value int) *int {
	return &value
}

func dwLayerIDs(items []models.DWLayer) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func entityIDs(items []models.Entity) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func entityAttributeIDs(items []models.EntityAttribute) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func entityRelationIDs(items []models.EntityRelation) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func logicalTableIDs(items []models.LogicalTable) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func logicalFieldIDs(items []models.LogicalField) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func dimensionHierarchyIDs(items []models.DimensionHierarchy) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func dimensionHierarchyLevelIDs(items []models.DimensionHierarchyLevel) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func tableRelationIDs(items []models.TableRelation) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func metricImplementationIDs(items []models.MetricImplementation) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
