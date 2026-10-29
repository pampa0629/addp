package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/addp/common/cleanup"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
)

type CleanupService struct {
	db      *gorm.DB
	runtime *cleanup.Runner
}

type GraphCleanupStats struct {
	Ontologies          int      `json:"ontologies"`
	EntityTypes         int      `json:"entity_types"`
	RelationTypes       int      `json:"relation_types"`
	OntologyVersions    int      `json:"ontology_versions"`
	KnowledgeGraphs     int      `json:"knowledge_graphs"`
	BuildTasks          int      `json:"build_tasks"`
	BuildMaterials      int      `json:"build_materials"`
	ReviewItems         int      `json:"review_items"`
	ArchivedOntologies  int      `json:"archived_ontologies,omitempty"`
	ArchivedGraphs      int      `json:"archived_graphs,omitempty"`
	CancelledBuildTasks int      `json:"cancelled_build_tasks,omitempty"`
	DeletedRecords      int      `json:"deleted_records,omitempty"`
	SkippedItems        int      `json:"skipped_items,omitempty"`
	Errors              []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, systemClient *commonClient.SystemServiceClient) *CleanupService {
	s := &CleanupService{db: db}
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleGraph, System: systemClient, Log: logger.With("component", "graph_cleanup"), Handle: s.runCleanup}
	return s
}
func (s *CleanupService) Start(ctx context.Context) error { return s.runtime.Start(ctx) }
func (s *CleanupService) Stop()                           { s.runtime.Stop() }

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*GraphCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) applyCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*GraphCleanupStats, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return nil, err
	}
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}

	stats := candidates.stats()
	if _, hasEngineID := graphCleanupContextUint(cleanupContext, "engine_id"); hasEngineID {
		for _, task := range candidates.buildTasks {
			if task.Status == models.BuildStatusRunning {
				stats.Errors = append(stats.Errors, fmt.Sprintf("graph build task %d is running", task.ID))
			}
		}
		if len(stats.Errors) > 0 {
			return stats, nil
		}
		s.logicalCleanup(ctx, candidates, stats)
		return stats, nil
	}
	switch cleanupMode {
	case events.CleanupModeLogical:
		s.logicalCleanup(ctx, candidates, stats)
	case events.CleanupModePhysical:
		s.physicalCleanup(ctx, candidates, stats)
	}
	return stats, nil
}

func graphEngineDeletionImpact(candidates graphCleanupCandidates) (events.CleanupImpactData, error) {
	items := make([]events.CleanupImpactItem, 0,
		len(candidates.ontologies)+len(candidates.entityTypes)+len(candidates.relationTypes)+len(candidates.ontologyVersions)+
			len(candidates.knowledgeGraphs)+len(candidates.buildTasks)*2+len(candidates.buildMaterials)+len(candidates.reviewItems))
	appendItems := func(prefix string, ids []uint) {
		for _, id := range ids {
			items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("%s:%d", prefix, id), Disposition: events.CleanupImpactWillDisable})
		}
	}
	appendItems("ontology", ontologyIDs(candidates.ontologies))
	appendItems("entity_type", entityTypeIDs(candidates.entityTypes))
	appendItems("relation_type", relationTypeIDs(candidates.relationTypes))
	appendItems("ontology_version", ontologyVersionIDs(candidates.ontologyVersions))
	appendItems("knowledge_graph", knowledgeGraphIDs(candidates.knowledgeGraphs))
	appendItems("graph_build_task", buildTaskIDs(candidates.buildTasks))
	appendItems("graph_build_material", buildMaterialIDs(candidates.buildMaterials))
	appendItems("graph_review_item", reviewItemIDs(candidates.reviewItems))
	for _, task := range candidates.buildTasks {
		if task.Status == models.BuildStatusRunning {
			items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("graph_build_task:%d", task.ID), Disposition: events.CleanupImpactRunning})
		}
	}
	return events.BuildCleanupImpactData(items, "/graph/graphs")
}

type graphCleanupCandidates struct {
	ontologies       []models.Ontology
	entityTypes      []models.EntityType
	relationTypes    []models.RelationType
	ontologyVersions []models.OntologyVersion
	knowledgeGraphs  []models.KnowledgeGraph
	buildTasks       []models.BuildTask
	buildMaterials   []models.BuildMaterial
	reviewItems      []models.ReviewItem
}

