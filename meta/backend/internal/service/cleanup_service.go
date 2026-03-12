package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
	"github.com/meilisearch/meilisearch-go"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// CleanupService 负责定期清理软删除的记录和处理垃圾数据清理事件
type CleanupService struct {
	db              *gorm.DB
	redis           *redis.Client
	systemClient    *commonClient.SystemClient
	indexer         *search.Indexer
	minioClient     *minio.Client
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
		systemClient:    systemClient,
		indexer:         indexer,
		minioClient:     minioClient,
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
	s.log.Info("开始清理软删除记录和历史扫描记录", "retention_days", s.retentionDays)

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

	// scan_task_runs 表已废弃（由 common.task_executions 统一管理），清理逻辑跳过
	var scansDeleted int64

	s.log.Info("清理任务完成",
		"duration", time.Since(startTime),
		"items_deleted", itemsDeleted,
		"nodes_deleted", nodesDeleted,
		"scans_deleted", scansDeleted,
		"total_deleted", itemsDeleted+nodesDeleted+scansDeleted)
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
	// 解析事件 - Redis Stream 将所有值存储为字符串，需要手动类型转换
	values := message.Values

	// 转换 expected_modules 从 JSON 字符串到数组
	if modulesStr, ok := values["expected_modules"].(string); ok && modulesStr != "" {
		var modules []string
		if err := json.Unmarshal([]byte(modulesStr), &modules); err == nil {
			values["expected_modules"] = modules
		}
	}

	// 转换 tenant_id 从字符串到数字
	if tenantIDStr, ok := values["tenant_id"].(string); ok {
		var tenantID uint64
		if _, err := fmt.Sscanf(tenantIDStr, "%d", &tenantID); err == nil {
			values["tenant_id"] = tenantID
		}
	}

	// 转换 requested_by 从字符串到数字
	if requestedByStr, ok := values["requested_by"].(string); ok {
		var requestedBy uint64
		if _, err := fmt.Sscanf(requestedByStr, "%d", &requestedBy); err == nil {
			values["requested_by"] = requestedBy
		}
	}

	var event events.CleanupRequestEvent
	eventJSON, _ := json.Marshal(values)
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		s.log.Error("解析清理请求失败", "error", err, "message_id", message.ID, "values", values)
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
		result.Statistics = convertToMap(stats)
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
		result.Statistics = convertToMap(execResult)
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
	invalidEngines, err := s.scanInvalidEngines(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("扫描无效引擎失败: %w", err)
	}
	stats.InvalidEngines.Count = len(invalidEngines)
	stats.InvalidEngines.Details = invalidEngines

	// 2. 扫描孤儿数据项
	orphanItems, err := s.scanOrphanItems(ctx, tenantID)
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
	expiredCount, err := s.scanExpiredData(ctx, tenantID, 90)
	if err != nil {
		return nil, fmt.Errorf("扫描过期数据失败: %w", err)
	}
	stats.ExpiredData.Count = expiredCount
	stats.ExpiredData.ThresholdDays = 90

	// 4. 扫描软删除数据
	softDeletedNodes, softDeletedItems, err := s.scanSoftDeleted(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("扫描软删除数据失败: %w", err)
	}
	stats.SoftDeleted.Nodes = softDeletedNodes
	stats.SoftDeleted.Items = softDeletedItems
	stats.SoftDeleted.CanRecover = true

	// 5. 扫描重复fingerprint
	duplicateCount, err := s.scanDuplicateFingerprints(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("扫描重复fingerprint失败: %w", err)
	}
	stats.DuplicateFingerprints.Count = duplicateCount

	// 6. 扫描 Meilisearch 垃圾（新增）
	if s.indexer != nil && s.indexer.Enabled() {
		meilisearchStats, err := s.scanMeilisearchGarbage(ctx, tenantID)
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
	if s.minioClient != nil {
		minioStats, err := s.scanMinIOGarbage(ctx, tenantID)
		if err != nil {
			s.log.Error("扫描 MinIO 垃圾失败", "error", err)
			// 不中断整体扫描流程
		} else {
			stats.MinIOFiles.Count = minioStats.TotalCount
			stats.MinIOFiles.TotalSizeBytes = minioStats.TotalSizeBytes
			stats.MinIOFiles.TotalSizeMB = minioStats.TotalSizeMB
			stats.MinIOFiles.ByBucket = minioStats.ByBucket
			if len(minioStats.Samples) > 10 {
				stats.MinIOFiles.Sample = minioStats.Samples[:10]
			} else {
				stats.MinIOFiles.Sample = minioStats.Samples
			}
		}
	}

	return stats, nil
}

// scanInvalidEngines 扫描无效引擎的数据
func (s *CleanupService) scanInvalidEngines(ctx context.Context, tenantID uint) ([]models.InvalidEngineDetail, error) {
	var details []models.InvalidEngineDetail

	// 如果 SystemClient 未配置，跳过检查
	if s.systemClient == nil {
		s.log.Warn("SystemClient 未配置，跳过无效引擎检查")
		return details, nil
	}

	// 1. 从 System API 获取所有引擎（包括活跃和非活跃）
	allEngines, err := s.systemClient.ListEngines("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取引擎列表失败: %w", err)
	}

	// 2. 构建有效引擎 ID 集合（只包含活跃的引擎）
	validEngineIDs := make(map[uint]string) // engine_id -> engine_name
	for _, engine := range allEngines {
		if engine.IsActive {
			validEngineIDs[engine.ID] = engine.Name
		}
	}

	// 3. 查询数据库中所有 meta_node 和 meta_item 按 engine_id 分组的统计
	type engineStats struct {
		EngineID      uint
		AffectedNodes int64
		AffectedItems int64
	}

	var stats []engineStats
	query := s.db.Table("metadata.meta_node mn").
		Select("mn.engine_id, COUNT(DISTINCT mn.id) as affected_nodes, COUNT(DISTINCT mi.id) as affected_items").
		Joins("LEFT JOIN metadata.meta_item mi ON mn.id = mi.node_id").
		Group("mn.engine_id")

	if tenantID > 0 {
		query = query.Where("mn.tenant_id = ?", tenantID)
	}

	if err := query.Scan(&stats).Error; err != nil {
		return nil, fmt.Errorf("查询引擎统计失败: %w", err)
	}

	// 4. 在应用层过滤出无效的引擎（不在有效引擎集合中的）
	for _, stat := range stats {
		if engineName, exists := validEngineIDs[stat.EngineID]; !exists {
			// 引擎 ID 不在有效列表中（已删除或禁用）
			details = append(details, models.InvalidEngineDetail{
				EngineID:      stat.EngineID,
				EngineName:    fmt.Sprintf("Engine#%d", stat.EngineID),
				AffectedNodes: int(stat.AffectedNodes),
				AffectedItems: int(stat.AffectedItems),
				Reason:        "引擎已删除或禁用",
			})
		} else {
			// 引擎存在但可能被禁用，检查 allEngines 列表
			for _, engine := range allEngines {
				if engine.ID == stat.EngineID && !engine.IsActive {
					details = append(details, models.InvalidEngineDetail{
						EngineID:      stat.EngineID,
						EngineName:    engineName,
						AffectedNodes: int(stat.AffectedNodes),
						AffectedItems: int(stat.AffectedItems),
						Reason:        "引擎已禁用",
					})
					break
				}
			}
		}
	}

	return details, nil
}

