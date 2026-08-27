package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/addp/asset/internal/models"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
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

type AssetCleanupStats struct {
	TypeDefinitions       int      `json:"type_definitions"`
	TypeFieldSchemas      int      `json:"type_field_schemas"`
	Catalogs              int      `json:"catalogs"`
	Assets                int      `json:"assets"`
	AssetComponents       int      `json:"asset_components"`
	AssetExtFields        int      `json:"asset_ext_fields"`
	Applications          int      `json:"applications"`
	Authorizations        int      `json:"authorizations"`
	Ratings               int      `json:"ratings"`
	OfflineAssets         int      `json:"offline_assets,omitempty"`
	RevokedAuthorizations int      `json:"revoked_authorizations,omitempty"`
	DeletedRecords        int      `json:"deleted_records,omitempty"`
	SkippedItems          int      `json:"skipped_items,omitempty"`
	Errors                []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, redisClient *redis.Client, taskExecRepo *commonExecution.TaskExecutionRepository) *CleanupService {
	return &CleanupService{
		db:           db,
		redis:        redisClient,
		taskExecRepo: taskExecRepo,
		log:          logger.With("component", "asset_cleanup_service"),
		stopCh:       make(chan struct{}),
	}
}

func (s *CleanupService) Start(ctx context.Context) error {
	if s == nil || s.redis == nil {
		return nil
	}
	go s.consumeCleanupRequests(ctx)
	s.log.Info("Asset 资源回收事件订阅已启动")
	return nil
}

func (s *CleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}

func (s *CleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "asset-cleanup-consumer"
	consumerName := "asset-worker"
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
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleAsset) {
		return
	}

	result := events.CleanupResultData{
		Module:      events.ModuleAsset,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Asset 资源回收执行记录失败", "error", execErr, "task_id", event.TaskID)
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
		result.Statistics = assetCleanupStatsToMap(stats)
		result.Summary = assetScanSummary(stats)
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
		result.Statistics = assetCleanupStatsToMap(stats)
		result.Summary = assetExecuteSummary(stats)
	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown resource reclaim action: " + event.Action}
		result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
	}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*AssetCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*AssetCleanupStats, error) {
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

type assetCleanupCandidates struct {
	typeDefinitions  []models.TypeDefinition
	typeFieldSchemas []models.TypeFieldSchema
	catalogs         []models.Catalog
	assets           []models.Asset
	assetComponents  []models.AssetComponent
	assetExtFields   []models.AssetExtField
	applications     []models.Application
	authorizations   []models.Authorization
	ratings          []models.Rating
}

func (c assetCleanupCandidates) stats() *AssetCleanupStats {
	return &AssetCleanupStats{
		TypeDefinitions:  len(c.typeDefinitions),
		TypeFieldSchemas: len(c.typeFieldSchemas),
		Catalogs:         len(c.catalogs),
		Assets:           len(c.assets),
		AssetComponents:  len(c.assetComponents),
		AssetExtFields:   len(c.assetExtFields),
		Applications:     len(c.applications),
		Authorizations:   len(c.authorizations),
		Ratings:          len(c.ratings),
	}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (assetCleanupCandidates, error) {
	if tenantID == 0 {
		return assetCleanupCandidates{}, fmt.Errorf("asset resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return assetCleanupCandidates{}, fmt.Errorf("asset resource reclaim database is not configured")
	}
	contextTenantID, hasContextTenantID := assetCleanupContextUint(cleanupContext, "tenant_id")
	if !hasContextTenantID || contextTenantID != tenantID {
		return assetCleanupCandidates{}, nil
	}
	return s.listTenantCandidates(ctx, int64(tenantID))
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID int64) (assetCleanupCandidates, error) {
	var candidates assetCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.typeDefinitions).Error; err != nil {
		return candidates, err
	}
	typeIDs := make([]int64, 0, len(candidates.typeDefinitions))
	for _, item := range candidates.typeDefinitions {
		typeIDs = append(typeIDs, item.ID)
	}
	if len(typeIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("type_id IN ?", typeIDs).Find(&candidates.typeFieldSchemas).Error; err != nil {
			return candidates, err
		}
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.catalogs).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.assets).Error; err != nil {
		return candidates, err
	}
	assetIDs := make([]int64, 0, len(candidates.assets))
	for _, item := range candidates.assets {
		assetIDs = append(assetIDs, item.ID)
	}
	if len(assetIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND asset_id IN ?", tenantID, assetIDs).Find(&candidates.assetComponents).Error; err != nil {
			return candidates, err
		}
		if err := s.db.WithContext(ctx).Where("asset_id IN ?", assetIDs).Find(&candidates.assetExtFields).Error; err != nil {
			return candidates, err
		}
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.applications).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.authorizations).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.ratings).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) logicalCleanup(ctx context.Context, candidates assetCleanupCandidates, stats *AssetCleanupStats) {
	for _, asset := range candidates.assets {
		updates := map[string]interface{}{"status": "offline"}
		if err := s.db.WithContext(ctx).Model(&models.Asset{}).Where("id = ?", asset.ID).Updates(updates).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("offline asset %d failed: %v", asset.ID, err))
			continue
		}
		stats.OfflineAssets++
	}
	now := time.Now()
	for _, auth := range candidates.authorizations {
		if !auth.IsActive {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.Authorization{}).Where("id = ?", auth.ID).Updates(map[string]interface{}{
			"is_active":  false,
			"revoked_at": now,
		}).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("revoke authorization %d failed: %v", auth.ID, err))
			continue
		}
		stats.RevokedAuthorizations++
	}
}

