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
	"github.com/addp/standard/internal/models"
	minio "github.com/minio/minio-go/v7"
	"gorm.io/gorm"
)

type CleanupService struct {
	db          *gorm.DB
	runtime     *cleanup.Runner
	minioClient *minio.Client
}

type StandardCleanupStats struct {
	Domains                  int      `json:"domains"`
	Glossaries               int      `json:"glossaries"`
	GlossaryElementMappings  int      `json:"glossary_element_mappings"`
	Elements                 int      `json:"elements"`
	CodeSets                 int      `json:"code_sets"`
	CodeItems                int      `json:"code_items"`
	MeasurementCategories    int      `json:"measurement_categories"`
	Units                    int      `json:"units"`
	MetricCategories         int      `json:"metric_categories"`
	Metrics                  int      `json:"metrics"`
	MetricRevisions          int      `json:"metric_revisions"`
	MetricDependencies       int      `json:"metric_dependencies"`
	Documents                int      `json:"documents"`
	DocumentElementMappings  int      `json:"document_element_mappings"`
	DocumentGlossaryMappings int      `json:"document_glossary_mappings"`
	DocumentMetricMappings   int      `json:"document_metric_mappings"`
	ReferenceDeletions       int      `json:"reference_deletions"`
	DeprecatedGlossaries     int      `json:"deprecated_glossaries,omitempty"`
	DeprecatedElements       int      `json:"deprecated_elements,omitempty"`
	DeprecatedMetrics        int      `json:"deprecated_metrics,omitempty"`
	DeprecatedDocuments      int      `json:"deprecated_documents,omitempty"`
	DeletedRecords           int      `json:"deleted_records,omitempty"`
	DeletedPhysicalArtifacts int      `json:"deleted_physical_artifacts,omitempty"`
	FreedBytes               int64    `json:"freed_bytes,omitempty"`
	SkippedItems             int      `json:"skipped_items,omitempty"`
	Errors                   []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, systemClient *commonClient.SystemServiceClient, minioClient *minio.Client) *CleanupService {
	s := &CleanupService{db: db, minioClient: minioClient}
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleStandard, System: systemClient, Log: logger.With("component", "standard_cleanup_service"), Handle: s.runCleanup}
	return s
}
func (s *CleanupService) Start(ctx context.Context) error { return s.runtime.Start(ctx) }
func (s *CleanupService) Stop()                           { s.runtime.Stop() }
func (s *CleanupService) runCleanup(ctx context.Context, run *cleanup.Run) error {
	// Historical Tenant cleanup has no approved execution authority. Never enter
	// its document/MinIO path through the active-Tenant User runtime.
	if run.Input.Context.TenantID != 0 {
		return fmt.Errorf("historical tenant cleanup is unsupported")
	}
	return run.Effect(ctx, func(tx *gorm.DB) error {
		scoped := &CleanupService{db: tx, minioClient: s.minioClient}
		var stats *StandardCleanupStats
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
			return fmt.Errorf("standard cleanup domain transaction failed")
		}
		summary := standardScanSummary(stats)
		if run.Input.Action == events.CleanupActionExecute {
			summary = standardExecuteSummary(stats)
		}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			return err
		}
		result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleStandard, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: events.CleanupResultSuccess, Summary: summary, Impact: &impact}
		receipt, err := commonExecution.NewCleanupReceipt(run.Execution, result, "")
		if err != nil {
			return err
		}
		return cleanup.Complete(ctx, tx, run.Lease, receipt, standardCleanupCounters(stats))
	})
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*StandardCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*StandardCleanupStats, error) {
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

type standardCleanupCandidates struct {
	domains                  []models.Domain
	glossaries               []models.Glossary
	glossaryElementMappings  []models.GlossaryElementMapping
	elements                 []models.Element
	codeSets                 []models.CodeSet
	codeSetRevisions         []models.CodeSetRevision
	codeItems                []models.CodeSetRevisionItem
	measurementCategories    []models.MeasurementCategory
	units                    []models.Unit
	metricCategories         []models.MetricCategory
	metrics                  []models.MetricDefinition
	metricRevisions          []models.MetricDefinitionRevision
	metricDependencies       []models.MetricDefinitionRevisionDependency
	documents                []models.Document
	documentRevisions        []models.DocumentRevision
	documentElementMappings  []models.DocumentElementMapping
	documentGlossaryMappings []models.DocumentGlossaryMapping
	documentMetricMappings   []models.DocumentMetricMapping
	referenceDeletions       []models.StandardReferenceDeletion
}

func (c standardCleanupCandidates) stats() *StandardCleanupStats {
	return &StandardCleanupStats{
		Domains:                  len(c.domains),
		Glossaries:               len(c.glossaries),
		GlossaryElementMappings:  len(c.glossaryElementMappings),
		Elements:                 len(c.elements),
		CodeSets:                 len(c.codeSets),
		CodeItems:                len(c.codeItems),
		MeasurementCategories:    len(c.measurementCategories),
		Units:                    len(c.units),
		MetricCategories:         len(c.metricCategories),
		Metrics:                  len(c.metrics),
		MetricRevisions:          len(c.metricRevisions),
		MetricDependencies:       len(c.metricDependencies),
		Documents:                len(c.documents),
		DocumentElementMappings:  len(c.documentElementMappings),
		DocumentGlossaryMappings: len(c.documentGlossaryMappings),
		DocumentMetricMappings:   len(c.documentMetricMappings),
		ReferenceDeletions:       len(c.referenceDeletions),
	}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (standardCleanupCandidates, error) {
	if tenantID == 0 {
		return standardCleanupCandidates{}, fmt.Errorf("standard resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return standardCleanupCandidates{}, fmt.Errorf("standard resource reclaim database is not configured")
	}
	contextTenantID, hasContextTenantID := standardCleanupContextUint(cleanupContext, "tenant_id")
	if !hasContextTenantID || contextTenantID != tenantID {
		return standardCleanupCandidates{}, nil
	}
	return s.listTenantCandidates(ctx, int64(tenantID))
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID int64) (standardCleanupCandidates, error) {
	var candidates standardCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.domains).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.glossaries).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.elements).Error; err != nil {
		return candidates, err
	}

	glossaryIDs := standardGlossaryIDs(candidates.glossaries)
	elementIDs := standardElementIDs(candidates.elements)
	if len(glossaryIDs) > 0 || len(elementIDs) > 0 {
		query := s.db.WithContext(ctx)
		if len(glossaryIDs) > 0 && len(elementIDs) > 0 {
			query = query.Where("glossary_id IN ? OR element_id IN ?", glossaryIDs, elementIDs)
		} else if len(glossaryIDs) > 0 {
			query = query.Where("glossary_id IN ?", glossaryIDs)
		} else {
			query = query.Where("element_id IN ?", elementIDs)
		}
		if err := query.Find(&candidates.glossaryElementMappings).Error; err != nil {
			return candidates, err
		}
	}

	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.codeSets).Error; err != nil {
		return candidates, err
	}
	codeSetIDs := standardCodeSetIDs(candidates.codeSets)
	if len(codeSetIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("code_set_id IN ?", codeSetIDs).Find(&candidates.codeSetRevisions).Error; err != nil {
			return candidates, err
		}
		revisionIDs := standardCodeSetRevisionIDs(candidates.codeSetRevisions)
		if len(revisionIDs) > 0 {
			if err := s.db.WithContext(ctx).Where("code_set_revision_id IN ?", revisionIDs).Find(&candidates.codeItems).Error; err != nil {
				return candidates, err
			}
		}
	}

	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.measurementCategories).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.units).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.metricCategories).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.metrics).Error; err != nil {
		return candidates, err
	}
	metricIDs := standardMetricIDs(candidates.metrics)
	if len(metricIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("metric_definition_id IN ?", metricIDs).Find(&candidates.metricRevisions).Error; err != nil {
			return candidates, err
		}
		revisionIDs := standardMetricRevisionIDs(candidates.metricRevisions)
		if len(revisionIDs) > 0 {
			if err := s.db.WithContext(ctx).Where("metric_definition_revision_id IN ?", revisionIDs).Find(&candidates.metricDependencies).Error; err != nil {
				return candidates, err
			}
		}
	}

	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.documents).Error; err != nil {
		return candidates, err
	}
	documentIDs := standardDocumentIDs(candidates.documents)
	if len(documentIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("document_id IN ?", documentIDs).Find(&candidates.documentRevisions).Error; err != nil {
			return candidates, err
		}
		if err := s.db.WithContext(ctx).Where("document_id IN ?", documentIDs).Find(&candidates.documentElementMappings).Error; err != nil {
			return candidates, err
		}
		if err := s.db.WithContext(ctx).Where("document_id IN ?", documentIDs).Find(&candidates.documentGlossaryMappings).Error; err != nil {
			return candidates, err
		}
		if err := s.db.WithContext(ctx).Where("document_id IN ?", documentIDs).Find(&candidates.documentMetricMappings).Error; err != nil {
			return candidates, err
		}
	}

	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.referenceDeletions).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) logicalCleanup(ctx context.Context, candidates standardCleanupCandidates, stats *StandardCleanupStats) {
	for _, glossary := range candidates.glossaries {
		var publishedCount int64
		if err := s.db.WithContext(ctx).Model(&models.GlossaryRevision{}).Where("glossary_id = ? AND status = ?", glossary.ID, models.RevisionStatusPublished).Count(&publishedCount).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("inspect glossary %d revisions failed: %v", glossary.ID, err))
			continue
		}
		if publishedCount == 0 {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.GlossaryRevision{}).Where("glossary_id = ? AND status = ?", glossary.ID, models.RevisionStatusPublished).
			Update("status", models.RevisionStatusWithdrawn).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("withdraw glossary %d revisions failed: %v", glossary.ID, err))
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.Glossary{}).Where("id = ?", glossary.ID).
			Update("version", gorm.Expr("version + 1")).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("bump glossary %d version failed: %v", glossary.ID, err))
			continue
		}
		stats.DeprecatedGlossaries++
	}
	for _, element := range candidates.elements {
		var publishedCount int64
		if err := s.db.WithContext(ctx).Model(&models.ElementRevision{}).Where("element_id = ? AND status = ?", element.ID, models.RevisionStatusPublished).Count(&publishedCount).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("inspect element %d revisions failed: %v", element.ID, err))
			continue
		}
		if publishedCount == 0 {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.ElementRevision{}).Where("element_id = ? AND status = ?", element.ID, models.RevisionStatusPublished).Update("status", models.RevisionStatusWithdrawn).Error; err != nil {
				return err
			}
			return tx.Model(&models.Element{}).Where("id = ?", element.ID).Update("version", gorm.Expr("version + 1")).Error
		}); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("deprecate element %d failed: %v", element.ID, err))
			continue
		}
		stats.DeprecatedElements++
	}
	for _, metric := range candidates.metrics {
		var publishedCount int64
		if err := s.db.WithContext(ctx).Model(&models.MetricDefinitionRevision{}).Where("metric_definition_id = ? AND status = ?", metric.ID, models.RevisionStatusPublished).Count(&publishedCount).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("inspect metric %d revisions failed: %v", metric.ID, err))
			continue
		}
		if publishedCount == 0 {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.MetricDefinitionRevision{}).Where("metric_definition_id = ? AND status = ?", metric.ID, models.RevisionStatusPublished).Update("status", models.RevisionStatusWithdrawn).Error; err != nil {
				return err
			}
			return tx.Model(&models.MetricDefinition{}).Where("id = ?", metric.ID).Update("version", gorm.Expr("version + 1")).Error
		}); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("deprecate metric %d failed: %v", metric.ID, err))
			continue
		}
		stats.DeprecatedMetrics++
	}
	for _, document := range candidates.documents {
		var publishedCount int64
		if err := s.db.WithContext(ctx).Model(&models.DocumentRevision{}).Where("document_id = ? AND status = ?", document.ID, models.RevisionStatusPublished).Count(&publishedCount).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("inspect document %d revisions failed: %v", document.ID, err))
			continue
		}
		if publishedCount == 0 {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.DocumentRevision{}).Where("document_id = ? AND status = ?", document.ID, models.RevisionStatusPublished).Update("status", models.RevisionStatusWithdrawn).Error; err != nil {
				return err
			}
			return tx.Model(&models.Document{}).Where("id = ?", document.ID).Update("version", gorm.Expr("version + 1")).Error
		}); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("withdraw document %d revisions failed: %v", document.ID, err))
			continue
		}
		stats.DeprecatedDocuments++
	}
}