// scanOrphanItems 扫描孤儿数据项
func (s *CleanupService) scanOrphanItems(ctx context.Context, tenantID uint) ([]models.OrphanItemDetail, error) {
	var items []models.OrphanItemDetail

	query := `
		SELECT mi.id, mi.name, mi.node_id
		FROM metadata.meta_item mi
		LEFT JOIN metadata.meta_node mn ON mi.node_id = mn.id
		WHERE mn.id IS NULL
	`

	if tenantID > 0 {
		query += fmt.Sprintf(" AND mi.tenant_id = %d", tenantID)
	}

	query += " LIMIT 100"

	rows, err := s.db.Raw(query).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item models.OrphanItemDetail
		if err := rows.Scan(&item.ItemID, &item.ItemName, &item.NodeID); err != nil {
			return nil, err
		}
		item.Reason = "node_id不存在"
		items = append(items, item)
	}

	return items, nil
}

// scanExpiredData 扫描过期数据
func (s *CleanupService) scanExpiredData(ctx context.Context, tenantID uint, thresholdDays int) (int, error) {
	var count int64

	query := s.db.Model(&models.MetaItem{}).
		Where("scanned_at < ?", time.Now().AddDate(0, 0, -thresholdDays))

	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}

	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}

	return int(count), nil
}

// scanSoftDeleted 扫描软删除数据
func (s *CleanupService) scanSoftDeleted(ctx context.Context, tenantID uint) (int, int, error) {
	var nodeCount, itemCount int64

	// 统计软删除的节点
	nodeQuery := s.db.Model(&models.MetaNode{}).Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		nodeQuery = nodeQuery.Where("tenant_id = ?", tenantID)
	}
	if err := nodeQuery.Count(&nodeCount).Error; err != nil {
		return 0, 0, err
	}

	// 统计软删除的数据项
	itemQuery := s.db.Model(&models.MetaItem{}).Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		itemQuery = itemQuery.Where("tenant_id = ?", tenantID)
	}
	if err := itemQuery.Count(&itemCount).Error; err != nil {
		return 0, 0, err
	}

	return int(nodeCount), int(itemCount), nil
}

