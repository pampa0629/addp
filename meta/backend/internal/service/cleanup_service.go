package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacleanup"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"github.com/addp/meta/internal/search"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// CleanupService 负责定期清理已逻辑删除的记录和处理垃圾数据清理事件
type CleanupService struct {
	db              *gorm.DB
	redis           *redis.Client
	dbCleaner       *metacleanup.DatabaseCleaner
	searchCleaner   *metacleanup.MeilisearchCleaner
	taskExecRepo    *commonExecution.TaskExecutionRepository
	log             *slog.Logger
	retentionDays   int
	cleanupInterval time.Duration
	enabled         bool
	stopCh          chan struct{}
}

// CleanupConfig 清理服务配置
type CleanupConfig struct {
	Enabled         bool
	RetentionDays   int
	CleanupInterval time.Duration
}

// DefaultCleanupConfig 返回默认配置
func DefaultCleanupConfig() CleanupConfig {
	return CleanupConfig{
		Enabled:         true,
		RetentionDays:   90,
		CleanupInterval: 24 * time.Hour,
	}
}

// NewCleanupService 创建清理服务
func NewCleanupService(
	db *gorm.DB,
	redisClient *redis.Client,
	systemClient *commonClient.SystemClient,
	indexer *search.Indexer,
	config CleanupConfig,
) *CleanupService {
	if config.RetentionDays == 0 {
		config.RetentionDays = 90
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 24 * time.Hour
	}

	return &CleanupService{
		db:              db,
		redis:           redisClient,
		dbCleaner:       metacleanup.NewDatabaseCleaner(db, systemClient, logger.With("component", "cleanup_database")),
		searchCleaner:   metacleanup.NewMeilisearchCleaner(indexer, logger.With("component", "cleanup_meilisearch")),
		taskExecRepo:    commonExecution.NewTaskExecutionRepository(db),
		log:             logger.With("component", "cleanup_service"),
		retentionDays:   config.RetentionDays,
		cleanupInterval: config.CleanupInterval,
		enabled:         config.Enabled,
		stopCh:          make(chan struct{}),
	}
}

// Start 启动清理服务
func (s *CleanupService) Start(ctx context.Context) error {
	if !s.enabled {
		s.log.Info("清理服务已禁用")
		return nil
	}

	s.log.Info("启动清理服务",
		"retention_days", s.retentionDays,
		"cleanup_interval", s.cleanupInterval)

	go s.scheduleCleanup(ctx)
	return nil
}

// Stop 停止清理服务
func (s *CleanupService) Stop(ctx context.Context) error {
	if !s.enabled {
		return nil
	}

	s.log.Info("停止清理服务")
	close(s.stopCh)
	return nil
}

// scheduleCleanup 定时调度清理任务
func (s *CleanupService) scheduleCleanup(ctx context.Context) {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCleanup(ctx)
		}
	}
}

// runCleanup 执行清理任务
func (s *CleanupService) runCleanup(ctx context.Context) {
	startTime := time.Now()
	s.log.Info("开始清理已逻辑删除记录", "retention_days", s.retentionDays)

	expiryTime := time.Now().AddDate(0, 0, -s.retentionDays)

	// 先清理 meta_item（子表）
	var itemsDeleted int64
	result := s.db.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", expiryTime).
		Delete(&models.MetaItem{})

	if result.Error != nil {
		s.log.Error("清理 meta_item 失败", "error", result.Error)
	} else {
		itemsDeleted = result.RowsAffected
		s.log.Info("清理 meta_item 完成", "deleted_count", itemsDeleted)
	}

	// 再清理 meta_node（父表）
	var nodesDeleted int64
	result = s.db.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", expiryTime).
		Delete(&models.MetaNode{})

	if result.Error != nil {
		s.log.Error("清理 meta_node 失败", "error", result.Error)
	} else {
		nodesDeleted = result.RowsAffected
		s.log.Info("清理 meta_node 完成", "deleted_count", nodesDeleted)
	}

	s.log.Info("清理任务完成",
		"duration", time.Since(startTime),
		"items_deleted", itemsDeleted,
		"nodes_deleted", nodesDeleted,
		"total_deleted", itemsDeleted+nodesDeleted)
}

