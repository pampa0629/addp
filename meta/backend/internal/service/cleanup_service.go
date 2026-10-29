package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/cleanup"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/metacleanup"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// CleanupService 负责定期清理已逻辑删除的记录和处理资源回收事件
type CleanupService struct {
	db              *gorm.DB
	dbCleaner       *metacleanup.DatabaseCleaner
	runtime         *cleanup.Runner
	log             *slog.Logger
	retentionDays   int
	cleanupInterval time.Duration
	enabled         bool
	stopCh          chan struct{}
}

// CleanupConfig 逻辑删除记录保留期清除配置
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

// NewCleanupService 创建 Meta 资源回收服务
func NewCleanupService(
	db *gorm.DB,
	systemClient *commonClient.SystemServiceClient,
	config CleanupConfig,
) *CleanupService {
	if config.RetentionDays == 0 {
		config.RetentionDays = 90
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 24 * time.Hour
	}

	s := &CleanupService{
		db:              db,
		dbCleaner:       metacleanup.NewDatabaseCleaner(db, systemClient, logger.With("component", "cleanup_database")),
		log:             logger.With("component", "cleanup_service"),
		retentionDays:   config.RetentionDays,
		cleanupInterval: config.CleanupInterval,
		enabled:         config.Enabled,
		stopCh:          make(chan struct{}),
	}
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleMeta, System: systemClient, Log: s.log, Handle: s.runCleanupExecution}
	return s
}

// Start 启动 Meta 资源回收服务
func (s *CleanupService) Start(ctx context.Context) error {
	if err := s.runtime.Start(ctx); err != nil {
		return err
	}
	if !s.enabled {
		s.log.Info("Meta 资源回收服务已禁用")
		return nil
	}

	s.log.Info("启动 Meta 资源回收服务",
		"retention_days", s.retentionDays,
		"cleanup_interval", s.cleanupInterval)

	go s.scheduleCleanup(ctx)
	return nil
}

// Stop 停止 Meta 资源回收服务
func (s *CleanupService) Stop(ctx context.Context) error {
	s.runtime.Stop()
	if !s.enabled {
		return nil
	}

	s.log.Info("停止 Meta 资源回收服务")
	close(s.stopCh)
	return nil
}

// scheduleCleanup 定时调度已逻辑删除记录的物理清除
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

// runCleanup 清除超过保留期的逻辑删除记录
func (s *CleanupService) runCleanup(ctx context.Context) {
	startTime := time.Now()
	s.log.Info("开始清理已逻辑删除记录", "retention_days", s.retentionDays)

	expiryTime := time.Now().AddDate(0, 0, -s.retentionDays)

	result, err := s.dbCleaner.ExecuteExpiredHardDelete(ctx, 0, expiryTime)
	if err != nil {
		s.log.Error("清理逻辑删除记录失败", "error", err)
		return
	}
	for _, cleanupError := range result.Errors {
		s.log.Error("清理逻辑删除记录失败", "error", cleanupError)
	}

	s.log.Info("逻辑删除记录清除完成",
		"duration", time.Since(startTime),
		"items_deleted", result.DeletedItems,
		"nodes_deleted", result.DeletedNodes,
		"retained_lineage_items", result.RetainedLineageItems,
		"retained_referenced_nodes", result.RetainedReferencedNodes,
		"total_deleted", result.DeletedItems+result.DeletedNodes)
}

// ManualCleanup 手动触发清理
func (s *CleanupService) ManualCleanup(ctx context.Context, retentionDays int) (map[string]int64, error) {
	if retentionDays == 0 {
		retentionDays = s.retentionDays
	}

	expiryTime := time.Now().AddDate(0, 0, -retentionDays)

	result, err := s.dbCleaner.ExecuteExpiredHardDelete(ctx, 0, expiryTime)
	if err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("清理逻辑删除记录失败: %v", result.Errors)
	}

	return map[string]int64{
		"meta_item":      int64(result.DeletedItems),
		"meta_node":      int64(result.DeletedNodes),
		"retained_items": int64(result.RetainedLineageItems),
		"retained_nodes": int64(result.RetainedReferencedNodes),
		"total":          int64(result.DeletedItems + result.DeletedNodes),
	}, nil
}