// scanDuplicateFingerprints 扫描重复fingerprint
func (s *CleanupService) scanDuplicateFingerprints(ctx context.Context, tenantID uint) (int, error) {
	var count int64

	query := `
		SELECT COUNT(*) FROM (
			SELECT fingerprint
			FROM metadata.meta_item
			WHERE deleted_at IS NULL
	`

	if tenantID > 0 {
		query += fmt.Sprintf(" AND tenant_id = %d", tenantID)
	}

	query += `
			GROUP BY fingerprint
			HAVING COUNT(*) > 1
		) AS duplicates
	`

	if err := s.db.Raw(query).Scan(&count).Error; err != nil {
		return 0, err
	}

	return int(count), nil
}

// ExecuteCleanup 执行清理
func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, deleteType string) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}

	// 根据删除类型执行数据库清理
	var dbResult *models.MetaCleanupExecuteResult
	var err error

	switch deleteType {
	case events.DeleteTypeSoft:
		dbResult, err = s.executeSoftDelete(ctx, tenantID)
	case events.DeleteTypeHard:
		dbResult, err = s.executeHardDelete(ctx, tenantID)
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
	if s.indexer != nil && s.indexer.Enabled() {
		deletedCount, err := s.executeMeilisearchCleanup(ctx, tenantID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("清理 Meilisearch 失败: %v", err))
			s.log.Error("清理 Meilisearch 失败", "error", err)
		} else {
			result.DeletedMeilisearchIndexes = deletedCount
			s.log.Info("Meilisearch 清理完成", "deleted_count", deletedCount)
		}
	}

	// 执行 MinIO 清理（新增）
	if s.minioClient != nil {
		minioResult, err := s.executeMinIOCleanup(ctx, tenantID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("清理 MinIO 失败: %v", err))
			s.log.Error("清理 MinIO 失败", "error", err)
		} else {
			result.DeletedMinIOFiles = minioResult.DeletedFiles
			result.FreedSpaceMB = minioResult.FreedSpaceMB
			s.log.Info("MinIO 清理完成", "deleted_files", minioResult.DeletedFiles, "freed_mb", minioResult.FreedSpaceMB)
		}
	}

	return result, nil
}

// executeSoftDelete 执行软删除
func (s *CleanupService) executeSoftDelete(ctx context.Context, tenantID uint) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}

	// 1. 软删除无效引擎的节点
	invalidEngineIDs := s.getInvalidEngineIDs(ctx, tenantID)
	if len(invalidEngineIDs) > 0 {
		nodeResult := s.db.Model(&models.MetaNode{}).
			Where("engine_id IN ?", invalidEngineIDs)

		if tenantID > 0 {
			nodeResult = nodeResult.Where("tenant_id = ?", tenantID)
		}

		if err := nodeResult.Delete(&models.MetaNode{}).Error; err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("软删除节点失败: %v", err))
		} else {
			result.DeletedNodes = int(nodeResult.RowsAffected)
		}

		// 软删除关联的items
		itemResult := s.db.Model(&models.MetaItem{}).
			Where("engine_id IN ?", invalidEngineIDs)

		if tenantID > 0 {
			itemResult = itemResult.Where("tenant_id = ?", tenantID)
		}

		if err := itemResult.Delete(&models.MetaItem{}).Error; err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("软删除项失败: %v", err))
		} else {
			result.DeletedItems = int(itemResult.RowsAffected)
		}
	}

	// 2. 软删除孤儿items
	orphanSQL := `
		DELETE FROM metadata.meta_item
		WHERE id IN (
			SELECT mi.id
			FROM metadata.meta_item mi
			LEFT JOIN metadata.meta_node mn ON mi.node_id = mn.id
			WHERE mn.id IS NULL
	`
	if tenantID > 0 {
		orphanSQL += fmt.Sprintf(" AND mi.tenant_id = %d", tenantID)
	}
	orphanSQL += ")"

	orphanResult := s.db.Exec(orphanSQL)
	if orphanResult.Error != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("软删除孤儿项失败: %v", orphanResult.Error))
	} else {
		result.DeletedItems += int(orphanResult.RowsAffected)
	}

	return result, nil
}