// ManualCleanup 手动触发清理
func (s *CleanupService) ManualCleanup(ctx context.Context, retentionDays int) (map[string]int64, error) {
	if retentionDays == 0 {
		retentionDays = s.retentionDays
	}

	expiryTime := time.Now().AddDate(0, 0, -retentionDays)

	var itemsDeleted, nodesDeleted int64

	result := s.db.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", expiryTime).
		Delete(&models.MetaItem{})
	if result.Error != nil {
		return nil, fmt.Errorf("清理 meta_item 失败: %w", result.Error)
	}
	itemsDeleted = result.RowsAffected

	result = s.db.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", expiryTime).
		Delete(&models.MetaNode{})
	if result.Error != nil {
		return nil, fmt.Errorf("清理 meta_node 失败: %w", result.Error)
	}
	nodesDeleted = result.RowsAffected

	return map[string]int64{
		"meta_item": itemsDeleted,
		"meta_node": nodesDeleted,
		"total":     itemsDeleted + nodesDeleted,
	}, nil
}

// ========== 垃圾数据清理事件处理 ==========

// SubscribeCleanupEvents 订阅清理事件
func (s *CleanupService) SubscribeCleanupEvents(ctx context.Context) error {
	if s.redis == nil {
		s.log.Warn("Redis 未配置，跳过清理事件订阅")
		return nil
	}

	// 使用 Redis Stream 订阅清理请求
	go s.consumeCleanupRequests(ctx)
	s.log.Info("清理事件订阅已启动")
	return nil
}

// consumeCleanupRequests 消费清理请求事件
func (s *CleanupService) consumeCleanupRequests(ctx context.Context) {
	groupName := "meta-cleanup-consumer"
	consumerName := "meta-worker"

	// 创建 Consumer Group（如果不存在）
	s.redis.XGroupCreateMkStream(ctx, events.EventCleanupRequest, groupName, "$")

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
			// 读取事件
			streams, err := s.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    groupName,
				Consumer: consumerName,
				Streams:  []string{events.EventCleanupRequest, ">"},
				Count:    1,
				Block:    5 * time.Second,
			}).Result()

			if err != nil {
				if err != redis.Nil {
					s.log.Error("读取清理事件失败", "error", err)
				}
				continue
			}

			// 处理事件
			for _, stream := range streams {
				for _, message := range stream.Messages {
					s.handleCleanupRequest(ctx, message)
					// 确认消息
					s.redis.XAck(ctx, events.EventCleanupRequest, groupName, message.ID)
				}
			}
		}
	}
}

// handleCleanupRequest 处理清理请求
func (s *CleanupService) handleCleanupRequest(ctx context.Context, message redis.XMessage) {
	event, err := metacleanup.ParseCleanupRequest(message.Values)
	if err != nil {
		s.log.Error("解析清理请求失败", "error", err, "message_id", message.ID, "values", message.Values)
		return
	}
	if !events.CleanupExpectedForModule(event.ExpectedModules, events.ModuleMeta) {
		return
	}

	s.log.Info("收到清理请求",
		"task_id", event.TaskID,
		"action", event.Action,
		"tenant_id", event.TenantID,
		"cleanup_mode", event.CleanupMode,
		"expected_modules", event.ExpectedModules)

	result := events.CleanupResultData{
		Module:      events.ModuleMeta,
		Action:      event.Action,
		TenantID:    event.TenantID,
		TaskID:      event.TaskID,
		CleanupMode: event.CleanupMode,
		TriggerType: event.TriggerType,
		Timestamp:   time.Now(),
	}

	exec, startedAt, execErr := s.createExecutorExecution(ctx, event)
	if execErr != nil {
		s.log.Error("创建 Meta cleanup executor execution 失败", "error", execErr, "task_id", event.TaskID)
	}

	// 无论成功失败都写入响应
	defer func() {
		if exec != nil {
			s.finishExecutorExecution(ctx, exec.ExecutionID, event.TenantID, startedAt, result)
		}
		s.writeResult(ctx, event.TaskID, result)
	}()

	// 根据动作类型处理
	switch event.Action {
	case events.CleanupActionScan:
		stats, err := s.ScanGarbage(ctx, event.TenantID, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			s.log.Error("扫描垃圾数据失败", "error", err, "tenant_id", event.TenantID)
			return
		}
		result.Status = events.CleanupResultSuccess
		result.Statistics = metacleanup.ToMap(stats)
		result.Summary = metaScanSummary(stats)
		s.log.Info("扫描垃圾数据完成", "tenant_id", event.TenantID, "task_id", event.TaskID)

	case events.CleanupActionExecute:
		execResult, err := s.ExecuteCleanup(ctx, event.TenantID, event.CleanupMode, event.Context)
		if err != nil {
			result.Status = events.CleanupResultFailed
			result.Errors = []string{err.Error()}
			s.log.Error("执行清理失败", "error", err, "tenant_id", event.TenantID)
			return
		}
		if len(execResult.Errors) > 0 {
			result.Status = events.CleanupResultPartialSuccess
			result.Errors = execResult.Errors
		} else {
			result.Status = events.CleanupResultSuccess
		}
		result.Statistics = metacleanup.ToMap(execResult)
		result.Summary = metaExecuteSummary(execResult)
		s.log.Info("执行清理完成", "tenant_id", event.TenantID, "task_id", event.TaskID)

	default:
		result.Status = events.CleanupResultFailed
		result.Errors = []string{"unknown action: " + event.Action}
		s.log.Error("未知的清理动作", "action", event.Action)
	}
}