func (s *CleanupService) metaEngineDeletionImpact(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}, _ *models.MetaCleanupStatistics) (events.CleanupImpactData, error) {
	scope := metacleanup.ScopeFromContext(cleanupContext)
	if scope.EngineID == 0 {
		return events.CleanupImpactData{}, errors.New("meta engine deletion assessment requires engine_id")
	}
	type idRow struct{ ID uint }
	var nodes []idRow
	if err := s.db.WithContext(ctx).Unscoped().Model(&models.MetaNode{}).
		Select("id").Where("tenant_id = ? AND engine_id = ?", tenantID, scope.EngineID).Find(&nodes).Error; err != nil {
		return events.CleanupImpactData{}, err
	}
	var metaItems []idRow
	if err := s.db.WithContext(ctx).Unscoped().Model(&models.MetaItem{}).
		Select("id").Where("tenant_id = ? AND engine_id = ?", tenantID, scope.EngineID).Find(&metaItems).Error; err != nil {
		return events.CleanupImpactData{}, err
	}
	var scanTasks []idRow
	if err := s.invalidEngineScanTaskDefinitionsQuery(ctx, tenantID, scope, []uint{scope.EngineID}).Unscoped().Select("id").Find(&scanTasks).Error; err != nil {
		return events.CleanupImpactData{}, err
	}
	items := make([]events.CleanupImpactItem, 0, len(nodes)+len(metaItems)+len(scanTasks)+1)
	for _, node := range nodes {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("meta_node:%d", node.ID), Disposition: events.CleanupImpactWillDelete})
	}
	for _, item := range metaItems {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("meta_item:%d", item.ID), Disposition: events.CleanupImpactWillDelete})
	}
	for _, task := range scanTasks {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("meta_scan_task:%d", task.ID), Disposition: events.CleanupImpactWillDisable})
	}
	return events.BuildCleanupImpactData(items, "/meta/catalog")
}

// ScanReclaimCandidates 扫描资源回收候选
func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*models.MetaCleanupStatistics, error) {
	stats := &models.MetaCleanupStatistics{}
	scope := metacleanup.ScopeFromContext(cleanupContext)
	invalidEngineIDs, err := s.dbCleaner.InvalidEngineIDsWithScope(ctx, tenantID, scope)
	if err != nil {
		return nil, err
	}

	// 1. 扫描无效引擎的数据
	invalidEngines, err := s.dbCleaner.ScanInvalidEnginesWithScope(ctx, tenantID, scope)
	if err != nil {
		return nil, fmt.Errorf("扫描无效引擎失败: %w", err)
	}
	stats.InvalidEngines.Count = len(invalidEngines)
	stats.InvalidEngines.Details = invalidEngines

	// 2. 扫描孤儿数据项
	orphanItems, err := s.dbCleaner.ScanOrphanItems(ctx, tenantID, scope)
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
	expiredCount, err := s.dbCleaner.ScanExpiredData(ctx, tenantID, 90, scope)
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
	retainedNodes, retainedItems, err := s.dbCleaner.ScanRetainedLineageReferencesWithScope(ctx, tenantID, scope)
	if err != nil {
		return nil, fmt.Errorf("扫描血缘保留项失败: %w", err)
	}
	stats.RetainedLineageReferences.Nodes = retainedNodes
	stats.RetainedLineageReferences.Items = retainedItems

	// 5. 扫描重复fingerprint
	duplicateCount, err := s.dbCleaner.ScanDuplicateFingerprints(ctx, tenantID, scope)
	if err != nil {
		return nil, fmt.Errorf("扫描重复fingerprint失败: %w", err)
	}
	stats.DuplicateFingerprints.Count = duplicateCount

	// 6. 扫描 Meta-owned 扫描任务定义残留。
	scanTaskDefinitionCount, err := s.scanInvalidEngineScanTaskDefinitions(ctx, tenantID, scope, invalidEngineIDs)
	if err != nil {
		return nil, fmt.Errorf("扫描扫描任务定义残留失败: %w", err)
	}
	stats.ScanTaskDefinitions.Count = scanTaskDefinitionCount

	return stats, nil
}

