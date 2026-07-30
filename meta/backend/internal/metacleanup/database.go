package metacleanup

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	commonUtils "github.com/addp/common/utils"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

type DatabaseCleaner struct {
	db           *gorm.DB
	systemClient *commonClient.SystemServiceClient
	log          *slog.Logger
}

type metaEngineEligibility struct {
	name          string
	invalidReason string
}

func metaEngineEligibilityByID(engines []commonModels.Engine) map[uint]metaEngineEligibility {
	eligibilityByID := make(map[uint]metaEngineEligibility, len(engines))
	for index := range engines {
		engine := &engines[index]
		eligibility := metaEngineEligibility{name: engine.Name}
		switch {
		case !engine.IsUsable():
			eligibility.invalidReason = "引擎已禁用"
		case !commonUtils.HasStorageCapability(engine):
			eligibility.invalidReason = "引擎不具备存储能力"
		}
		eligibilityByID[engine.ID] = eligibility
	}
	return eligibilityByID
}

func NewDatabaseCleaner(db *gorm.DB, systemClient *commonClient.SystemServiceClient, log *slog.Logger) *DatabaseCleaner {
	return &DatabaseCleaner{db: db, systemClient: systemClient, log: log}
}

func (c *DatabaseCleaner) ScanInvalidEngines(ctx context.Context, tenantID uint) ([]models.InvalidEngineDetail, error) {
	return c.ScanInvalidEnginesWithScope(ctx, tenantID, CleanupScope{})
}

func (c *DatabaseCleaner) ScanInvalidEnginesWithScope(ctx context.Context, tenantID uint, scope CleanupScope) ([]models.InvalidEngineDetail, error) {
	var details []models.InvalidEngineDetail
	if c.systemClient == nil {
		if c.log != nil {
			c.log.Warn("SystemClient 未配置，跳过无效引擎检查")
		}
		return details, nil
	}

	allEngines, err := c.systemClient.WithTenantID(tenantID).ListEngines(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取引擎列表失败: %w", err)
	}

	eligibilityByID := metaEngineEligibilityByID(allEngines)

	type engineStats struct {
		EngineID      uint
		AffectedNodes int64
		AffectedItems int64
	}

	var stats []engineStats
	query := c.db.Table("meta.meta_node mn").
		Select("mn.engine_id, COUNT(DISTINCT mn.id) as affected_nodes, COUNT(DISTINCT mi.id) as affected_items").
		Joins("LEFT JOIN meta.meta_item mi ON mn.id = mi.node_id AND mi.deleted_at IS NULL").
		Where("mn.deleted_at IS NULL").
		Group("mn.engine_id").
		Order("mn.engine_id")
	if tenantID > 0 {
		query = query.Where("mn.tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		query = query.Where("mn.engine_id = ?", scope.EngineID)
	}

	if err := query.Scan(&stats).Error; err != nil {
		return nil, fmt.Errorf("查询引擎统计失败: %w", err)
	}

	for _, stat := range stats {
		eligibility, exists := eligibilityByID[stat.EngineID]
		if !exists {
			details = append(details, models.InvalidEngineDetail{
				EngineID:      stat.EngineID,
				EngineName:    fmt.Sprintf("Engine#%d", stat.EngineID),
				AffectedNodes: int(stat.AffectedNodes),
				AffectedItems: int(stat.AffectedItems),
				Reason:        "引擎已删除",
			})
			continue
		}
		if eligibility.invalidReason != "" {
			details = append(details, models.InvalidEngineDetail{
				EngineID:      stat.EngineID,
				EngineName:    eligibility.name,
				AffectedNodes: int(stat.AffectedNodes),
				AffectedItems: int(stat.AffectedItems),
				Reason:        eligibility.invalidReason,
			})
		}
	}
	return details, nil
}

func (c *DatabaseCleaner) ScanOrphanItems(ctx context.Context, tenantID uint) ([]models.OrphanItemDetail, error) {
	var items []models.OrphanItemDetail

	query := c.db.WithContext(ctx).
		Table("meta.meta_item AS mi").
		Select("mi.id AS item_id, mi.name AS item_name, mi.node_id").
		Joins("LEFT JOIN meta.meta_node mn ON mi.node_id = mn.id").
		Where("mn.id IS NULL").
		Where("mi.deleted_at IS NULL")
	if tenantID > 0 {
		query = query.Where("mi.tenant_id = ?", tenantID)
	}
	if err := query.Limit(100).Scan(&items).Error; err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Reason = "node_id不存在"
	}
	return items, nil
}

