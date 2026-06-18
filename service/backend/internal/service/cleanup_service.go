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
	"github.com/addp/service/internal/models"
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

type ServiceCleanupStats struct {
	QueryServices          int      `json:"query_services"`
	GraphQueryServices     int      `json:"graph_query_services"`
	TileServices           int      `json:"tile_services"`
	TileLayers             int      `json:"tile_layers"`
	DisabledServiceRecords int      `json:"disabled_service_records,omitempty"`
	DisabledTileLayers     int      `json:"disabled_tile_layers,omitempty"`
	DeletedServiceRecords  int      `json:"deleted_service_records,omitempty"`
	DeletedTileLayers      int      `json:"deleted_tile_layers,omitempty"`
	Errors                 []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, redisClient *redis.Client, taskExecRepo *commonExecution.TaskExecutionRepository) *CleanupService {
	return &CleanupService{
		db:           db,
		redis:        redisClient,
		taskExecRepo: taskExecRepo,
		log:          logger.With("component", "service_cleanup_service"),
		stopCh:       make(chan struct{}),
	}
}

func (s *CleanupService) Start(ctx context.Context) error {
	if s == nil || s.redis == nil {
		return nil
	}
	go s.consumeCleanupRequests(ctx)
	s.log.Info("Service cleanup 事件订阅已启动")
	return nil
}

func (s *CleanupService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}

func (s *CleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "service-cleanup-consumer"
	consumerName := "service-worker"
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
					s.log.Error("读取 cleanup request 失败", "error", err)
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
		s.log.Error("解析 cleanup request 失败", "error", err, "message_id", message.ID)
		return
	}
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleService) {
		return
	}

	result := events.CleanupResultData{
		Module:      events.ModuleService,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Service cleanup executor execution 失败", "error", execErr, "task_id", event.TaskID)
	}
	defer func() {
		if exec != nil {
			s.finishExecutorExecution(ctx, exec.ExecutionID, event.TenantID, startedAt, result)
		}
		s.writeResult(ctx, event.TaskID, result)
	}()

	switch event.Action {
	case events.CleanupActionScan:
		stats, err := s.ScanGarbage(ctx, event.TenantID, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
			return
		}
		result.Status = events.CleanupResultSuccess
		result.Statistics = serviceCleanupStatsToMap(stats)
		result.Summary = serviceScanSummary(stats)
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
		result.Statistics = serviceCleanupStatsToMap(stats)
		result.Summary = serviceExecuteSummary(stats)
	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown cleanup action: " + event.Action}
		result.Summary = events.CleanupResultSummary{ErrorCount: 1, RiskLevel: "low"}
	}
}

func (s *CleanupService) ScanGarbage(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*ServiceCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*ServiceCleanupStats, error) {
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
		s.disableCandidates(ctx, candidates, stats)
	case events.CleanupModePhysical:
		s.deleteCandidates(ctx, candidates, stats)
	}
	return stats, nil
}

type serviceCleanupCandidates struct {
	queryServices      []models.QueryService
	graphQueryServices []models.GraphQueryService
	tileServices       []models.TileService
	tileLayers         []models.TileServiceLayer
}

func (c serviceCleanupCandidates) stats() *ServiceCleanupStats {
	return &ServiceCleanupStats{
		QueryServices:      len(c.queryServices),
		GraphQueryServices: len(c.graphQueryServices),
		TileServices:       len(c.tileServices),
		TileLayers:         len(c.tileLayers),
	}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (serviceCleanupCandidates, error) {
	if tenantID == 0 {
		return serviceCleanupCandidates{}, fmt.Errorf("service cleanup requires tenant_id")
	}
	if s == nil || s.db == nil {
		return serviceCleanupCandidates{}, fmt.Errorf("service cleanup database is not configured")
	}
	engineID, hasEngineID := cleanupContextUint(cleanupContext, "engine_id")
	contextTenantID, hasContextTenantID := cleanupContextUint(cleanupContext, "tenant_id")
	if !hasEngineID {
		if hasContextTenantID && contextTenantID == tenantID {
			return s.listTenantCandidates(ctx, tenantID)
		}
		return serviceCleanupCandidates{}, nil
	}
	if engineID == 0 {
		return serviceCleanupCandidates{}, nil
	}
	return s.listEngineCandidates(ctx, tenantID, engineID)
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID uint) (serviceCleanupCandidates, error) {
	var candidates serviceCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.queryServices).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.graphQueryServices).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.tileServices).Error; err != nil {
		return candidates, err
	}
	if len(candidates.tileServices) > 0 {
		serviceIDs := make([]uint, 0, len(candidates.tileServices))
		for _, service := range candidates.tileServices {
			serviceIDs = append(serviceIDs, service.ID)
		}
		if err := s.db.WithContext(ctx).Where("service_id IN ?", serviceIDs).Find(&candidates.tileLayers).Error; err != nil {
			return candidates, err
		}
	}
	return candidates, nil
}

