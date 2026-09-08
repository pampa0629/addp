package metacleanup

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	commonClient "github.com/addp/common/client"
	engineselection "github.com/addp/common/engine/selection"
	commonModels "github.com/addp/common/models"
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
		case !engineselection.IsSelectionOption(engine):
			eligibility.invalidReason = "引擎已禁用"
		case !engineselection.HasStorageCapability(engine):
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
	if c.systemClient == nil && scope.EngineID == 0 {
		if c.log != nil {
			c.log.Warn("SystemClient 未配置，跳过无效引擎检查")
		}
		return details, nil
	}

	var eligibilityByID map[uint]metaEngineEligibility
	if scope.EngineID == 0 {
		allEngines, err := c.systemClient.WithTenantID(tenantID).ListEngines(ctx)
		if err != nil {
			return nil, fmt.Errorf("获取引擎列表失败: %w", err)
		}
		eligibilityByID = metaEngineEligibilityByID(allEngines)
	}

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
		if scope.EngineID > 0 {
			details = append(details, models.InvalidEngineDetail{
				EngineID: stat.EngineID, EngineName: fmt.Sprintf("Engine#%d", stat.EngineID),
				AffectedNodes: int(stat.AffectedNodes), AffectedItems: int(stat.AffectedItems), Reason: "引擎正在删除",
			})
			continue
		}
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
	itemIDs := c.hardDeleteEligibleItemIDs(ctx, tenantID, scope, nil)
	itemCount, err := c.countCandidateIDs(ctx, itemIDs)
	if err != nil {
		return 0, 0, err
	}
	nodeCount, err := c.countCandidateIDs(ctx, c.hardDeleteEligibleNodeIDs(ctx, tenantID, scope, nil, itemIDs))
	if err != nil {
		return 0, 0, err
	}
	return nodeCount, itemCount, nil
}

func (c *DatabaseCleaner) ScanRetainedLineageReferencesWithScope(ctx context.Context, tenantID uint, scope CleanupScope) (int, int, error) {
	baseItems, err := c.countCandidateIDs(ctx, c.hardDeleteBaseItemIDs(ctx, tenantID, scope, nil))
	if err != nil {
		return 0, 0, err
	}
	eligibleItemIDs := c.hardDeleteEligibleItemIDs(ctx, tenantID, scope, nil)
	eligibleItems, err := c.countCandidateIDs(ctx, eligibleItemIDs)
	if err != nil {
		return 0, 0, err
	}
	baseNodes, err := c.countCandidateIDs(ctx, c.hardDeleteBaseNodeIDs(ctx, tenantID, scope, nil))
	if err != nil {
		return 0, 0, err
	}
	eligibleNodes, err := c.countCandidateIDs(ctx, c.hardDeleteEligibleNodeIDs(ctx, tenantID, scope, nil, eligibleItemIDs))
	if err != nil {
		return 0, 0, err
	}
	return baseNodes - eligibleNodes, baseItems - eligibleItems, nil
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
	return c.executeHardDelete(ctx, tenantID, scope, nil)
}

func (c *DatabaseCleaner) ExecuteExpiredHardDelete(ctx context.Context, tenantID uint, deletedBefore time.Time) (*models.MetaCleanupExecuteResult, error) {
	return c.executeHardDelete(ctx, tenantID, CleanupScope{}, &deletedBefore)
}

func (c *DatabaseCleaner) executeHardDelete(ctx context.Context, tenantID uint, scope CleanupScope, deletedBefore *time.Time) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}
	baseItems, err := c.countCandidateIDs(ctx, c.hardDeleteBaseItemIDs(ctx, tenantID, scope, deletedBefore))
	if err != nil {
		return nil, fmt.Errorf("统计待物理清理项失败: %w", err)
	}
	baseNodes, err := c.countCandidateIDs(ctx, c.hardDeleteBaseNodeIDs(ctx, tenantID, scope, deletedBefore))
	if err != nil {
		return nil, fmt.Errorf("统计待物理清理节点失败: %w", err)
	}

	itemIDs := c.hardDeleteEligibleItemIDs(ctx, tenantID, scope, deletedBefore)
	eligibleItems, err := c.countCandidateIDs(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("统计可物理清理项失败: %w", err)
	}
	result.RetainedLineageItems = baseItems - eligibleItems

	itemDelete := c.db.WithContext(ctx).Unscoped().Where("id IN (?)", itemIDs).Delete(&models.MetaItem{})
	if itemDelete.Error != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("物理删除项失败: %v", itemDelete.Error))
		result.RetainedReferencedNodes = baseNodes
		return result, nil
	} else {
		result.DeletedItems = int(itemDelete.RowsAffected)
	}

	nodeIDs := c.hardDeleteEligibleNodeIDs(ctx, tenantID, scope, deletedBefore, nil)
	eligibleNodes, err := c.countCandidateIDs(ctx, nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("统计可物理清理节点失败: %w", err)
	}
	result.RetainedReferencedNodes = baseNodes - eligibleNodes
	nodeDelete := c.db.WithContext(ctx).Unscoped().Where("id IN (?)", nodeIDs).Delete(&models.MetaNode{})
	if nodeDelete.Error != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("物理删除节点失败: %v", nodeDelete.Error))
	} else {
		result.DeletedNodes = int(nodeDelete.RowsAffected)
	}
	return result, nil
}

