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
	"github.com/addp/graph/internal/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type CleanupService struct {
	db           *gorm.DB
	redis        *redis.Client
	taskExecRepo *commonExecution.TaskExecutionRepository
	log          *slog.Logger
	stopCh       chan struct{}
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

func NewCleanupService(db *gorm.DB, redisClient *redis.Client, taskExecRepo *commonExecution.TaskExecutionRepository) *CleanupService {
	return &CleanupService{
		db:           db,
		redis:        redisClient,
		taskExecRepo: taskExecRepo,
		log:          logger.With("component", "graph_cleanup_service"),
		stopCh:       make(chan struct{}),
	}
}

func (s *CleanupService) Start(ctx context.Context) error {
	if s == nil || s.redis == nil {
		return nil
	}
	go s.consumeCleanupRequests(ctx)
	s.log.Info("Graph 资源回收事件订阅已启动")
	return nil
}

func (s *CleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}

func (s *CleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "graph-cleanup-consumer"
	consumerName := "graph-worker"
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
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleGraph) {
		return
	}

	result := events.CleanupResultData{
		Module:      events.ModuleGraph,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Graph 资源回收执行记录失败", "error", execErr, "task_id", event.TaskID)
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
		result.Statistics = graphCleanupStatsToMap(stats)
		result.Summary = graphScanSummary(stats)
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
		result.Statistics = graphCleanupStatsToMap(stats)
		result.Summary = graphExecuteSummary(stats)
	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown resource reclaim action: " + event.Action}
		result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
	}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*GraphCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*GraphCleanupStats, error) {
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
	for _, task := range candidates.buildTasks {
		if isCompletedGraphBuildStatus(task.Status) {
			stats.SkippedItems++
			continue
		}
		updates := map[string]interface{}{
			"status":        models.BuildStatusCancelled,
			"error_message": "source lifecycle cleanup",
			"completed_at":  time.Now(),
		}
		if err := s.db.WithContext(ctx).Model(&models.BuildTask{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("cancel build task %d failed: %v", task.ID, err))
			continue
		}
		stats.CancelledBuildTasks++
	}
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

func (s *CleanupService) createExecutorExecution(ctx context.Context, event events.CleanupRequestEvent) (*commonExecution.TaskExecution, time.Time, error) {
	if s.taskExecRepo == nil || event.ParentExecutionID == "" {
		return nil, time.Time{}, nil
	}
	startedAt := time.Now()
	currentStep := fmt.Sprintf("Graph 资源回收 %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleGraph,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       graphIntPtr(int(event.RequestedBy)),
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
	status := commonExecution.StatusFromCleanupResult(result.Status)
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
		s.log.Warn("更新 Graph 资源回收执行记录失败", "execution_id", executionID, "error", err)
	}
}

func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil || taskID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化 Graph 资源回收结果失败", "error", err, "task_id", taskID)
		return
	}
	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleGraph, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入 Graph 资源回收结果失败", "error", err, "task_id", taskID)
	}
}

func graphCleanupStatsToMap(stats *GraphCleanupStats) map[string]interface{} {
	if stats == nil {
		return nil
	}
	data, _ := json.Marshal(stats)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
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