// ScanGarbage 扫描垃圾数据
func (s *CleanupService) ScanGarbage(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*models.MetaCleanupStatistics, error) {
	stats := &models.MetaCleanupStatistics{}
	scope := metacleanup.ScopeFromContext(cleanupContext)

	// 1. 扫描无效引擎的数据
	invalidEngines, err := s.dbCleaner.ScanInvalidEnginesWithScope(ctx, tenantID, scope)
	if err != nil {
		return nil, fmt.Errorf("扫描无效引擎失败: %w", err)
	}
	stats.InvalidEngines.Count = len(invalidEngines)
	stats.InvalidEngines.Details = invalidEngines

	// 2. 扫描孤儿数据项
	orphanItems, err := s.dbCleaner.ScanOrphanItems(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("扫描孤儿数据失败: %w", err)
	}
	stats.OrphanItems.Count = len(orphanItems)
	if len(orphanItems) > 10 {
		stats.OrphanItems.Sample = orphanItems[:10]
	} else {
		stats.OrphanItems.Sample = orphanItems
	}

	// 3. 扫描过期数据（90天未扫描）
	expiredCount, err := s.dbCleaner.ScanExpiredData(ctx, tenantID, 90)
	if err != nil {
		return nil, fmt.Errorf("扫描过期数据失败: %w", err)
	}
	stats.ExpiredData.Count = expiredCount
	stats.ExpiredData.ThresholdDays = 90

	// 4. 扫描逻辑清理候选
	logicalCleanupNodes, logicalCleanupItems, err := s.dbCleaner.ScanLogicalCleanupCandidatesWithScope(ctx, tenantID, scope)
	if err != nil {
		return nil, fmt.Errorf("扫描逻辑清理候选失败: %w", err)
	}
	stats.LogicalCleanupCandidates.Nodes = logicalCleanupNodes
	stats.LogicalCleanupCandidates.Items = logicalCleanupItems
	stats.LogicalCleanupCandidates.CanRecover = true

	// 5. 扫描重复fingerprint
	duplicateCount, err := s.dbCleaner.ScanDuplicateFingerprints(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("扫描重复fingerprint失败: %w", err)
	}
	stats.DuplicateFingerprints.Count = duplicateCount

	// 6. 扫描 Meilisearch 垃圾（新增）
	if s.searchCleaner != nil && s.searchCleaner.Enabled() {
		meilisearchStats, err := s.searchCleaner.ScanGarbage(ctx, tenantID, s.dbCleaner.InvalidEngineIDsWithScope(ctx, tenantID, scope))
		if err != nil {
			s.log.Error("扫描 Meilisearch 垃圾失败", "error", err)
			// 不中断整体扫描流程
		} else {
			stats.MeilisearchIndexes.Count = meilisearchStats.TotalCount
			stats.MeilisearchIndexes.ByType = meilisearchStats.ByType
			if len(meilisearchStats.Samples) > 10 {
				stats.MeilisearchIndexes.Sample = meilisearchStats.Samples[:10]
			} else {
				stats.MeilisearchIndexes.Sample = meilisearchStats.Samples
			}
		}
	}

	// 7. 扫描 Meta-owned 扫描任务定义残留。
	scanTaskDefinitionCount, err := s.scanInvalidEngineScanTaskDefinitions(ctx, tenantID, scope)
	if err != nil {
		return nil, fmt.Errorf("扫描扫描任务定义残留失败: %w", err)
	}
	stats.ScanTaskDefinitions.Count = scanTaskDefinitionCount

	return stats, nil
}