func (c graphCleanupCandidates) stats() *GraphCleanupStats {
	return &GraphCleanupStats{
		Ontologies:       len(c.ontologies),
		EntityTypes:      len(c.entityTypes),
		RelationTypes:    len(c.relationTypes),
		OntologyVersions: len(c.ontologyVersions),
		KnowledgeGraphs:  len(c.knowledgeGraphs),
		BuildTasks:       len(c.buildTasks),
		BuildMaterials:   len(c.buildMaterials),
		ReviewItems:      len(c.reviewItems),
	}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (graphCleanupCandidates, error) {
	if tenantID == 0 {
		return graphCleanupCandidates{}, fmt.Errorf("graph resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return graphCleanupCandidates{}, fmt.Errorf("graph resource reclaim database is not configured")
	}
	engineID, hasEngineID := graphCleanupContextUint(cleanupContext, "engine_id")
	contextTenantID, hasContextTenantID := graphCleanupContextUint(cleanupContext, "tenant_id")
	if !hasEngineID {
		if hasContextTenantID && contextTenantID == tenantID {
			return s.listTenantCandidates(ctx, tenantID)
		}
		return graphCleanupCandidates{}, nil
	}
	if engineID == 0 {
		return graphCleanupCandidates{}, nil
	}
	return s.listEngineCandidates(ctx, tenantID, engineID)
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID uint) (graphCleanupCandidates, error) {
	var candidates graphCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.ontologies).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.entityTypes).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.relationTypes).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.ontologyVersions).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.knowledgeGraphs).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.buildTasks).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.buildMaterials).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.reviewItems).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) listEngineCandidates(ctx context.Context, tenantID uint, engineID uint) (graphCleanupCandidates, error) {
	var candidates graphCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).Find(&candidates.knowledgeGraphs).Error; err != nil {
		return candidates, err
	}
	if len(candidates.knowledgeGraphs) == 0 {
		return candidates, nil
	}
	graphIDs := make([]uint, 0, len(candidates.knowledgeGraphs))
	for _, graph := range candidates.knowledgeGraphs {
		graphIDs = append(graphIDs, graph.ID)
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND graph_id IN ?", tenantID, graphIDs).Find(&candidates.buildTasks).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND graph_id IN ?", tenantID, graphIDs).Find(&candidates.buildMaterials).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND graph_id IN ?", tenantID, graphIDs).Find(&candidates.reviewItems).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) logicalCleanup(ctx context.Context, candidates graphCleanupCandidates, stats *GraphCleanupStats) {
	for _, ontology := range candidates.ontologies {
		if ontology.Status == "archived" {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.Ontology{}).Where("id = ?", ontology.ID).Update("status", "archived").Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("archive ontology %d failed: %v", ontology.ID, err))
			continue
		}
		stats.ArchivedOntologies++
	}
	for _, graph := range candidates.knowledgeGraphs {
		if graph.Status == "archived" {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.KnowledgeGraph{}).Where("id = ?", graph.ID).Update("status", "archived").Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("archive knowledge graph %d failed: %v", graph.ID, err))
			continue
		}
		stats.ArchivedGraphs++
	}
	// Build execution cancellation is exclusively performed by the three-phase
	// protocol. A definition without an execution has no runtime to cancel.

}

