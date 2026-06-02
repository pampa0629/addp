package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/metacleanup"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// CleanupService 负责定期清理软删除的记录和处理垃圾数据清理事件
type CleanupService struct {
	db              *gorm.DB
	redis           *redis.Client
	dbCleaner       *metacleanup.DatabaseCleaner
	searchCleaner   *metacleanup.MeilisearchCleaner
	minioCleaner    *metacleanup.MinIOCleaner
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
	minioClient *minio.Client,
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
		minioCleaner:    metacleanup.NewMinIOCleaner(minioClient, logger.With("component", "cleanup_minio")),
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
	s.log.Info("开始清理软删除记录", "retention_days", s.retentionDays)

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

	s.log.Info("收到清理请求",
		"task_id", event.TaskID,
		"action", event.Action,
		"tenant_id", event.TenantID,
		"delete_type", event.DeleteType,
		"expected_modules", event.ExpectedModules)

	result := events.CleanupResultData{
		Module:    events.ModuleMeta,
		Timestamp: time.Now(),
	}

	// 无论成功失败都写入响应
	defer func() {
		s.writeResult(ctx, event.TaskID, result)
	}()

	// 根据动作类型处理
	switch event.Action {
	case events.CleanupActionScan:
		stats, err := s.ScanGarbage(ctx, event.TenantID)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			s.log.Error("扫描垃圾数据失败", "error", err, "tenant_id", event.TenantID)
			return
		}
		result.Status = "success"
		result.Statistics = metacleanup.ToMap(stats)
		s.log.Info("扫描垃圾数据完成", "tenant_id", event.TenantID, "task_id", event.TaskID)

	case events.CleanupActionExecute:
		execResult, err := s.ExecuteCleanup(ctx, event.TenantID, event.DeleteType)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			s.log.Error("执行清理失败", "error", err, "tenant_id", event.TenantID)
			return
		}
		result.Status = "success"
		result.Statistics = metacleanup.ToMap(execResult)
		s.log.Info("执行清理完成", "tenant_id", event.TenantID, "task_id", event.TaskID)

	default:
		result.Status = "failed"
		result.Error = "unknown action: " + event.Action
		s.log.Error("未知的清理动作", "action", event.Action)
	}
}

// ScanGarbage 扫描垃圾数据
func (s *CleanupService) ScanGarbage(ctx context.Context, tenantID uint) (*models.MetaCleanupStatistics, error) {
	stats := &models.MetaCleanupStatistics{}

	// 1. 扫描无效引擎的数据
	invalidEngines, err := s.dbCleaner.ScanInvalidEngines(ctx, tenantID)
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

	// 4. 扫描软删除数据
	softDeletedNodes, softDeletedItems, err := s.dbCleaner.ScanSoftDeleted(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("扫描软删除数据失败: %w", err)
	}
	stats.SoftDeleted.Nodes = softDeletedNodes
	stats.SoftDeleted.Items = softDeletedItems
	stats.SoftDeleted.CanRecover = true

	// 5. 扫描重复fingerprint
	duplicateCount, err := s.dbCleaner.ScanDuplicateFingerprints(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("扫描重复fingerprint失败: %w", err)
	}
	stats.DuplicateFingerprints.Count = duplicateCount

	// 6. 扫描 Meilisearch 垃圾（新增）
	if s.searchCleaner != nil && s.searchCleaner.Enabled() {
		meilisearchStats, err := s.searchCleaner.ScanGarbage(ctx, tenantID, s.dbCleaner.InvalidEngineIDs(ctx, tenantID))
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

	// 7. 扫描 MinIO 垃圾（新增）
	if s.minioCleaner != nil && s.minioCleaner.Enabled() {
		minioStats, err := s.minioCleaner.ScanGarbage(ctx, s.dbCleaner.InvalidFingerprints(ctx, s.dbCleaner.InvalidEngineIDs(ctx, tenantID)))
		if err != nil {
			s.log.Error("扫描 MinIO 垃圾失败", "error", err)
			// 不中断整体扫描流程
		} else {
			stats.MinIOObjects.Count = minioStats.TotalCount
			stats.MinIOObjects.TotalSizeBytes = minioStats.TotalSizeBytes
			stats.MinIOObjects.TotalSizeMB = minioStats.TotalSizeMB
			stats.MinIOObjects.ByBucket = minioStats.ByBucket
			if len(minioStats.Samples) > 10 {
				stats.MinIOObjects.Sample = minioStats.Samples[:10]
			} else {
				stats.MinIOObjects.Sample = minioStats.Samples
			}
		}
	}

	return stats, nil
}

// ExecuteCleanup 执行清理
func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, deleteType string) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}

	// 根据删除类型执行数据库清理
	var dbResult *models.MetaCleanupExecuteResult
	var err error

	switch deleteType {
	case events.DeleteTypeSoft:
		dbResult, err = s.dbCleaner.ExecuteSoftDelete(ctx, tenantID, s.dbCleaner.InvalidEngineIDs(ctx, tenantID))
	case events.DeleteTypeHard:
		dbResult, err = s.dbCleaner.ExecuteHardDelete(ctx, tenantID)
	default:
		return nil, fmt.Errorf("unknown delete type: %s", deleteType)
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
		deletedCount, err := s.searchCleaner.ExecuteCleanup(ctx, tenantID, s.dbCleaner.InvalidEngineIDs(ctx, tenantID))
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("清理 Meilisearch 失败: %v", err))
			s.log.Error("清理 Meilisearch 失败", "error", err)
		} else {
			result.DeletedMeilisearchIndexes = deletedCount
			s.log.Info("Meilisearch 清理完成", "deleted_count", deletedCount)
		}
	}

	// 执行 MinIO 清理（新增）
	if s.minioCleaner != nil && s.minioCleaner.Enabled() {
		minioResult, err := s.minioCleaner.ExecuteCleanup(ctx, s.dbCleaner.InvalidFingerprints(ctx, s.dbCleaner.InvalidEngineIDs(ctx, tenantID)))
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("清理 MinIO 失败: %v", err))
			s.log.Error("清理 MinIO 失败", "error", err)
		} else {
			result.DeletedMinIOObjects = minioResult.DeletedObjects
			result.FreedSpaceMB = minioResult.FreedSpaceMB
			s.log.Info("MinIO 清理完成", "deleted_objects", minioResult.DeletedObjects, "freed_mb", minioResult.FreedSpaceMB)
		}
	}

	return result, nil
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

