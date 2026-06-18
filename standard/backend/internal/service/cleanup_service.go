package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/standard/internal/models"
	"github.com/google/uuid"
	minio "github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type CleanupService struct {
	db           *gorm.DB
	redis        *redis.Client
	taskExecRepo *commonExecution.TaskExecutionRepository
	minioClient  *minio.Client
	log          *slog.Logger
	stopCh       chan struct{}
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
	Classifications          int      `json:"classifications"`
	GradingLevels            int      `json:"grading_levels"`
	MetricCategories         int      `json:"metric_categories"`
	Metrics                  int      `json:"metrics"`
	MetricElementMappings    int      `json:"metric_element_mappings"`
	MetricDependencies       int      `json:"metric_dependencies"`
	Documents                int      `json:"documents"`
	DocumentElementMappings  int      `json:"document_element_mappings"`
	DocumentGlossaryMappings int      `json:"document_glossary_mappings"`
	DocumentMetricMappings   int      `json:"document_metric_mappings"`
	DimensionHierarchies     int      `json:"dimension_hierarchies"`
	DimensionHierarchyLevels int      `json:"dimension_hierarchy_levels"`
	DeprecatedGlossaries     int      `json:"deprecated_glossaries,omitempty"`
	DeprecatedElements       int      `json:"deprecated_elements,omitempty"`
	DeprecatedMetrics        int      `json:"deprecated_metrics,omitempty"`
	DeletedRecords           int      `json:"deleted_records,omitempty"`
	DeletedPhysicalArtifacts int      `json:"deleted_physical_artifacts,omitempty"`
	FreedBytes               int64    `json:"freed_bytes,omitempty"`
	SkippedItems             int      `json:"skipped_items,omitempty"`
	Errors                   []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, redisClient *redis.Client, taskExecRepo *commonExecution.TaskExecutionRepository, minioClient *minio.Client) *CleanupService {
	return &CleanupService{
		db:           db,
		redis:        redisClient,
		taskExecRepo: taskExecRepo,
		minioClient:  minioClient,
		log:          logger.With("component", "standard_cleanup_service"),
		stopCh:       make(chan struct{}),
	}
}

func (s *CleanupService) Start(ctx context.Context) error {
	if s == nil || s.redis == nil {
		return nil
	}
	go s.consumeCleanupRequests(ctx)
	s.log.Info("Standard 资源回收事件订阅已启动")
	return nil
}

func (s *CleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}

func (s *CleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "standard-cleanup-consumer"
	consumerName := "standard-worker"
	_ = s.redis.XGroupCreateMkStream(ctx, events.EventCleanupRequest, groupName, "$").Err()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
			streams, err := s.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    groupName,
				Consumer: consumerName,
				Streams:  []string{events.EventCleanupRequest, ">"},
				Count:    1,
				Block:    5 * time.Second,
			}).Result()
			if err != nil {
				if err != redis.Nil {
					s.log.Error("读取资源回收请求失败", "error", err)
				}
				continue
			}
			for _, stream := range streams {
				for _, message := range stream.Messages {
					s.handleCleanupRequest(ctx, message)
					_ = s.redis.XAck(ctx, events.EventCleanupRequest, groupName, message.ID).Err()
				}
			}
		}
	}
}