// executeHardDelete 执行硬删除（物理删除软删除的数据）
func (s *CleanupService) executeHardDelete(ctx context.Context, tenantID uint) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}

	// 物理删除软删除的节点
	nodeQuery := s.db.Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		nodeQuery = nodeQuery.Where("tenant_id = ?", tenantID)
	}

	if err := nodeQuery.Delete(&models.MetaNode{}).Error; err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("物理删除节点失败: %v", err))
	} else {
		result.DeletedNodes = int(nodeQuery.RowsAffected)
	}

	// 物理删除软删除的items
	itemQuery := s.db.Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		itemQuery = itemQuery.Where("tenant_id = ?", tenantID)
	}

	if err := itemQuery.Delete(&models.MetaItem{}).Error; err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("物理删除项失败: %v", err))
	} else {
		result.DeletedItems = int(itemQuery.RowsAffected)
	}

	return result, nil
}

// getInvalidEngineIDs 获取无效的engine_id列表
func (s *CleanupService) getInvalidEngineIDs(ctx context.Context, tenantID uint) []uint {
	var ids []uint

	// 如果 SystemClient 未配置，返回空列表
	if s.systemClient == nil {
		s.log.Warn("SystemClient 未配置，无法获取无效引擎列表")
		return ids
	}

	// 1. 从 System API 获取所有引擎
	allEngines, err := s.systemClient.ListEngines("", tenantID)
	if err != nil {
		s.log.Error("获取引擎列表失败", "error", err)
		return ids
	}

	// 2. 构建有效引擎 ID 集合（只包含活跃的引擎）
	validEngineIDs := make(map[uint]bool)
	for _, engine := range allEngines {
		if engine.IsActive {
			validEngineIDs[engine.ID] = true
		}
	}

	// 3. 从数据库查询所有 meta_node 中的唯一 engine_id
	var allEngineIDsInDB []uint
	query := s.db.Table("metadata.meta_node").
		Select("DISTINCT engine_id").
		Where("deleted_at IS NULL")

	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}

	if err := query.Scan(&allEngineIDsInDB).Error; err != nil {
		s.log.Error("查询数据库引擎ID失败", "error", err)
		return ids
	}

	// 4. 过滤出无效的引擎 ID（不在有效引擎集合中的）
	for _, engineID := range allEngineIDsInDB {
		if !validEngineIDs[engineID] {
			ids = append(ids, engineID)
		}
	}

	return ids
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

// SubscribeEngineDeletedEvent 订阅Engine删除事件
func (s *CleanupService) SubscribeEngineDeletedEvent(ctx context.Context) error {
	if s.redis == nil {
		s.log.Warn("Redis 未配置，跳过Engine删除事件订阅")
		return nil
	}

	go s.consumeEngineDeletedEvents(ctx)
	s.log.Info("Engine删除事件订阅已启动")
	return nil
}

// consumeEngineDeletedEvents 消费Engine删除事件
func (s *CleanupService) consumeEngineDeletedEvents(ctx context.Context) {
	groupName := "meta-engine-cleanup-consumer"
	consumerName := "meta-worker"

	// 创建 Consumer Group（如果不存在）
	s.redis.XGroupCreateMkStream(ctx, events.EventEngineDeleted, groupName, "$")

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
				Streams:  []string{events.EventEngineDeleted, ">"},
				Count:    1,
				Block:    5 * time.Second,
			}).Result()

			if err != nil {
				if err != redis.Nil {
					s.log.Error("读取Engine删除事件失败", "error", err)
				}
				continue
			}

			for _, stream := range streams {
				for _, message := range stream.Messages {
					s.handleEngineDeleted(ctx, message)
					s.redis.XAck(ctx, events.EventEngineDeleted, groupName, message.ID)
				}
			}
		}
	}
}

// handleEngineDeleted 处理Engine删除事件
func (s *CleanupService) handleEngineDeleted(ctx context.Context, message redis.XMessage) {
	var event events.EngineDeletedEvent
	eventJSON, _ := json.Marshal(message.Values)
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		s.log.Error("解析Engine删除事件失败", "error", err)
		return
	}

	s.log.Info("收到Engine删除事件", "engine_id", event.EngineID, "tenant_id", event.TenantID)

	// 1. 软删除关联的MetaNode
	nodeResult := s.db.Where("engine_id = ?", event.EngineID).Delete(&models.MetaNode{})
	if nodeResult.Error != nil {
		s.log.Error("软删除MetaNode失败", "error", nodeResult.Error, "engine_id", event.EngineID)
	} else {
		s.log.Info("软删除MetaNode完成", "engine_id", event.EngineID, "count", nodeResult.RowsAffected)
	}

	// 2. 软删除关联的MetaItem
	itemResult := s.db.Where("engine_id = ?", event.EngineID).Delete(&models.MetaItem{})
	if itemResult.Error != nil {
		s.log.Error("软删除MetaItem失败", "error", itemResult.Error, "engine_id", event.EngineID)
	} else {
		s.log.Info("软删除MetaItem完成", "engine_id", event.EngineID, "count", itemResult.RowsAffected)
	}

	// 3. 删除 Meilisearch 索引（新增）
	if s.indexer != nil && s.indexer.Enabled() {
		filterStr := fmt.Sprintf("engine_id = %d", event.EngineID)
		if event.TenantID > 0 {
			filterStr = fmt.Sprintf("%s AND tenant_id = %d", filterStr, event.TenantID)
		}

		task, err := s.indexer.Client().Index(s.indexer.AssetIndexName()).DeleteDocumentsByFilter(filterStr)
		if err != nil {
			s.log.Error("删除 Meilisearch 索引失败", "engine_id", event.EngineID, "error", err)
		} else {
			s.log.Info("Meilisearch 索引已删除", "engine_id", event.EngineID, "task_uid", task.TaskUID)
		}
	}

	// 4. 删除 MinIO MVT 瓦片（新增）
	if s.minioClient != nil {
		s.deleteMinIOMVTByEngine(ctx, event.TenantID, event.EngineID)
	}
}