func (c *DatabaseCleaner) hardDeleteBaseItemIDs(ctx context.Context, tenantID uint, scope CleanupScope, deletedBefore *time.Time) *gorm.DB {
	query := c.db.WithContext(ctx).Table("meta.meta_item AS cleanup_item").Select("cleanup_item.id")
	if scope.EngineID == 0 {
		query = query.Where("cleanup_item.deleted_at IS NOT NULL")
	}
	if deletedBefore != nil {
		query = query.Where("cleanup_item.deleted_at < ?", *deletedBefore)
	}
	if tenantID > 0 {
		query = query.Where("cleanup_item.tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		query = query.Where("cleanup_item.engine_id = ?", scope.EngineID)
	}
	return query
}

func (c *DatabaseCleaner) hardDeleteEligibleItemIDs(ctx context.Context, tenantID uint, scope CleanupScope, deletedBefore *time.Time) *gorm.DB {
	return c.hardDeleteBaseItemIDs(ctx, tenantID, scope, deletedBefore).
		Where(`NOT EXISTS (
			SELECT 1 FROM meta.lineage_item_relations relation
			WHERE relation.source_item_id = cleanup_item.id OR relation.target_item_id = cleanup_item.id
		)`).
		Where(`NOT EXISTS (
			SELECT 1 FROM meta.lineage_service_dependencies dependency
			WHERE dependency.source_item_id = cleanup_item.id
		)`).
		Where(`NOT EXISTS (
			SELECT 1 FROM meta.lineage_observations observation
			WHERE observation.source_item_id = cleanup_item.id OR observation.target_item_id = cleanup_item.id
		)`)
}

func (c *DatabaseCleaner) hardDeleteBaseNodeIDs(ctx context.Context, tenantID uint, scope CleanupScope, deletedBefore *time.Time) *gorm.DB {
	query := c.db.WithContext(ctx).Table("meta.meta_node AS cleanup_node").Select("cleanup_node.id")
	if scope.EngineID == 0 {
		query = query.Where("cleanup_node.deleted_at IS NOT NULL")
	}
	if deletedBefore != nil {
		query = query.Where("cleanup_node.deleted_at < ?", *deletedBefore)
	}
	if tenantID > 0 {
		query = query.Where("cleanup_node.tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		query = query.Where("cleanup_node.engine_id = ?", scope.EngineID)
	}
	return query
}

func (c *DatabaseCleaner) hardDeleteEligibleNodeIDs(ctx context.Context, tenantID uint, scope CleanupScope, deletedBefore *time.Time, deletableItemIDs *gorm.DB) *gorm.DB {
	query := c.hardDeleteBaseNodeIDs(ctx, tenantID, scope, deletedBefore)
	if deletableItemIDs == nil {
		return query.Where(`NOT EXISTS (
			SELECT 1 FROM meta.meta_item remaining_item
			WHERE remaining_item.node_id = cleanup_node.id
		)`)
	}
	return query.Where(`NOT EXISTS (
		SELECT 1 FROM meta.meta_item remaining_item
		WHERE remaining_item.node_id = cleanup_node.id
		  AND remaining_item.id NOT IN (?)
	)`, deletableItemIDs)
}

func (c *DatabaseCleaner) countCandidateIDs(ctx context.Context, query *gorm.DB) (int, error) {
	var count int64
	if err := c.db.WithContext(ctx).Table("(?) AS cleanup_candidates", query).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (c *DatabaseCleaner) InvalidEngineIDs(ctx context.Context, tenantID uint) []uint {
	return c.InvalidEngineIDsWithScope(ctx, tenantID, CleanupScope{})
}

func (c *DatabaseCleaner) InvalidEngineIDsWithScope(ctx context.Context, tenantID uint, scope CleanupScope) []uint {
	var ids []uint
	if scope.EngineID > 0 {
		return []uint{scope.EngineID}
	}
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

	var nodeEngineIDs []uint
	query := c.db.Table("meta.meta_node").
		Select("DISTINCT engine_id").
		Order("engine_id")
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		query = query.Where("engine_id = ?", scope.EngineID)
	}
	if err := query.Scan(&nodeEngineIDs).Error; err != nil {
		if c.log != nil {
			c.log.Error("查询数据库引擎ID失败", "error", err)
		}
		return ids
	}
	referencedEngineIDs := make(map[uint]struct{}, len(nodeEngineIDs))
	for _, engineID := range nodeEngineIDs {
		referencedEngineIDs[engineID] = struct{}{}
	}
	if metaScanTaskTableExists(c.db) {
		var taskEngineIDs []uint
		taskQuery := c.db.Model(&models.ScanTask{}).Distinct("engine_id")
		if tenantID > 0 {
			taskQuery = taskQuery.Where("tenant_id = ?", tenantID)
		}
		if err := taskQuery.Pluck("engine_id", &taskEngineIDs).Error; err != nil {
			if c.log != nil {
				c.log.Error("查询扫描任务引擎ID失败", "error", err)
			}
			return ids
		}
		for _, engineID := range taskEngineIDs {
			referencedEngineIDs[engineID] = struct{}{}
		}
	}
	allEngineIDsInDB := make([]uint, 0, len(referencedEngineIDs))
	for engineID := range referencedEngineIDs {
		allEngineIDsInDB = append(allEngineIDsInDB, engineID)
	}
	sort.Slice(allEngineIDsInDB, func(i, j int) bool { return allEngineIDsInDB[i] < allEngineIDsInDB[j] })

	for _, engineID := range allEngineIDsInDB {
		eligibility, exists := eligibilityByID[engineID]
		if !exists || eligibility.invalidReason != "" {
			ids = append(ids, engineID)
		}
	}
	return ids
}

func metaScanTaskTableExists(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	if db.Dialector.Name() == "sqlite" {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM meta.sqlite_master WHERE type = 'table' AND name = 'scan_tasks'").Scan(&count).Error; err != nil {
			return false
		}
		return count > 0
	}
	return db.Migrator().HasTable(&models.ScanTask{})
}
