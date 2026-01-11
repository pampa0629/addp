package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// CleanupService 负责定期清理软删除的记录和处理垃圾数据清理事件
type CleanupService struct {
	db              *gorm.DB
	redis           *redis.Client
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
func NewCleanupService(db *gorm.DB, redisClient *redis.Client, config CleanupConfig) *CleanupService {
	if config.RetentionDays == 0 {
		config.RetentionDays = 90
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 24 * time.Hour
	}

	return &CleanupService{
		db:              db,
		redis:           redisClient,
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

	// 清理历史扫描记录（激进策略）
	var scansDeleted int64
	scansDeleted = s.cleanupScanTaskRuns()

	s.log.Info("清理任务完成",
		"duration", time.Since(startTime),
		"items_deleted", itemsDeleted,
		"nodes_deleted", nodesDeleted,
		"scans_deleted", scansDeleted,
		"total_deleted", itemsDeleted+nodesDeleted+scansDeleted)
}

// cleanupScanTaskRuns 清理历史扫描记录（激进策略）
func (s *CleanupService) cleanupScanTaskRuns() int64 {
	now := time.Now()
	var totalDeleted int64

	// 1. 清理 30 天前的成功记录
	result := s.db.Where("status = ? AND completed_at < ?",
		"success", now.Add(-30*24*time.Hour)).
		Delete(&models.ScanTaskRun{})

	if result.Error != nil {
		s.log.Error("清理成功扫描记录失败", "error", result.Error)
	} else {
		totalDeleted += result.RowsAffected
		if result.RowsAffected > 0 {
			s.log.Info("清理成功扫描记录", "deleted", result.RowsAffected, "before", now.Add(-30*24*time.Hour).Format("2006-01-02"))
		}
	}

	// 2. 清理 90 天前的失败记录
	result = s.db.Where("status = ? AND completed_at < ?",
		"failed", now.Add(-90*24*time.Hour)).
		Delete(&models.ScanTaskRun{})

	if result.Error != nil {
		s.log.Error("清理失败扫描记录失败", "error", result.Error)
	} else {
		totalDeleted += result.RowsAffected
		if result.RowsAffected > 0 {
			s.log.Info("清理失败扫描记录", "deleted", result.RowsAffected, "before", now.Add(-90*24*time.Hour).Format("2006-01-02"))
		}
	}

	// 3. 清理 7 天前的取消记录
	result = s.db.Where("status = ? AND completed_at < ?",
		"canceled", now.Add(-7*24*time.Hour)).
		Delete(&models.ScanTaskRun{})

	if result.Error != nil {
		s.log.Error("清理取消扫描记录失败", "error", result.Error)
	} else {
		totalDeleted += result.RowsAffected
		if result.RowsAffected > 0 {
			s.log.Info("清理取消扫描记录", "deleted", result.RowsAffected, "before", now.Add(-7*24*time.Hour).Format("2006-01-02"))
		}
	}

	return totalDeleted
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

	return stats, nil
}

// scanInvalidEngines 扫描无效引擎的数据
func (s *CleanupService) scanInvalidEngines(ctx context.Context, tenantID uint) ([]models.InvalidEngineDetail, error) {
	var details []models.InvalidEngineDetail

	query := `
		SELECT
			mn.engine_id,
			COUNT(DISTINCT mn.id) as affected_nodes,
			COUNT(DISTINCT mi.id) as affected_items
		FROM metadata.meta_node mn
		LEFT JOIN metadata.meta_item mi ON mn.id = mi.node_id
		LEFT JOIN system.engines e ON mn.engine_id = e.id
		WHERE (e.id IS NULL OR e.is_active = false)
	`

	if tenantID > 0 {
		query += fmt.Sprintf(" AND mn.tenant_id = %d", tenantID)
	}

	query += " GROUP BY mn.engine_id"

	rows, err := s.db.Raw(query).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var detail models.InvalidEngineDetail
		if err := rows.Scan(&detail.EngineID, &detail.AffectedNodes, &detail.AffectedItems); err != nil {
			return nil, err
		}
		detail.Reason = "引擎已删除或禁用"
		detail.EngineName = fmt.Sprintf("Engine#%d", detail.EngineID)
		details = append(details, detail)
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
	switch deleteType {
	case events.DeleteTypeSoft:
		return s.executeSoftDelete(ctx, tenantID)
	case events.DeleteTypeHard:
		return s.executeHardDelete(ctx, tenantID)
	default:
		return nil, fmt.Errorf("unknown delete type: %s", deleteType)
	}
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

	query := `
		SELECT DISTINCT mn.engine_id
		FROM metadata.meta_node mn
		LEFT JOIN system.engines e ON mn.engine_id = e.id
		WHERE e.id IS NULL OR e.is_active = false
	`

	if tenantID > 0 {
		query += fmt.Sprintf(" AND mn.tenant_id = %d", tenantID)
	}

	s.db.Raw(query).Scan(&ids)
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

	// 软删除关联的MetaNode
	nodeResult := s.db.Where("engine_id = ?", event.EngineID).Delete(&models.MetaNode{})
	if nodeResult.Error != nil {
		s.log.Error("软删除MetaNode失败", "error", nodeResult.Error, "engine_id", event.EngineID)
	} else {
		s.log.Info("软删除MetaNode完成", "engine_id", event.EngineID, "count", nodeResult.RowsAffected)
	}

	// 软删除关联的MetaItem
	itemResult := s.db.Where("engine_id = ?", event.EngineID).Delete(&models.MetaItem{})
	if itemResult.Error != nil {
		s.log.Error("软删除MetaItem失败", "error", itemResult.Error, "engine_id", event.EngineID)
	} else {
		s.log.Info("软删除MetaItem完成", "engine_id", event.EngineID, "count", itemResult.RowsAffected)
	}
}

// convertToMap 将结构体转换为 map[string]interface{}
func convertToMap(v interface{}) map[string]interface{} {
	data, _ := json.Marshal(v)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}