// convertToMap 将结构体转换为 map[string]interface{}
func convertToMap(v interface{}) map[string]interface{} {
	data, _ := json.Marshal(v)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}

// ========== Meilisearch 清理相关方法 ==========

// MeilisearchGarbageStats - Meilisearch 垃圾统计（内部使用）
type MeilisearchGarbageStats struct {
	TotalCount int
	ByType     map[string]int
	Samples    []models.MeilisearchRecordInfo
}

// scanMeilisearchGarbage 扫描 Meilisearch 中的垃圾索引
func (s *CleanupService) scanMeilisearchGarbage(ctx context.Context, tenantID uint) (*MeilisearchGarbageStats, error) {
	stats := &MeilisearchGarbageStats{
		ByType:  make(map[string]int),
		Samples: []models.MeilisearchRecordInfo{},
	}

	// 1. 获取无效引擎ID列表
	invalidEngineIDs := s.getInvalidEngineIDs(ctx, tenantID)
	if len(invalidEngineIDs) == 0 {
		return stats, nil
	}

	// 2. 构建过滤条件: (engine_id = X OR engine_id = Y) AND tenant_id = Z
	engineFilters := make([]string, len(invalidEngineIDs))
	for i, id := range invalidEngineIDs {
		engineFilters[i] = fmt.Sprintf("engine_id = %d", id)
	}

	filterStr := fmt.Sprintf("(%s)", strings.Join(engineFilters, " OR "))
	if tenantID > 0 {
		filterStr = fmt.Sprintf("%s AND tenant_id = %d", filterStr, tenantID)
	}

	// 3. 查询 assets 索引（包含表、对象、文档）
	searchReq := &meilisearch.SearchRequest{
		Limit:  1000, // 获取总数
		Filter: filterStr,
	}

	resp, err := s.indexer.Client().Index(s.indexer.AssetIndexName()).Search("", searchReq)
	if err != nil {
		s.log.Error("扫描 Meilisearch 索引失败", "error", err)
		return stats, err
	}

	// 4. 统计并提取样本
	stats.TotalCount = int(resp.EstimatedTotalHits)

	for i, hit := range resp.Hits {
		hitMap := hit.(map[string]interface{})
		assetType := getString(hitMap, "asset_type")

		// 按类型统计
		stats.ByType[assetType]++

		// 收集样本（最多10条）
		if i < 10 {
			stats.Samples = append(stats.Samples, models.MeilisearchRecordInfo{
				AssetID:   getString(hitMap, "asset_id"),
				AssetType: assetType,
				EngineID:  getUint(hitMap, "engine_id"),
				TenantID:  getUint(hitMap, "tenant_id"),
				Name:      getString(hitMap, "name"),
				Reason:    "引擎已删除或禁用",
			})
		}
	}

	return stats, nil
}

// executeMeilisearchCleanup 执行 Meilisearch 清理
func (s *CleanupService) executeMeilisearchCleanup(ctx context.Context, tenantID uint) (int, error) {
	// 1. 获取无效引擎ID列表
	invalidEngineIDs := s.getInvalidEngineIDs(ctx, tenantID)
	if len(invalidEngineIDs) == 0 {
		return 0, nil
	}

	// 2. 构建过滤条件
	engineFilters := make([]string, len(invalidEngineIDs))
	for i, id := range invalidEngineIDs {
		engineFilters[i] = fmt.Sprintf("engine_id = %d", id)
	}

	filterStr := fmt.Sprintf("(%s)", strings.Join(engineFilters, " OR "))
	if tenantID > 0 {
		filterStr = fmt.Sprintf("%s AND tenant_id = %d", filterStr, tenantID)
	}

	// 3. 先查询记录数（用于统计）
	searchReq := &meilisearch.SearchRequest{
		Limit:  0,
		Filter: filterStr,
	}

	resp, err := s.indexer.Client().Index(s.indexer.AssetIndexName()).Search("", searchReq)
	if err != nil {
		return 0, fmt.Errorf("查询待删除记录失败: %w", err)
	}

	count := int(resp.EstimatedTotalHits)
	if count == 0 {
		return 0, nil
	}

	// 4. 执行批量删除
	task, err := s.indexer.Client().Index(s.indexer.AssetIndexName()).DeleteDocumentsByFilter(filterStr)
	if err != nil {
		return 0, fmt.Errorf("删除索引记录失败: %w", err)
	}

	s.log.Info("Meilisearch 索引清理完成",
		"index", s.indexer.AssetIndexName(),
		"tenant_id", tenantID,
		"engine_ids", invalidEngineIDs,
		"deleted_count", count,
		"task_uid", task.TaskUID,
	)

	return count, nil
}