func (s *CleanupService) listEngineCandidates(ctx context.Context, tenantID uint, engineID uint) (serviceCleanupCandidates, error) {
	var candidates serviceCleanupCandidates
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Find(&candidates.queryServices).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Find(&candidates.graphQueryServices).Error; err != nil {
		return candidates, err
	}

	var services []models.TileService
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&services).Error; err != nil {
		return candidates, err
	}
	if len(services) == 0 {
		return candidates, nil
	}
	serviceByID := make(map[uint]models.TileService, len(services))
	serviceIDs := make([]uint, 0, len(services))
	for _, service := range services {
		serviceByID[service.ID] = service
		serviceIDs = append(serviceIDs, service.ID)
	}

	var layers []models.TileServiceLayer
	if err := s.db.WithContext(ctx).Where("service_id IN ?", serviceIDs).Find(&layers).Error; err != nil {
		return candidates, err
	}
	tileServiceSeen := map[uint]struct{}{}
	for _, layer := range layers {
		if layer.GetEngineID() != engineID {
			continue
		}
		candidates.tileLayers = append(candidates.tileLayers, layer)
		if _, ok := tileServiceSeen[layer.ServiceID]; !ok {
			candidates.tileServices = append(candidates.tileServices, serviceByID[layer.ServiceID])
			tileServiceSeen[layer.ServiceID] = struct{}{}
		}
	}
	return candidates, nil
}

func (s *CleanupService) disableCandidates(ctx context.Context, candidates serviceCleanupCandidates, stats *ServiceCleanupStats) {
	message := "missing_source: cleanup lifecycle context"
	for _, item := range candidates.queryServices {
		if err := s.db.WithContext(ctx).Model(&models.QueryService{}).Where("id = ?", item.ID).Updates(map[string]interface{}{"status": "inactive", "error_message": message}).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("disable query service %d failed: %v", item.ID, err))
			continue
		}
		stats.DisabledServiceRecords++
	}
	for _, item := range candidates.graphQueryServices {
		if err := s.db.WithContext(ctx).Model(&models.GraphQueryService{}).Where("id = ?", item.ID).Updates(map[string]interface{}{"status": "inactive", "error_message": message}).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("disable graph query service %d failed: %v", item.ID, err))
			continue
		}
		stats.DisabledServiceRecords++
	}
	for _, item := range candidates.tileServices {
		if err := s.db.WithContext(ctx).Model(&models.TileService{}).Where("id = ?", item.ID).Updates(map[string]interface{}{"status": "inactive", "error_message": message}).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("disable tile service %d failed: %v", item.ID, err))
			continue
		}
		stats.DisabledServiceRecords++
	}
	for _, item := range candidates.tileLayers {
		if err := s.db.WithContext(ctx).Model(&models.TileServiceLayer{}).Where("id = ?", item.ID).Update("enabled", false).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("disable tile layer %d failed: %v", item.ID, err))
			continue
		}
		stats.DisabledTileLayers++
	}
}

func (s *CleanupService) deleteCandidates(ctx context.Context, candidates serviceCleanupCandidates, stats *ServiceCleanupStats) {
	for _, item := range candidates.tileLayers {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.TileServiceLayer{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete tile layer %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedTileLayers++
	}
	for _, item := range candidates.queryServices {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.QueryService{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete query service %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedServiceRecords++
	}
	for _, item := range candidates.graphQueryServices {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.GraphQueryService{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete graph query service %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedServiceRecords++
	}
	for _, item := range candidates.tileServices {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.TileService{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete tile service %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedServiceRecords++
	}
}

func cleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
	if cleanupContext == nil {
		return 0, false
	}
	raw, ok := cleanupContext[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case uint:
		return value, true
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
		return uint(parsed), err == nil
	case string:
		parsed, err := strconv.ParseUint(value, 10, 32)
		return uint(parsed), err == nil
	}
	return 0, false
}

func (s *CleanupService) createExecutorExecution(ctx context.Context, event events.CleanupRequestEvent) (*commonExecution.TaskExecution, time.Time, error) {
	if s.taskExecRepo == nil || event.ParentExecutionID == "" {
		return nil, time.Time{}, nil
	}
	startedAt := time.Now()
	currentStep := fmt.Sprintf("Service cleanup %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleService,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       serviceIntPtr(int(event.RequestedBy)),
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
		s.log.Warn("更新 Service cleanup executor execution 失败", "execution_id", executionID, "error", err)
	}
}

func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil || taskID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化 Service cleanup result 失败", "error", err, "task_id", taskID)
		return
	}
	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleService, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入 Service cleanup result 失败", "error", err, "task_id", taskID)
	}
}

func serviceCleanupStatsToMap(stats *ServiceCleanupStats) map[string]interface{} {
	if stats == nil {
		return nil
	}
	data, _ := json.Marshal(stats)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
}

func serviceScanSummary(stats *ServiceCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	scanned := stats.QueryServices + stats.GraphQueryServices + stats.TileServices + stats.TileLayers
	return events.CleanupResultSummary{
		ScannedItems: scanned,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    serviceRiskLevelForCount(scanned),
	}
}

func serviceExecuteSummary(stats *ServiceCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.DisabledServiceRecords + stats.DisabledTileLayers + stats.DeletedServiceRecords + stats.DeletedTileLayers
	return events.CleanupResultSummary{
		AffectedRecords: affected,
		MarkedOutdated:  stats.DisabledServiceRecords + stats.DisabledTileLayers,
		ErrorCount:      len(stats.Errors),
		RiskLevel:       "low",
	}
}

func serviceRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func serviceIntPtr(value int) *int {
	return &value
}