func (s *CleanupService) physicalCleanup(ctx context.Context, candidates standardCleanupCandidates, stats *StandardCleanupStats) {
	blockedDocumentIDs := s.deleteStandardDocumentFiles(ctx, candidates.documentRevisions, stats)
	documentsToDelete := standardDocumentsExcept(candidates.documents, blockedDocumentIDs)
	documentElementMappingsToDelete := standardDocumentElementMappingsForDocuments(candidates.documentElementMappings, documentsToDelete)
	documentGlossaryMappingsToDelete := standardDocumentGlossaryMappingsForDocuments(candidates.documentGlossaryMappings, documentsToDelete)
	documentMetricMappingsToDelete := standardDocumentMetricMappingsForDocuments(candidates.documentMetricMappings, documentsToDelete)
	if len(candidates.glossaryElementMappings) > 0 {
		deleted, err := s.deleteGlossaryElementMappings(ctx, candidates)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete glossary element mappings failed: %v", err))
		} else {
			stats.DeletedRecords += deleted
		}
	}

	for _, batch := range []struct {
		model interface{}
		ids   []int64
		name  string
	}{
		{model: &models.StandardReferenceDeletion{}, ids: standardReferenceDeletionIDs(candidates.referenceDeletions), name: "reference deletions"},
		{model: &models.DocumentElementMapping{}, ids: standardDocumentElementMappingIDs(documentElementMappingsToDelete), name: "document element mappings"},
		{model: &models.DocumentGlossaryMapping{}, ids: standardDocumentGlossaryMappingIDs(documentGlossaryMappingsToDelete), name: "document glossary mappings"},
		{model: &models.DocumentMetricMapping{}, ids: standardDocumentMetricMappingIDs(documentMetricMappingsToDelete), name: "document metric mappings"},
		{model: &models.MetricDefinitionRevisionDependency{}, ids: standardMetricDependencyIDs(candidates.metricDependencies), name: "metric revision dependencies"},
		{model: &models.MetricDefinitionRevision{}, ids: standardMetricRevisionIDs(candidates.metricRevisions), name: "metric revisions"},
		{model: &models.CodeSetRevisionItem{}, ids: standardCodeItemIDs(candidates.codeItems), name: "code items"},
		{model: &models.Unit{}, ids: standardUnitIDs(candidates.units), name: "units"},
		{model: &models.Document{}, ids: standardDocumentIDs(documentsToDelete), name: "documents"},
		{model: &models.MetricDefinition{}, ids: standardMetricIDs(candidates.metrics), name: "metrics"},
		{model: &models.MetricCategory{}, ids: standardMetricCategoryIDs(candidates.metricCategories), name: "metric categories"},
		{model: &models.Element{}, ids: standardElementIDs(candidates.elements), name: "elements"},
		{model: &models.Glossary{}, ids: standardGlossaryIDs(candidates.glossaries), name: "glossaries"},
		{model: &models.MeasurementCategory{}, ids: standardMeasurementCategoryIDs(candidates.measurementCategories), name: "measurement categories"},
		{model: &models.CodeSet{}, ids: standardCodeSetIDs(candidates.codeSets), name: "code sets"},
		{model: &models.Domain{}, ids: standardDomainIDs(candidates.domains), name: "domains"},
	} {
		if len(batch.ids) == 0 {
			continue
		}
		if err := s.db.WithContext(ctx).Unscoped().Delete(batch.model, batch.ids).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete %s failed: %v", batch.name, err))
			continue
		}
		stats.DeletedRecords += len(batch.ids)
	}
}