// getString 从 map 中安全获取字符串
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if str, ok := v.(string); ok {
			return str
		}
	}
	return ""
}

// getUint 从 map 中安全获取 uint
func getUint(m map[string]interface{}, key string) uint {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return uint(val)
		case int64:
			return uint(val)
		case int:
			return uint(val)
		case uint:
			return val
		}
	}
	return 0
}

// ========== MinIO 清理相关方法 ==========

// MinIOGarbageStats - MinIO 垃圾统计（内部使用）
type MinIOGarbageStats struct {
	TotalCount     int
	TotalSizeBytes int64
	TotalSizeMB    float64
	ByBucket       map[string]int
	Samples        []models.MinIOFileInfo
}

// scanMinIOGarbage 扫描 MinIO 中的垃圾文件
func (s *CleanupService) scanMinIOGarbage(ctx context.Context, tenantID uint) (*MinIOGarbageStats, error) {
	stats := &MinIOGarbageStats{
		ByBucket: make(map[string]int),
		Samples:  []models.MinIOFileInfo{},
	}

	if s.minioClient == nil {
		s.log.Warn("MinIO 客户端未配置，跳过 MinIO 垃圾扫描")
		return stats, nil
	}

	// 1. 获取无效引擎ID列表
	invalidEngineIDs := s.getInvalidEngineIDs(ctx, tenantID)

	// 2. 查询 manager.quick_view 表获取无效引擎的 fingerprint
	invalidFingerprints := s.getInvalidFingerprints(ctx, invalidEngineIDs)
	invalidFingerprintSet := make(map[string]bool)
	for _, fp := range invalidFingerprints {
		invalidFingerprintSet[fp] = true
	}

	// 3. 扫描 System bucket（审计日志，30天保留策略）
	systemStats := s.scanMinIOBucket(ctx, "system", func(key string, size int64, modified time.Time) (bool, string) {
		// 路径格式: audit-logs/{year}/{month}/logs-{date}.csv
		if strings.HasPrefix(key, "audit-logs/") {
			if time.Since(modified) > 30*24*time.Hour {
				return true, "超过30天"
			}
		}
		return false, ""
	})
	stats.TotalCount += systemStats.TotalCount
	stats.TotalSizeBytes += systemStats.TotalSizeBytes
	stats.ByBucket["system"] = systemStats.TotalCount
	stats.Samples = append(stats.Samples, systemStats.Samples...)

	// 4. 扫描 Manager bucket（MVT 瓦片，基于 fingerprint）
	managerStats := s.scanMinIOBucket(ctx, "manager", func(key string, size int64, modified time.Time) (bool, string) {
		// 路径格式: mvt-tiles/{fingerprint}/tiles/z{z}/{x}_{y}.mvt.gz
		if strings.HasPrefix(key, "mvt-tiles/") {
			parts := strings.Split(key, "/")
			if len(parts) >= 2 {
				fingerprint := parts[1]
				if invalidFingerprintSet[fingerprint] {
					return true, "引擎已删除"
				}
			}
		}
		return false, ""
	})
	stats.TotalCount += managerStats.TotalCount
	stats.TotalSizeBytes += managerStats.TotalSizeBytes
	stats.ByBucket["manager"] = managerStats.TotalCount
	stats.Samples = append(stats.Samples, managerStats.Samples...)

	// 5. 计算总大小（MB）
	stats.TotalSizeMB = float64(stats.TotalSizeBytes) / (1024 * 1024)

	return stats, nil
}