func (s *CleanupService) handleCleanupRequest(ctx context.Context, message redis.XMessage) {
	event, err := events.ParseCleanupRequest(message.Values)
	if err != nil {
		s.log.Error("解析资源回收请求失败", "error", err, "message_id", message.ID)
		return
	}
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleStandard) {
		return
	}

	result := events.CleanupResultData{
		Module:      events.ModuleStandard,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Standard 资源回收执行记录失败", "error", execErr, "task_id", event.TaskID)
	}
	defer func() {
		if exec != nil {
			s.finishExecutorExecution(ctx, exec.ExecutionID, event.TenantID, startedAt, result)
		}
		s.writeResult(ctx, event.TaskID, result)
	}()

	switch event.Action {
	case events.CleanupActionScan:
		stats, err := s.ScanReclaimCandidates(ctx, event.TenantID, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
			return
		}
		result.Status = events.CleanupResultSuccess
		result.Statistics = standardCleanupStatsToMap(stats)
		result.Summary = standardScanSummary(stats)
	case events.CleanupActionExecute:
		stats, err := s.ExecuteCleanup(ctx, event.TenantID, event.CleanupMode, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
			return
		}
		if len(stats.Errors) > 0 {
			result.Status = events.CleanupResultPartialSuccess
			result.Errors = stats.Errors
		} else {
			result.Status = events.CleanupResultSuccess
		}
		result.Statistics = standardCleanupStatsToMap(stats)
		result.Summary = standardExecuteSummary(stats)
	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown resource reclaim action: " + event.Action}
		result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
	}
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
	codeItems                []models.CodeItem
	measurementCategories    []models.MeasurementCategory
	units                    []models.Unit
	classifications          []models.Classification
	gradingLevels            []models.GradingLevel
	metricCategories         []models.MetricCategory
	metrics                  []models.Metric
	metricElementMappings    []models.MetricElementMapping
	metricDependencies       []models.MetricDependency
	documents                []models.Document
	documentElementMappings  []models.DocumentElementMapping
	documentGlossaryMappings []models.DocumentGlossaryMapping
	documentMetricMappings   []models.DocumentMetricMapping
	dimensionHierarchies     []models.DimensionHierarchy
	dimensionHierarchyLevels []models.DimensionHierarchyLevel
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
		Classifications:          len(c.classifications),
		GradingLevels:            len(c.gradingLevels),
		MetricCategories:         len(c.metricCategories),
		Metrics:                  len(c.metrics),
		MetricElementMappings:    len(c.metricElementMappings),
		MetricDependencies:       len(c.metricDependencies),
		Documents:                len(c.documents),
		DocumentElementMappings:  len(c.documentElementMappings),
		DocumentGlossaryMappings: len(c.documentGlossaryMappings),
		DocumentMetricMappings:   len(c.documentMetricMappings),
		DimensionHierarchies:     len(c.dimensionHierarchies),
		DimensionHierarchyLevels: len(c.dimensionHierarchyLevels),
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
		if err := s.db.WithContext(ctx).Where("code_set_id IN ?", codeSetIDs).Find(&candidates.codeItems).Error; err != nil {
			return candidates, err
		}
	}

	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.measurementCategories).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.units).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.classifications).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.gradingLevels).Error; err != nil {
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
		if err := s.db.WithContext(ctx).Where("metric_id IN ?", metricIDs).Find(&candidates.metricElementMappings).Error; err != nil {
			return candidates, err
		}
		if err := s.db.WithContext(ctx).
			Where("from_metric_id IN ? OR to_metric_id IN ?", metricIDs, metricIDs).
			Find(&candidates.metricDependencies).Error; err != nil {
			return candidates, err
		}
	}

	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.documents).Error; err != nil {
		return candidates, err
	}
	documentIDs := standardDocumentIDs(candidates.documents)
	if len(documentIDs) > 0 {
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

	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.dimensionHierarchies).Error; err != nil {
		return candidates, err
	}
	hierarchyIDs := standardDimensionHierarchyIDs(candidates.dimensionHierarchies)
	if len(hierarchyIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("hierarchy_id IN ?", hierarchyIDs).Find(&candidates.dimensionHierarchyLevels).Error; err != nil {
			return candidates, err
		}
	}
	return candidates, nil
}