func (s *CleanupService) deleteStandardDocumentFiles(ctx context.Context, revisions []models.DocumentRevision, stats *StandardCleanupStats) map[int64]struct{} {
	blockedDocumentIDs := make(map[int64]struct{})
	for _, revision := range revisions {
		if revision.FileKey == "" {
			continue
		}
		if s.minioClient == nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete document file %s failed: minio client is not configured", revision.FileKey))
			stats.SkippedItems++
			blockedDocumentIDs[revision.DocumentID] = struct{}{}
			continue
		}
		if err := s.minioClient.RemoveObject(ctx, minioBucket, revision.FileKey, minio.RemoveObjectOptions{}); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete document file %s failed: %v", revision.FileKey, err))
			stats.SkippedItems++
			blockedDocumentIDs[revision.DocumentID] = struct{}{}
			continue
		}
		stats.DeletedPhysicalArtifacts++
		stats.FreedBytes += revision.FileSize
	}
	return blockedDocumentIDs
}

func (s *CleanupService) deleteGlossaryElementMappings(ctx context.Context, candidates standardCleanupCandidates) (int, error) {
	glossaryIDs := standardGlossaryIDs(candidates.glossaries)
	elementIDs := standardElementIDs(candidates.elements)
	query := s.db.WithContext(ctx).Unscoped()
	if len(glossaryIDs) > 0 && len(elementIDs) > 0 {
		query = query.Where("glossary_id IN ? OR element_id IN ?", glossaryIDs, elementIDs)
	} else if len(glossaryIDs) > 0 {
		query = query.Where("glossary_id IN ?", glossaryIDs)
	} else if len(elementIDs) > 0 {
		query = query.Where("element_id IN ?", elementIDs)
	} else {
		return 0, nil
	}
	result := query.Delete(&models.GlossaryElementMapping{})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func standardCleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
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

func standardCleanupCounters(stats *StandardCleanupStats) map[string]int64 {
	return map[string]int64{"domains": int64(stats.Domains), "glossaries": int64(stats.Glossaries), "glossary_element_mappings": int64(stats.GlossaryElementMappings), "elements": int64(stats.Elements), "code_sets": int64(stats.CodeSets), "code_items": int64(stats.CodeItems), "measurement_categories": int64(stats.MeasurementCategories), "units": int64(stats.Units), "metric_categories": int64(stats.MetricCategories), "metrics": int64(stats.Metrics), "metric_revisions": int64(stats.MetricRevisions), "metric_dependencies": int64(stats.MetricDependencies), "documents": int64(stats.Documents), "document_element_mappings": int64(stats.DocumentElementMappings), "document_glossary_mappings": int64(stats.DocumentGlossaryMappings), "document_metric_mappings": int64(stats.DocumentMetricMappings), "reference_deletions": int64(stats.ReferenceDeletions), "deprecated_glossaries": int64(stats.DeprecatedGlossaries), "deprecated_elements": int64(stats.DeprecatedElements), "deprecated_metrics": int64(stats.DeprecatedMetrics), "deprecated_documents": int64(stats.DeprecatedDocuments), "deleted_records": int64(stats.DeletedRecords), "deleted_physical_artifacts": int64(stats.DeletedPhysicalArtifacts), "freed_bytes": int64(stats.FreedBytes), "skipped_items": int64(stats.SkippedItems), "error_count": int64(len(stats.Errors))}
}

func standardScanSummary(stats *StandardCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	count := standardCandidateRecordCount(stats)
	return events.CleanupResultSummary{
		ScannedItems: count,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    standardRiskLevelForCount(count),
	}
}

func standardExecuteSummary(stats *StandardCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.DeprecatedGlossaries + stats.DeprecatedElements + stats.DeprecatedMetrics + stats.DeprecatedDocuments + stats.DeletedRecords
	return events.CleanupResultSummary{
		AffectedRecords:          affected,
		DeletedPhysicalArtifacts: stats.DeletedPhysicalArtifacts,
		FreedBytes:               stats.FreedBytes,
		MarkedOutdated:           stats.DeprecatedGlossaries + stats.DeprecatedElements + stats.DeprecatedMetrics + stats.DeprecatedDocuments,
		SkippedItems:             stats.SkippedItems,
		ErrorCount:               len(stats.Errors),
		RiskLevel:                "low",
	}
}

func standardCandidateRecordCount(stats *StandardCleanupStats) int {
	return stats.Domains +
		stats.Glossaries +
		stats.GlossaryElementMappings +
		stats.Elements +
		stats.CodeSets +
		stats.CodeItems +
		stats.MeasurementCategories +
		stats.Units +
		stats.MetricCategories +
		stats.Metrics +
		stats.MetricRevisions +
		stats.MetricDependencies +
		stats.Documents +
		stats.DocumentElementMappings +
		stats.DocumentGlossaryMappings +
		stats.DocumentMetricMappings +
		stats.ReferenceDeletions
}

func standardRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func standardIntPtr(value int) *int {
	return &value
}

func standardDomainIDs(items []models.Domain) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardGlossaryIDs(items []models.Glossary) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardElementIDs(items []models.Element) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardCodeSetIDs(items []models.CodeSet) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardCodeSetRevisionIDs(items []models.CodeSetRevision) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardCodeItemIDs(items []models.CodeSetRevisionItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardMeasurementCategoryIDs(items []models.MeasurementCategory) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardUnitIDs(items []models.Unit) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardMetricCategoryIDs(items []models.MetricCategory) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardMetricIDs(items []models.MetricDefinition) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardMetricRevisionIDs(items []models.MetricDefinitionRevision) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardMetricDependencyIDs(items []models.MetricDefinitionRevisionDependency) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardDocumentIDs(items []models.Document) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardDocumentsExcept(items []models.Document, blockedIDs map[int64]struct{}) []models.Document {
	if len(blockedIDs) == 0 {
		return items
	}
	result := make([]models.Document, 0, len(items))
	for _, item := range items {
		if _, blocked := blockedIDs[item.ID]; blocked {
			continue
		}
		result = append(result, item)
	}
	return result
}

func standardDocumentElementMappingsForDocuments(items []models.DocumentElementMapping, documents []models.Document) []models.DocumentElementMapping {
	documentIDs := standardDocumentIDSet(documents)
	result := make([]models.DocumentElementMapping, 0, len(items))
	for _, item := range items {
		if _, ok := documentIDs[item.DocumentID]; ok {
			result = append(result, item)
		}
	}
	return result
}

func standardDocumentGlossaryMappingsForDocuments(items []models.DocumentGlossaryMapping, documents []models.Document) []models.DocumentGlossaryMapping {
	documentIDs := standardDocumentIDSet(documents)
	result := make([]models.DocumentGlossaryMapping, 0, len(items))
	for _, item := range items {
		if _, ok := documentIDs[item.DocumentID]; ok {
			result = append(result, item)
		}
	}
	return result
}

func standardDocumentMetricMappingsForDocuments(items []models.DocumentMetricMapping, documents []models.Document) []models.DocumentMetricMapping {
	documentIDs := standardDocumentIDSet(documents)
	result := make([]models.DocumentMetricMapping, 0, len(items))
	for _, item := range items {
		if _, ok := documentIDs[item.DocumentID]; ok {
			result = append(result, item)
		}
	}
	return result
}

func standardDocumentIDSet(documents []models.Document) map[int64]struct{} {
	ids := make(map[int64]struct{}, len(documents))
	for _, doc := range documents {
		ids[doc.ID] = struct{}{}
	}
	return ids
}

func standardDocumentElementMappingIDs(items []models.DocumentElementMapping) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardDocumentGlossaryMappingIDs(items []models.DocumentGlossaryMapping) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardDocumentMetricMappingIDs(items []models.DocumentMetricMapping) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardReferenceDeletionIDs(items []models.StandardReferenceDeletion) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