// getInvalidFingerprints 查询 manager.quick_view 表获取无效引擎的 fingerprint
func (s *CleanupService) getInvalidFingerprints(ctx context.Context, invalidEngineIDs []uint) []string {
	if len(invalidEngineIDs) == 0 {
		return []string{}
	}

	var fingerprints []string

	// 查询 manager.quick_view 表
	err := s.db.Table("manager.quick_view").
		Where("engine_id IN ?", invalidEngineIDs).
		Distinct("fingerprint").
		Pluck("fingerprint", &fingerprints).Error

	if err != nil {
		s.log.Error("查询 manager.quick_view fingerprint 失败", "error", err)
		return []string{}
	}

	s.log.Debug("获取无效引擎的 fingerprint",
		"engine_ids", invalidEngineIDs,
		"fingerprint_count", len(fingerprints))

	return fingerprints
}

// scanMinIOBucket 扫描指定 bucket 的垃圾文件
func (s *CleanupService) scanMinIOBucket(ctx context.Context, bucket string, isGarbage func(key string, size int64, modified time.Time) (bool, string)) *MinIOGarbageStats {
	stats := &MinIOGarbageStats{
		ByBucket: make(map[string]int),
		Samples:  []models.MinIOFileInfo{},
	}

	// 列出 bucket 中的所有对象
	objectCh := s.minioClient.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Recursive: true,
	})

	for object := range objectCh {
		if object.Err != nil {
			s.log.Error("列出对象失败", "bucket", bucket, "error", object.Err)
			continue
		}

		// 判断是否为垃圾文件
		if isGarbage, reason := isGarbage(object.Key, object.Size, object.LastModified); isGarbage {
			stats.TotalCount++
			stats.TotalSizeBytes += object.Size

			// 收集样本（最多10条）
			if len(stats.Samples) < 10 {
				stats.Samples = append(stats.Samples, models.MinIOFileInfo{
					Bucket:   bucket,
					Key:      object.Key,
					Size:     object.Size,
					Modified: object.LastModified,
					Reason:   reason,
				})
			}
		}
	}

	return stats
}

// MinIOCleanupResult - MinIO 清理结果（内部使用）
type MinIOCleanupResult struct {
	DeletedFiles int
	FreedSpaceMB float64
}

// executeMinIOCleanup 执行 MinIO 清理
func (s *CleanupService) executeMinIOCleanup(ctx context.Context, tenantID uint) (*MinIOCleanupResult, error) {
	result := &MinIOCleanupResult{}

	if s.minioClient == nil {
		s.log.Warn("MinIO 客户端未配置，跳过 MinIO 清理")
		return result, nil
	}

	// 1. 获取无效引擎ID列表
	invalidEngineIDs := s.getInvalidEngineIDs(ctx, tenantID)

	// 2. 查询 manager.quick_view 表获取无效引擎的 fingerprint
	invalidFingerprints := s.getInvalidFingerprints(ctx, invalidEngineIDs)
	invalidFingerprintSet := make(map[string]bool)
	for _, fp := range invalidFingerprints {
		invalidFingerprintSet[fp] = true
	}

	var totalFreedBytes int64

	// 3. 清理 System bucket（审计日志）
	systemDeleted, systemFreed := s.deleteMinIOBucketObjects(ctx, "system", func(key string, size int64, modified time.Time) bool {
		// 路径格式: audit-logs/{year}/{month}/logs-{date}.csv
		if strings.HasPrefix(key, "audit-logs/") {
			if time.Since(modified) > 30*24*time.Hour {
				return true
			}
		}
		return false
	})
	result.DeletedFiles += systemDeleted
	totalFreedBytes += systemFreed

	// 4. 清理 Manager bucket（MVT 瓦片）
	managerDeleted, managerFreed := s.deleteMinIOBucketObjects(ctx, "manager", func(key string, size int64, modified time.Time) bool {
		// 路径格式: mvt-tiles/{fingerprint}/tiles/z{z}/{x}_{y}.mvt.gz
		if strings.HasPrefix(key, "mvt-tiles/") {
			parts := strings.Split(key, "/")
			if len(parts) >= 2 {
				fingerprint := parts[1]
				if invalidFingerprintSet[fingerprint] {
					return true
				}
			}
		}
		return false
	})
	result.DeletedFiles += managerDeleted
	totalFreedBytes += managerFreed

	// 5. 计算释放的空间（MB）
	result.FreedSpaceMB = float64(totalFreedBytes) / (1024 * 1024)

	s.log.Info("MinIO 清理完成",
		"tenant_id", tenantID,
		"deleted_files", result.DeletedFiles,
		"freed_space_mb", result.FreedSpaceMB)

	return result, nil
}

