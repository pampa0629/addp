package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CleanupService struct {
	db           *gorm.DB
	redis        *redis.Client
	taskExecRepo *commonExecution.TaskExecutionRepository
	log          *slog.Logger
	stopCh       chan struct{}
	stopOnce     sync.Once
}

type ModelCleanupStats struct {
	DWLayers           int      `json:"dw_layers"`
	Entities           int      `json:"entities"`
	EntityAttributes   int      `json:"entity_attributes"`
	EntityRelations    int      `json:"entity_relations"`
	LogicalTables      int      `json:"logical_tables"`
	LogicalFields      int      `json:"logical_fields"`
	TableRelations     int      `json:"table_relations"`
	FactMetricMappings int      `json:"fact_metric_mappings"`
	DraftedEntities    int      `json:"drafted_entities,omitempty"`
	DraftedTables      int      `json:"drafted_tables,omitempty"`
	DeletedRecords     int      `json:"deleted_records,omitempty"`
	SkippedItems       int      `json:"skipped_items,omitempty"`
	Errors             []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, redisClient *redis.Client, taskExecRepo *commonExecution.TaskExecutionRepository) *CleanupService {
	return &CleanupService{
		db:           db,
		redis:        redisClient,
		taskExecRepo: taskExecRepo,
		log:          logger.With("component", "model_cleanup_service"),
		stopCh:       make(chan struct{}),
	}
}

func (s *CleanupService) Start(ctx context.Context) error {
	if s == nil || s.redis == nil {
		return nil
	}
	go s.consumeCleanupRequests(ctx)
	s.log.Info("Model 资源回收事件订阅已启动")
	return nil
}

func (s *CleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *CleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "model-cleanup-consumer"
	consumerName := "model-worker"
	_ = s.redis.XGroupCreateMkStream(ctx, events.EventCleanupRequest, groupName, "$").Err()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
			// 接管因进程崩溃遗留的 pending 消息，避免清理请求永久停留在消费组中。
			pending, _, claimErr := s.redis.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream: events.EventCleanupRequest, Group: groupName, Consumer: consumerName,
				MinIdle: time.Minute, Start: "-", Count: 1,
			}).Result()
			if claimErr == nil {
				for _, message := range pending {
					if s.handleCleanupRequest(ctx, message) {
						_ = s.redis.XAck(ctx, events.EventCleanupRequest, groupName, message.ID).Err()
					}
				}
			}
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
					if s.handleCleanupRequest(ctx, message) {
						_ = s.redis.XAck(ctx, events.EventCleanupRequest, groupName, message.ID).Err()
					}
				}
			}
		}
	}
}

func (s *CleanupService) handleCleanupRequest(ctx context.Context, message redis.XMessage) bool {
	event, err := events.ParseCleanupRequest(message.Values)
	if err != nil {
		s.log.Error("解析资源回收请求失败", "error", err, "message_id", message.ID)
		return false
	}
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleModel) {
		return true
	}

	result := events.CleanupResultData{
		Module:      events.ModuleModel,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Model 资源回收执行记录失败", "error", execErr, "task_id", event.TaskID)
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
			return false
		}
		result.Status = events.CleanupResultSuccess
		result.Statistics = modelCleanupStatsToMap(stats)
		result.Summary = modelScanSummary(stats)
	case events.CleanupActionExecute:
		stats, err := s.ExecuteCleanup(ctx, event.TenantID, event.CleanupMode, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
			return false
		}
		if len(stats.Errors) > 0 {
			result.Status = events.CleanupResultPartialSuccess
			result.Errors = stats.Errors
		} else {
			result.Status = events.CleanupResultSuccess
		}
		result.Statistics = modelCleanupStatsToMap(stats)
		result.Summary = modelExecuteSummary(stats)
	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown resource reclaim action: " + event.Action}
		result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
		return false
	}
	return true
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
	TenantID           int64
	dwLayers           []models.DWLayer
	entities           []models.Entity
	entityAttributes   []models.EntityAttribute
	entityRelations    []models.EntityRelation
	logicalTables      []models.LogicalTable
	logicalFields      []models.LogicalField
	tableRelations     []models.TableRelation
	factMetricMappings []models.FactMetricMapping
}

func (c modelCleanupCandidates) stats() *ModelCleanupStats {
	return &ModelCleanupStats{
		DWLayers:           len(c.dwLayers),
		Entities:           len(c.entities),
		EntityAttributes:   len(c.entityAttributes),
		EntityRelations:    len(c.entityRelations),
		LogicalTables:      len(c.logicalTables),
		LogicalFields:      len(c.logicalFields),
		TableRelations:     len(c.tableRelations),
		FactMetricMappings: len(c.factMetricMappings),
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
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.tableRelations).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.factMetricMappings).Error; err != nil {
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
		{model: &models.FactMetricMapping{}, ids: factMetricMappingIDs(candidates.factMetricMappings), name: "fact metric mappings", tenantScoped: true},
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

func (s *CleanupService) createExecutorExecution(ctx context.Context, event events.CleanupRequestEvent) (*commonExecution.TaskExecution, time.Time, error) {
	if s.taskExecRepo == nil || event.ParentExecutionID == "" {
		return nil, time.Time{}, nil
	}
	startedAt := time.Now()
	currentStep := fmt.Sprintf("Model 资源回收 %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleModel,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       modelIntPtr(int(event.RequestedBy)),
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
		s.log.Warn("更新 Model 资源回收执行记录失败", "execution_id", executionID, "error", err)
	}
}

func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil || taskID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化 Model 资源回收结果失败", "error", err, "task_id", taskID)
		return
	}
	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleModel, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入 Model 资源回收结果失败", "error", err, "task_id", taskID)
	}
}

func modelCleanupStatsToMap(stats *ModelCleanupStats) map[string]interface{} {
	if stats == nil {
		return nil
	}
	data, _ := json.Marshal(stats)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
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
		stats.TableRelations +
		stats.FactMetricMappings
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

func tableRelationIDs(items []models.TableRelation) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func factMetricMappingIDs(items []models.FactMetricMapping) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