// ExecuteCleanup 执行资源回收
func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}
	scope := metacleanup.ScopeFromContext(cleanupContext)
	invalidEngineIDs, err := s.dbCleaner.InvalidEngineIDsWithScope(ctx, tenantID, scope)
	if err != nil {
		return nil, err
	}

	// 根据清理模式执行数据库清理
	var dbResult *models.MetaCleanupExecuteResult

	switch cleanupMode {
	case events.CleanupModeLogical:
		dbResult, err = s.dbCleaner.ExecuteSoftDelete(ctx, tenantID, invalidEngineIDs, scope)
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
	result.RetainedLineageItems = dbResult.RetainedLineageItems
	result.RetainedReferencedNodes = dbResult.RetainedReferencedNodes
	result.Errors = append(result.Errors, dbResult.Errors...)

	switch cleanupMode {
	case events.CleanupModeLogical:
		disabled, err := s.disableInvalidEngineScanTaskDefinitions(ctx, tenantID, scope, invalidEngineIDs)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("禁用扫描任务定义失败: %v", err))
		}
		result.DisabledScanTaskDefinitions = disabled
	case events.CleanupModePhysical:
		if scope.EngineID > 0 {
			disabled, err := s.disableInvalidEngineScanTaskDefinitions(ctx, tenantID, scope, invalidEngineIDs)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("禁用扫描任务定义失败: %v", err))
			}
			result.DisabledScanTaskDefinitions = disabled
		} else {
			deleted, err := s.deleteInvalidEngineScanTaskDefinitions(ctx, tenantID, scope, invalidEngineIDs)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("删除扫描任务定义失败: %v", err))
			}
			result.DeletedScanTaskDefinitions = deleted
		}
	}

	return result, nil
}

func (s *CleanupService) scanInvalidEngineScanTaskDefinitions(ctx context.Context, tenantID uint, scope metacleanup.CleanupScope, invalidEngineIDs []uint) (int, error) {
	query := s.invalidEngineScanTaskDefinitionsQuery(ctx, tenantID, scope, invalidEngineIDs)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (s *CleanupService) disableInvalidEngineScanTaskDefinitions(ctx context.Context, tenantID uint, scope metacleanup.CleanupScope, invalidEngineIDs []uint) (int, error) {
	query := s.invalidEngineScanTaskDefinitionsQuery(ctx, tenantID, scope, invalidEngineIDs).Where("enabled = ?", true)
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

func (s *CleanupService) deleteInvalidEngineScanTaskDefinitions(ctx context.Context, tenantID uint, scope metacleanup.CleanupScope, invalidEngineIDs []uint) (int, error) {
	query := s.invalidEngineScanTaskDefinitionsQuery(ctx, tenantID, scope, invalidEngineIDs).Unscoped()
	result := query.Delete(&models.ScanTask{})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func (s *CleanupService) invalidEngineScanTaskDefinitionsQuery(ctx context.Context, tenantID uint, scope metacleanup.CleanupScope, invalidEngineIDs []uint) *gorm.DB {
	query := s.db.WithContext(ctx).Model(&models.ScanTask{})
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		return query.Where("engine_id = ?", scope.EngineID)
	}

	if len(invalidEngineIDs) == 0 {
		return query.Where("1 = 0")
	}

	return query.Where("engine_id IN ?", invalidEngineIDs)
}

// writeResult 写入资源回收结果到 Redis
func metaScanSummary(stats *models.MetaCleanupStatistics) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	invalidEngineResources := 0
	for _, detail := range stats.InvalidEngines.Details {
		invalidEngineResources += detail.AffectedNodes + detail.AffectedItems
	}
	scannedItems := invalidEngineResources +
		stats.OrphanItems.Count +
		stats.ExpiredData.Count +
		stats.LogicalCleanupCandidates.Nodes +
		stats.LogicalCleanupCandidates.Items +
		stats.DuplicateFingerprints.Count +
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
			result.DisabledScanTaskDefinitions +
			result.DeletedScanTaskDefinitions,
		DisabledTaskDefinitions: result.DisabledScanTaskDefinitions,
		SkippedItems: result.RetainedLineageItems +
			result.RetainedReferencedNodes,
		ErrorCount: errorCount,
		RiskLevel:  "low",
	}
}

func ptrInt(value int) *int {
	return &value
}