func (s *CleanupService) physicalCleanup(ctx context.Context, candidates graphCleanupCandidates, stats *GraphCleanupStats) {
	if len(candidates.reviewItems) > 0 {
		if err := s.deleteByIDs(ctx, &models.ReviewItem{}, reviewItemIDs(candidates.reviewItems)); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete review items failed: %v", err))
		} else {
			stats.DeletedRecords += len(candidates.reviewItems)
		}
	}
	if len(candidates.buildMaterials) > 0 {
		if err := s.deleteByIDs(ctx, &models.BuildMaterial{}, buildMaterialIDs(candidates.buildMaterials)); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete build materials failed: %v", err))
		} else {
			stats.DeletedRecords += len(candidates.buildMaterials)
		}
	}
	if len(candidates.buildTasks) > 0 {
		if err := s.deleteByIDs(ctx, &models.BuildTask{}, buildTaskIDs(candidates.buildTasks)); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete build tasks failed: %v", err))
		} else {
			stats.DeletedRecords += len(candidates.buildTasks)
		}
	}
	if len(candidates.knowledgeGraphs) > 0 {
		if err := s.deleteByIDs(ctx, &models.KnowledgeGraph{}, knowledgeGraphIDs(candidates.knowledgeGraphs)); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete knowledge graphs failed: %v", err))
		} else {
			stats.DeletedRecords += len(candidates.knowledgeGraphs)
		}
	}
	if len(candidates.ontologyVersions) > 0 {
		if err := s.deleteByIDs(ctx, &models.OntologyVersion{}, ontologyVersionIDs(candidates.ontologyVersions)); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete ontology versions failed: %v", err))
		} else {
			stats.DeletedRecords += len(candidates.ontologyVersions)
		}
	}
	if len(candidates.relationTypes) > 0 {
		if err := s.deleteByIDs(ctx, &models.RelationType{}, relationTypeIDs(candidates.relationTypes)); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete relation types failed: %v", err))
		} else {
			stats.DeletedRecords += len(candidates.relationTypes)
		}
	}
	if len(candidates.entityTypes) > 0 {
		if err := s.deleteByIDs(ctx, &models.EntityType{}, entityTypeIDs(candidates.entityTypes)); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete entity types failed: %v", err))
		} else {
			stats.DeletedRecords += len(candidates.entityTypes)
		}
	}
	if len(candidates.ontologies) > 0 {
		if err := s.deleteByIDs(ctx, &models.Ontology{}, ontologyIDs(candidates.ontologies)); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete ontologies failed: %v", err))
		} else {
			stats.DeletedRecords += len(candidates.ontologies)
		}
	}
}

func (s *CleanupService) deleteByIDs(ctx context.Context, model interface{}, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Unscoped().Delete(model, ids).Error
}

func isCompletedGraphBuildStatus(status string) bool {
	switch status {
	case models.BuildStatusSuccess, models.BuildStatusFailed, models.BuildStatusTimeout, models.BuildStatusCancelled:
		return true
	default:
		return false
	}
}

func graphCleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
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

func graphScanSummary(stats *GraphCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	return events.CleanupResultSummary{
		ScannedItems: graphCandidateRecordCount(stats),
		ErrorCount:   len(stats.Errors),
		RiskLevel:    graphRiskLevelForCount(graphCandidateRecordCount(stats)),
	}
}

func graphExecuteSummary(stats *GraphCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.ArchivedOntologies + stats.ArchivedGraphs + stats.CancelledBuildTasks + stats.DeletedRecords
	return events.CleanupResultSummary{
		AffectedRecords:         affected,
		DisabledTaskDefinitions: stats.CancelledBuildTasks,
		SkippedItems:            stats.SkippedItems,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               "low",
	}
}

func graphCandidateRecordCount(stats *GraphCleanupStats) int {
	return stats.Ontologies +
		stats.EntityTypes +
		stats.RelationTypes +
		stats.OntologyVersions +
		stats.KnowledgeGraphs +
		stats.BuildTasks +
		stats.BuildMaterials +
		stats.ReviewItems
}

func graphRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func graphIntPtr(value int) *int {
	return &value
}

func ontologyIDs(items []models.Ontology) []uint {
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func entityTypeIDs(items []models.EntityType) []uint {
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func relationTypeIDs(items []models.RelationType) []uint {
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func ontologyVersionIDs(items []models.OntologyVersion) []uint {
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func knowledgeGraphIDs(items []models.KnowledgeGraph) []uint {
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func buildTaskIDs(items []models.BuildTask) []uint {
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func buildMaterialIDs(items []models.BuildMaterial) []uint {
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func reviewItemIDs(items []models.ReviewItem) []uint {
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func graphCleanupCounters(s *GraphCleanupStats) map[string]int64 {
	return map[string]int64{"ontologies": int64(s.Ontologies), "entity_types": int64(s.EntityTypes), "relation_types": int64(s.RelationTypes), "ontology_versions": int64(s.OntologyVersions), "knowledge_graphs": int64(s.KnowledgeGraphs), "build_tasks": int64(s.BuildTasks), "build_materials": int64(s.BuildMaterials), "review_items": int64(s.ReviewItems), "archived_ontologies": int64(s.ArchivedOntologies), "archived_graphs": int64(s.ArchivedGraphs), "cancelled_build_tasks": int64(s.CancelledBuildTasks), "deleted_records": int64(s.DeletedRecords), "skipped_items": int64(s.SkippedItems), "error_count": int64(len(s.Errors))}
}