func (s *CleanupService) CleanupEngineDeleted(ctx context.Context, engineID uint, tenantID uint) {
	s.log.Info("收到Engine删除清理请求", "engine_id", engineID, "tenant_id", tenantID)

	// 1. 软删除关联的MetaNode
	nodeResult := s.db.Where("engine_id = ?", engineID).Delete(&models.MetaNode{})
	if nodeResult.Error != nil {
		s.log.Error("软删除MetaNode失败", "error", nodeResult.Error, "engine_id", engineID)
	} else {
		s.log.Info("软删除MetaNode完成", "engine_id", engineID, "count", nodeResult.RowsAffected)
	}

	// 2. 软删除关联的MetaItem
	itemResult := s.db.Where("engine_id = ?", engineID).Delete(&models.MetaItem{})
	if itemResult.Error != nil {
		s.log.Error("软删除MetaItem失败", "error", itemResult.Error, "engine_id", engineID)
	} else {
		s.log.Info("软删除MetaItem完成", "engine_id", engineID, "count", itemResult.RowsAffected)
	}

	// 3. 删除 Meilisearch 索引（新增）
	if s.searchCleaner != nil && s.searchCleaner.Enabled() {
		if err := s.searchCleaner.DeleteByEngine(ctx, tenantID, engineID); err != nil {
			s.log.Error("删除 Meilisearch 索引失败", "engine_id", engineID, "error", err)
		}
	}

	// 4. 删除 MinIO MVT 瓦片（新增）
	if s.minioCleaner != nil && s.minioCleaner.Enabled() {
		s.deleteMinIOMVTByEngine(ctx, tenantID, engineID)
	}
}

// deleteMinIOMVTByEngine 删除指定引擎的 MVT 瓦片（自动清理）
func (s *CleanupService) deleteMinIOMVTByEngine(ctx context.Context, tenantID uint, engineID uint) {
	if s.minioCleaner == nil || !s.minioCleaner.Enabled() {
		s.log.Warn("MinIO 客户端未配置，跳过 MVT 瓦片清理")
		return
	}

	fingerprints := s.dbCleaner.FingerprintsByEngine(ctx, engineID)
	if len(fingerprints) == 0 {
		s.log.Debug("未找到需要清理的 MVT 瓦片", "engine_id", engineID)
		return
	}

	s.minioCleaner.DeleteMVTByFingerprints(ctx, engineID, fingerprints)
}