func (s *CleanupService) logicalCleanup(ctx context.Context, candidates standardCleanupCandidates, stats *StandardCleanupStats) {
	for _, glossary := range candidates.glossaries {
		if glossary.Status == "deprecated" {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.Glossary{}).Where("id = ?", glossary.ID).Update("status", "deprecated").Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("deprecate glossary %d failed: %v", glossary.ID, err))
			continue
		}
		stats.DeprecatedGlossaries++
	}
	for _, element := range candidates.elements {
		if element.Status == "deprecated" {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.Element{}).Where("id = ?", element.ID).Update("status", "deprecated").Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("deprecate element %d failed: %v", element.ID, err))
			continue
		}
		stats.DeprecatedElements++
	}
	for _, metric := range candidates.metrics {
		if metric.Status == "deprecated" {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.Metric{}).Where("id = ?", metric.ID).Update("status", "deprecated").Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("deprecate metric %d failed: %v", metric.ID, err))
			continue
		}
		stats.DeprecatedMetrics++
	}
}

func (s *CleanupService) physicalCleanup(ctx context.Context, candidates standardCleanupCandidates, stats *StandardCleanupStats) {
	blockedDocumentIDs := s.deleteStandardDocumentFiles(ctx, candidates.documents, stats)
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
		{model: &models.DocumentElementMapping{}, ids: standardDocumentElementMappingIDs(documentElementMappingsToDelete), name: "document element mappings"},
		{model: &models.DocumentGlossaryMapping{}, ids: standardDocumentGlossaryMappingIDs(documentGlossaryMappingsToDelete), name: "document glossary mappings"},
		{model: &models.DocumentMetricMapping{}, ids: standardDocumentMetricMappingIDs(documentMetricMappingsToDelete), name: "document metric mappings"},
		{model: &models.MetricDependency{}, ids: standardMetricDependencyIDs(candidates.metricDependencies), name: "metric dependencies"},
		{model: &models.MetricElementMapping{}, ids: standardMetricElementMappingIDs(candidates.metricElementMappings), name: "metric element mappings"},
		{model: &models.DimensionHierarchyLevel{}, ids: standardDimensionHierarchyLevelIDs(candidates.dimensionHierarchyLevels), name: "dimension hierarchy levels"},
		{model: &models.CodeItem{}, ids: standardCodeItemIDs(candidates.codeItems), name: "code items"},
		{model: &models.Unit{}, ids: standardUnitIDs(candidates.units), name: "units"},
		{model: &models.Document{}, ids: standardDocumentIDs(documentsToDelete), name: "documents"},
		{model: &models.Metric{}, ids: standardMetricIDs(candidates.metrics), name: "metrics"},
		{model: &models.MetricCategory{}, ids: standardMetricCategoryIDs(candidates.metricCategories), name: "metric categories"},
		{model: &models.DimensionHierarchy{}, ids: standardDimensionHierarchyIDs(candidates.dimensionHierarchies), name: "dimension hierarchies"},
		{model: &models.Element{}, ids: standardElementIDs(candidates.elements), name: "elements"},
		{model: &models.Glossary{}, ids: standardGlossaryIDs(candidates.glossaries), name: "glossaries"},
		{model: &models.GradingLevel{}, ids: standardGradingLevelIDs(candidates.gradingLevels), name: "grading levels"},
		{model: &models.Classification{}, ids: standardClassificationIDs(candidates.classifications), name: "classifications"},
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

func (s *CleanupService) deleteStandardDocumentFiles(ctx context.Context, documents []models.Document, stats *StandardCleanupStats) map[int64]struct{} {
	blockedDocumentIDs := make(map[int64]struct{})
	for _, doc := range documents {
		if doc.FileKey == "" {
			continue
		}
		if s.minioClient == nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete document file %s failed: minio client is not configured", doc.FileKey))
			stats.SkippedItems++
			blockedDocumentIDs[doc.ID] = struct{}{}
			continue
		}
		if err := s.minioClient.RemoveObject(ctx, minioBucket, doc.FileKey, minio.RemoveObjectOptions{}); err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete document file %s failed: %v", doc.FileKey, err))
			stats.SkippedItems++
			blockedDocumentIDs[doc.ID] = struct{}{}
			continue
		}
		stats.DeletedPhysicalArtifacts++
		stats.FreedBytes += doc.FileSize
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

func (s *CleanupService) createExecutorExecution(ctx context.Context, event events.CleanupRequestEvent) (*commonExecution.TaskExecution, time.Time, error) {
	if s.taskExecRepo == nil || event.ParentExecutionID == "" {
		return nil, time.Time{}, nil
	}
	startedAt := time.Now()
	currentStep := fmt.Sprintf("Standard 资源回收 %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleStandard,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       standardIntPtr(int(event.RequestedBy)),
		ExecutionConfig: commonModels.JSONMap{
			"task_id":       event.TaskID,
			"action":        event.Action,
			"cleanup_mode":  event.CleanupMode,
			"based_on_scan": event.BasedOnScan,
			"cause_event":   event.CauseEvent,
			"context":       event.Context,
		},
		StartedAt: &startedAt,
		CreatedAt: startedAt,
		UpdatedAt: startedAt,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return nil, startedAt, err
	}
	return exec, startedAt, nil
}

func (s *CleanupService) finishExecutorExecution(ctx context.Context, executionID string, tenantID uint, startedAt time.Time, result events.CleanupResultData) {
	if s.taskExecRepo == nil || executionID == "" {
		return
	}
	now := time.Now()
	status := commonExecution.ExecutionStatusSuccess
	if result.Status == events.CleanupResultFailed {
		status = commonExecution.ExecutionStatusFailed
	}
	var errDetails commonModels.JSONMap
	if len(result.Errors) > 0 {
		errDetails = commonModels.JSONMap{"errors": result.Errors}
	}
	if err := s.taskExecRepo.UpdateFields(ctx, executionID, int(tenantID), map[string]interface{}{
		"status":            status,
		"progress":          100,
		"metadata":          commonModels.JSONMap{"cleanup_result": result, "summary": result.Summary},
		"error_details":     errDetails,
		"completed_at":      now,
		"execution_time_ms": now.Sub(startedAt).Milliseconds(),
		"updated_at":        now,
	}); err != nil {
		s.log.Warn("更新 Standard 资源回收执行记录失败", "execution_id", executionID, "error", err)
	}
}

func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil || taskID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化 Standard 资源回收结果失败", "error", err, "task_id", taskID)
		return
	}
	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleStandard, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入 Standard 资源回收结果失败", "error", err, "task_id", taskID)
	}
}