func (c *DatabaseCleaner) ScanExpiredData(ctx context.Context, tenantID uint, thresholdDays int) (int, error) {
	var count int64

	query := c.db.Model(&models.MetaItem{}).
		Where("scanned_at < ?", time.Now().AddDate(0, 0, -thresholdDays))
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (c *DatabaseCleaner) ScanLogicalCleanupCandidates(ctx context.Context, tenantID uint) (int, int, error) {
	return c.ScanLogicalCleanupCandidatesWithScope(ctx, tenantID, CleanupScope{})
}

func (c *DatabaseCleaner) ScanLogicalCleanupCandidatesWithScope(ctx context.Context, tenantID uint, scope CleanupScope) (int, int, error) {
	var nodeCount, itemCount int64

	nodeQuery := c.db.Model(&models.MetaNode{}).Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		nodeQuery = nodeQuery.Where("tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		nodeQuery = nodeQuery.Where("engine_id = ?", scope.EngineID)
	}
	if err := nodeQuery.Count(&nodeCount).Error; err != nil {
		return 0, 0, err
	}

	itemQuery := c.db.Model(&models.MetaItem{}).Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		itemQuery = itemQuery.Where("tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		itemQuery = itemQuery.Where("engine_id = ?", scope.EngineID)
	}
	if err := itemQuery.Count(&itemCount).Error; err != nil {
		return 0, 0, err
	}

	return int(nodeCount), int(itemCount), nil
}

func (c *DatabaseCleaner) ScanDuplicateFingerprints(ctx context.Context, tenantID uint) (int, error) {
	var count int64

	query := `
		SELECT COUNT(*) FROM (
			SELECT fingerprint
			FROM meta.meta_item
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

	if err := c.db.Raw(query).Scan(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (c *DatabaseCleaner) ExecuteSoftDelete(ctx context.Context, tenantID uint, invalidEngineIDs []uint) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}

	if len(invalidEngineIDs) > 0 {
		nodeResult := c.db.Model(&models.MetaNode{}).Where("engine_id IN ?", invalidEngineIDs)
		if tenantID > 0 {
			nodeResult = nodeResult.Where("tenant_id = ?", tenantID)
		}
		if err := nodeResult.Delete(&models.MetaNode{}).Error; err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("软删除节点失败: %v", err))
		} else {
			result.DeletedNodes = int(nodeResult.RowsAffected)
		}

		itemResult := c.db.Model(&models.MetaItem{}).Where("engine_id IN ?", invalidEngineIDs)
		if tenantID > 0 {
			itemResult = itemResult.Where("tenant_id = ?", tenantID)
		}
		if err := itemResult.Delete(&models.MetaItem{}).Error; err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("软删除项失败: %v", err))
		} else {
			result.DeletedItems = int(itemResult.RowsAffected)
		}
	}

	orphanIDs := c.db.Table("meta.meta_item AS mi").
		Select("mi.id").
		Joins("LEFT JOIN meta.meta_node mn ON mi.node_id = mn.id").
		Where("mn.id IS NULL").
		Where("mi.deleted_at IS NULL")
	if tenantID > 0 {
		orphanIDs = orphanIDs.Where("mi.tenant_id = ?", tenantID)
	}

	orphanResult := c.db.WithContext(ctx).
		Where("id IN (?)", orphanIDs).
		Delete(&models.MetaItem{})
	if orphanResult.Error != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("软删除孤儿项失败: %v", orphanResult.Error))
	} else {
		result.DeletedItems += int(orphanResult.RowsAffected)
	}
	return result, nil
}

func (c *DatabaseCleaner) ExecuteSoftDeleteByEngine(ctx context.Context, tenantID uint, engineID uint) (*models.MetaCleanupExecuteResult, error) {
	return c.ExecuteSoftDelete(ctx, tenantID, []uint{engineID})
}

func (c *DatabaseCleaner) ExecuteHardDelete(ctx context.Context, tenantID uint) (*models.MetaCleanupExecuteResult, error) {
	return c.ExecuteHardDeleteWithScope(ctx, tenantID, CleanupScope{})
}

func (c *DatabaseCleaner) ExecuteHardDeleteWithScope(ctx context.Context, tenantID uint, scope CleanupScope) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}

	nodeQuery := c.db.Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		nodeQuery = nodeQuery.Where("tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		nodeQuery = nodeQuery.Where("engine_id = ?", scope.EngineID)
	}
	if err := nodeQuery.Delete(&models.MetaNode{}).Error; err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("物理删除节点失败: %v", err))
	} else {
		result.DeletedNodes = int(nodeQuery.RowsAffected)
	}

	itemQuery := c.db.Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		itemQuery = itemQuery.Where("tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		itemQuery = itemQuery.Where("engine_id = ?", scope.EngineID)
	}
	if err := itemQuery.Delete(&models.MetaItem{}).Error; err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("物理删除项失败: %v", err))
	} else {
		result.DeletedItems = int(itemQuery.RowsAffected)
	}
	return result, nil
}

func (c *DatabaseCleaner) InvalidEngineIDs(ctx context.Context, tenantID uint) []uint {
	return c.InvalidEngineIDsWithScope(ctx, tenantID, CleanupScope{})
}

func (c *DatabaseCleaner) InvalidEngineIDsWithScope(ctx context.Context, tenantID uint, scope CleanupScope) []uint {
	var ids []uint
	if c.systemClient == nil {
		if c.log != nil {
			c.log.Warn("SystemClient 未配置，无法获取无效引擎列表")
		}
		return ids
	}

	allEngines, err := c.systemClient.WithTenantID(tenantID).ListEngines(ctx)
	if err != nil {
		if c.log != nil {
			c.log.Error("获取引擎列表失败", "error", err)
		}
		return ids
	}

	eligibilityByID := metaEngineEligibilityByID(allEngines)

	var allEngineIDsInDB []uint
	query := c.db.Table("meta.meta_node").
		Select("DISTINCT engine_id").
		Order("engine_id")
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		query = query.Where("engine_id = ?", scope.EngineID)
	}
	if err := query.Scan(&allEngineIDsInDB).Error; err != nil {
		if c.log != nil {
			c.log.Error("查询数据库引擎ID失败", "error", err)
		}
		return ids
	}

	for _, engineID := range allEngineIDsInDB {
		eligibility, exists := eligibilityByID[engineID]
		if !exists || eligibility.invalidReason != "" {
			ids = append(ids, engineID)
		}
	}
	return ids
}