// ExecuteCleanup 执行清理
func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}
	scope := metacleanup.ScopeFromContext(cleanupContext)

	// 根据清理模式执行数据库清理
	var dbResult *models.MetaCleanupExecuteResult
	var err error

	switch cleanupMode {
	case events.CleanupModeLogical:
		dbResult, err = s.dbCleaner.ExecuteSoftDelete(ctx, tenantID, s.dbCleaner.InvalidEngineIDsWithScope(ctx, tenantID, scope))
	case events.CleanupModePhysical:
		dbResult, err = s.dbCleaner.ExecuteHardDeleteWithScope(ctx, tenantID, scope)
	default:
		return nil, fmt.Errorf("unknown cleanup mode: %s", cleanupMode)
	}

	if err != nil {
		return nil, err
	}

	// 合并数据库清理结果
	result.DeletedNodes = dbResult.DeletedNodes
	result.DeletedItems = dbResult.DeletedItems
	result.DeletedFingerprints = dbResult.DeletedFingerprints
	result.Errors = append(result.Errors, dbResult.Errors...)

	// 执行 Meilisearch 清理（新增）
	if s.searchCleaner != nil && s.searchCleaner.Enabled() {
		deletedCount, err := s.searchCleaner.ExecuteCleanup(ctx, tenantID, s.dbCleaner.InvalidEngineIDsWithScope(ctx, tenantID, scope))
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("清理 Meilisearch 失败: %v", err))
			s.log.Error("清理 Meilisearch 失败", "error", err)
		} else {
			result.DeletedMeilisearchIndexes = deletedCount
			s.log.Info("Meilisearch 清理完成", "deleted_count", deletedCount)
		}
	}

	switch cleanupMode {
	case events.CleanupModeLogical:
		disabled, err := s.disableInvalidEngineScanTaskDefinitions(ctx, tenantID, scope)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("禁用扫描任务定义失败: %v", err))
		}
		result.DisabledScanTaskDefinitions = disabled
	case events.CleanupModePhysical:
		deleted, err := s.deleteInvalidEngineScanTaskDefinitions(ctx, tenantID, scope)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("删除扫描任务定义失败: %v", err))
		}
		result.DeletedScanTaskDefinitions = deleted
	}

	return result, nil
}