func (s *CleanupService) physicalCleanup(ctx context.Context, candidates assetCleanupCandidates, stats *AssetCleanupStats) {
	for _, batch := range []struct {
		model interface{}
		ids   []int64
		name  string
	}{
		{model: &models.Rating{}, ids: ratingIDs(candidates.ratings), name: "ratings"},
		{model: &models.Authorization{}, ids: authorizationIDs(candidates.authorizations), name: "authorizations"},
		{model: &models.Application{}, ids: applicationIDs(candidates.applications), name: "applications"},
		{model: &models.AssetExtField{}, ids: assetExtFieldIDs(candidates.assetExtFields), name: "asset ext fields"},
		{model: &models.AssetComponent{}, ids: assetComponentIDs(candidates.assetComponents), name: "asset components"},
		{model: &models.Asset{}, ids: assetIDs(candidates.assets), name: "assets"},
		{model: &models.Catalog{}, ids: catalogIDs(candidates.catalogs), name: "catalogs"},
		{model: &models.TypeFieldSchema{}, ids: typeFieldSchemaIDs(candidates.typeFieldSchemas), name: "type field schemas"},
		{model: &models.TypeDefinition{}, ids: typeDefinitionIDs(candidates.typeDefinitions), name: "type definitions"},
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

func assetComponentIDs(items []models.AssetComponent) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func assetCleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
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
	currentStep := fmt.Sprintf("Asset 资源回收 %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleAsset,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       assetIntPtr(int(event.RequestedBy)),
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
		s.log.Warn("更新 Asset 资源回收执行记录失败", "execution_id", executionID, "error", err)
	}
}

func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil || taskID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化 Asset 资源回收结果失败", "error", err, "task_id", taskID)
		return
	}
	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleAsset, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入 Asset 资源回收结果失败", "error", err, "task_id", taskID)
	}
}

func assetCleanupStatsToMap(stats *AssetCleanupStats) map[string]interface{} {
	if stats == nil {
		return nil
	}
	data, _ := json.Marshal(stats)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
}

func assetScanSummary(stats *AssetCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	count := assetCandidateRecordCount(stats)
	return events.CleanupResultSummary{
		ScannedItems: count,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    assetRiskLevelForCount(count),
	}
}

func assetExecuteSummary(stats *AssetCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.OfflineAssets + stats.RevokedAuthorizations + stats.DeletedRecords
	return events.CleanupResultSummary{
		AffectedRecords:         affected,
		MarkedMissingSource:     stats.OfflineAssets,
		DisabledTaskDefinitions: stats.RevokedAuthorizations,
		SkippedItems:            stats.SkippedItems,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               "low",
	}
}

func assetCandidateRecordCount(stats *AssetCleanupStats) int {
	return stats.TypeDefinitions +
		stats.TypeFieldSchemas +
		stats.Catalogs +
		stats.Assets +
		stats.AssetComponents +
		stats.AssetExtFields +
		stats.Applications +
		stats.Authorizations +
		stats.Ratings
}

func assetRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func assetIntPtr(value int) *int {
	return &value
}

func typeDefinitionIDs(items []models.TypeDefinition) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func typeFieldSchemaIDs(items []models.TypeFieldSchema) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func catalogIDs(items []models.Catalog) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func assetIDs(items []models.Asset) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func assetExtFieldIDs(items []models.AssetExtField) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func applicationIDs(items []models.Application) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func authorizationIDs(items []models.Authorization) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func ratingIDs(items []models.Rating) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