// deleteMinIOBucketObjects 删除指定 bucket 中的垃圾文件
func (s *CleanupService) deleteMinIOBucketObjects(ctx context.Context, bucket string, shouldDelete func(key string, size int64, modified time.Time) bool) (int, int64) {
	var deletedCount int
	var freedBytes int64
	var objectsToDelete []string

	// 列出 bucket 中的所有对象
	objectCh := s.minioClient.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Recursive: true,
	})

	for object := range objectCh {
		if object.Err != nil {
			s.log.Error("列出对象失败", "bucket", bucket, "error", object.Err)
			continue
		}

		// 判断是否需要删除
		if shouldDelete(object.Key, object.Size, object.LastModified) {
			objectsToDelete = append(objectsToDelete, object.Key)
			freedBytes += object.Size
		}
	}

	// 批量删除对象
	if len(objectsToDelete) > 0 {
		objectsCh := make(chan minio.ObjectInfo)

		// 启动 goroutine 发送对象列表
		go func() {
			defer close(objectsCh)
			for _, key := range objectsToDelete {
				objectsCh <- minio.ObjectInfo{Key: key}
			}
		}()

		// 执行批量删除
		errorCh := s.minioClient.RemoveObjects(ctx, bucket, objectsCh, minio.RemoveObjectsOptions{})
		for err := range errorCh {
			if err.Err != nil {
				s.log.Error("删除对象失败", "bucket", bucket, "key", err.ObjectName, "error", err.Err)
			} else {
				deletedCount++
			}
		}

		s.log.Info("MinIO bucket 清理完成",
			"bucket", bucket,
			"deleted_count", deletedCount,
			"freed_bytes", freedBytes)
	}

	return deletedCount, freedBytes
}

// deleteMinIOMVTByEngine 删除指定引擎的 MVT 瓦片（自动清理）
func (s *CleanupService) deleteMinIOMVTByEngine(ctx context.Context, tenantID uint, engineID uint) {
	if s.minioClient == nil {
		s.log.Warn("MinIO 客户端未配置，跳过 MVT 瓦片清理")
		return
	}

	// 1. 查询 manager.quick_view 表获取该引擎的所有 fingerprint
	var fingerprints []string
	err := s.db.Table("manager.quick_view").
		Where("engine_id = ?", engineID).
		Distinct("fingerprint").
		Pluck("fingerprint", &fingerprints).Error

	if err != nil {
		s.log.Error("查询 manager.quick_view fingerprint 失败", "engine_id", engineID, "error", err)
		return
	}

	if len(fingerprints) == 0 {
		s.log.Debug("未找到需要清理的 MVT 瓦片", "engine_id", engineID)
		return
	}

	s.log.Info("开始清理引擎 MVT 瓦片", "engine_id", engineID, "fingerprint_count", len(fingerprints))

	// 2. 遍历每个 fingerprint，删除对应的 MVT 瓦片目录
	var totalDeleted int
	var totalFreedBytes int64

	for _, fingerprint := range fingerprints {
		deleted, freed := s.deleteMinIOMVTByFingerprint(ctx, fingerprint)
		totalDeleted += deleted
		totalFreedBytes += freed
	}

	s.log.Info("引擎 MVT 瓦片清理完成",
		"engine_id", engineID,
		"deleted_files", totalDeleted,
		"freed_mb", float64(totalFreedBytes)/(1024*1024))
}

// deleteMinIOMVTByFingerprint 删除指定 fingerprint 的 MVT 瓦片
func (s *CleanupService) deleteMinIOMVTByFingerprint(ctx context.Context, fingerprint string) (int, int64) {
	bucket := "manager"
	prefix := fmt.Sprintf("mvt-tiles/%s/", fingerprint)

	var deletedCount int
	var freedBytes int64
	var objectsToDelete []string

	// 列出该 fingerprint 下的所有对象
	objectCh := s.minioClient.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	for object := range objectCh {
		if object.Err != nil {
			s.log.Error("列出对象失败", "bucket", bucket, "prefix", prefix, "error", object.Err)
			continue
		}

		objectsToDelete = append(objectsToDelete, object.Key)
		freedBytes += object.Size
	}

	// 批量删除对象
	if len(objectsToDelete) > 0 {
		objectsCh := make(chan minio.ObjectInfo)

		// 启动 goroutine 发送对象列表
		go func() {
			defer close(objectsCh)
			for _, key := range objectsToDelete {
				objectsCh <- minio.ObjectInfo{Key: key}
			}
		}()

		// 执行批量删除
		errorCh := s.minioClient.RemoveObjects(ctx, bucket, objectsCh, minio.RemoveObjectsOptions{})
		for err := range errorCh {
			if err.Err != nil {
				s.log.Error("删除 MVT 瓦片失败", "key", err.ObjectName, "error", err.Err)
			} else {
				deletedCount++
			}
		}

		s.log.Info("MVT 瓦片已删除",
			"fingerprint", fingerprint,
			"deleted_count", deletedCount,
			"freed_bytes", freedBytes)
	}

	return deletedCount, freedBytes
}