func standardCleanupStatsToMap(stats *StandardCleanupStats) map[string]interface{} {
	if stats == nil {
		return nil
	}
	data, _ := json.Marshal(stats)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
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
	affected := stats.DeprecatedGlossaries + stats.DeprecatedElements + stats.DeprecatedMetrics + stats.DeletedRecords
	return events.CleanupResultSummary{
		AffectedRecords:          affected,
		DeletedPhysicalArtifacts: stats.DeletedPhysicalArtifacts,
		FreedBytes:               stats.FreedBytes,
		MarkedOutdated:           stats.DeprecatedGlossaries + stats.DeprecatedElements + stats.DeprecatedMetrics,
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
		stats.Classifications +
		stats.GradingLevels +
		stats.MetricCategories +
		stats.Metrics +
		stats.MetricElementMappings +
		stats.MetricDependencies +
		stats.Documents +
		stats.DocumentElementMappings +
		stats.DocumentGlossaryMappings +
		stats.DocumentMetricMappings +
		stats.DimensionHierarchies +
		stats.DimensionHierarchyLevels
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

func standardCodeItemIDs(items []models.CodeItem) []int64 {
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

func standardClassificationIDs(items []models.Classification) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardGradingLevelIDs(items []models.GradingLevel) []int64 {
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

func standardMetricIDs(items []models.Metric) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardMetricElementMappingIDs(items []models.MetricElementMapping) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardMetricDependencyIDs(items []models.MetricDependency) []int64 {
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

func standardDimensionHierarchyIDs(items []models.DimensionHierarchy) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func standardDimensionHierarchyLevelIDs(items []models.DimensionHierarchyLevel) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