func (s *CleanupService) scanInvalidEngineScanTaskDefinitions(ctx context.Context, tenantID uint, scope metacleanup.CleanupScope) (int, error) {
	query := s.invalidEngineScanTaskDefinitionsQuery(ctx, tenantID, scope)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (s *CleanupService) disableInvalidEngineScanTaskDefinitions(ctx context.Context, tenantID uint, scope metacleanup.CleanupScope) (int, error) {
	query := s.invalidEngineScanTaskDefinitionsQuery(ctx, tenantID, scope).Where("enabled = ?", true)
	result := query.Updates(map[string]interface{}{
		"enabled":     false,
		"next_run_at": nil,
		"updated_at":  time.Now(),
	})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func (s *CleanupService) deleteInvalidEngineScanTaskDefinitions(ctx context.Context, tenantID uint, scope metacleanup.CleanupScope) (int, error) {
	query := s.invalidEngineScanTaskDefinitionsQuery(ctx, tenantID, scope).Unscoped()
	result := query.Delete(&models.ScanTask{})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func (s *CleanupService) invalidEngineScanTaskDefinitionsQuery(ctx context.Context, tenantID uint, scope metacleanup.CleanupScope) *gorm.DB {
	query := s.db.Model(&models.ScanTask{}).
		Where("owner_module = ?", "system")
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		return query.Where("engine_id = ? AND owner_ref = ?", scope.EngineID, scantask.AutomaticTaskOwnerRef(scope.EngineID))
	}

	invalidEngineIDs := s.dbCleaner.InvalidEngineIDsWithScope(ctx, tenantID, scope)
	if len(invalidEngineIDs) == 0 {
		return query.Where("1 = 0")
	}

	ownerRefs := make([]string, 0, len(invalidEngineIDs))
	for _, engineID := range invalidEngineIDs {
		ownerRefs = append(ownerRefs, scantask.AutomaticTaskOwnerRef(engineID))
	}
	return query.Where("engine_id IN ? AND owner_ref IN ?", invalidEngineIDs, ownerRefs)
}

// writeResult 写入结果到Redis
func (s *CleanupService) writeResult(ctx context.Context, taskID string, result events.CleanupResultData) {
	if s.redis == nil {
		return
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.log.Error("序列化清理结果失败", "error", err)
		return
	}

	key := fmt.Sprintf("cleanup:results:%s", taskID)
	if err := s.redis.HSet(ctx, key, events.ModuleMeta, string(resultJSON)).Err(); err != nil {
		s.log.Error("写入清理结果失败", "error", err, "task_id", taskID)
	}
}

func (s *CleanupService) createExecutorExecution(ctx context.Context, event events.CleanupRequestEvent) (*commonExecution.TaskExecution, time.Time, error) {
	if s.taskExecRepo == nil || event.ParentExecutionID == "" {
		return nil, time.Time{}, nil
	}
	startedAt := time.Now()
	currentStep := fmt.Sprintf("Meta cleanup %s", event.Action)
	triggerType, err := commonExecution.NormalizeTriggerType(event.TriggerType)
	if err != nil {
		triggerType = commonExecution.TriggerTypeManual
	}
	exec := &commonExecution.TaskExecution{
		TenantID:          int(event.TenantID),
		ExecutionID:       uuid.NewString(),
		Module:            commonExecution.ModuleMeta,
		TaskType:          commonExecution.TaskTypeCleanupExecutor,
		Source:            commonExecution.ModuleSystem,
		ParentExecutionID: &event.ParentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		Progress:          0,
		CurrentStep:       &currentStep,
		TriggerType:       triggerType,
		TriggeredBy:       ptrInt(int(event.RequestedBy)),
		ExecutionConfig: commonModels.JSONMap{
			"task_id":        event.TaskID,
			"action":         event.Action,
			"cleanup_mode":   event.CleanupMode,
			"based_on_scan":  event.BasedOnScan,
			"cause_event":    event.CauseEvent,
			"context":        event.Context,
			"request_module": commonExecution.ModuleSystem,
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
		s.log.Warn("更新 Meta cleanup executor execution 失败", "execution_id", executionID, "error", err)
	}
}

func metaScanSummary(stats *models.MetaCleanupStatistics) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	scannedItems := stats.InvalidEngines.Count +
		stats.OrphanItems.Count +
		stats.ExpiredData.Count +
		stats.LogicalCleanupCandidates.Nodes +
		stats.LogicalCleanupCandidates.Items +
		stats.DuplicateFingerprints.Count +
		stats.MeilisearchIndexes.Count +
		stats.ScanTaskDefinitions.Count

	riskLevel := "low"
	if scannedItems > 1000 {
		riskLevel = "high"
	} else if scannedItems > 100 {
		riskLevel = "medium"
	}

	return events.CleanupResultSummary{
		ScannedItems: scannedItems,
		RiskLevel:    riskLevel,
	}
}

func metaExecuteSummary(result *models.MetaCleanupExecuteResult) events.CleanupResultSummary {
	if result == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	errorCount := len(result.Errors)
	return events.CleanupResultSummary{
		AffectedRecords: result.DeletedNodes +
			result.DeletedItems +
			result.DeletedFingerprints +
			result.DeletedMeilisearchIndexes +
			result.DisabledScanTaskDefinitions +
			result.DeletedScanTaskDefinitions,
		DisabledTaskDefinitions: result.DisabledScanTaskDefinitions,
		ErrorCount:              errorCount,
		RiskLevel:               "low",
	}
}

func ptrInt(value int) *int {
	return &value
}
