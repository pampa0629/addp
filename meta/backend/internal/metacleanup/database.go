package metacleanup

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	commonClient "github.com/addp/common/client"
	engineselection "github.com/addp/common/engine/selection"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

type DatabaseCleaner struct {
	db               *gorm.DB
	systemClient     *commonClient.SystemServiceClient
	log              *slog.Logger
	snapshotTenant   uint
	snapshotEngines  []commonModels.Engine
	snapshotReady    bool
	snapshotObserved map[uint]bool
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

	var eligibilityByID map[uint]metaEngineEligibility
	if scope.EngineID == 0 {
		allEngines, err := c.engineFacts(ctx, tenantID)
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

func (c *DatabaseCleaner) ScanOrphanItems(ctx context.Context, tenantID uint, scope CleanupScope) ([]models.OrphanItemDetail, error) {
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
	if scope.EngineID > 0 {
		query = query.Where("mi.engine_id = ?", scope.EngineID)
	}
	if err := query.Limit(100).Scan(&items).Error; err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Reason = "node_id不存在"
	}
	return items, nil
}

func (c *DatabaseCleaner) ScanExpiredData(ctx context.Context, tenantID uint, thresholdDays int, scope CleanupScope) (int, error) {
	var count int64

	query := c.db.Model(&models.MetaItem{}).
		Where("scanned_at < ?", time.Now().AddDate(0, 0, -thresholdDays))
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if scope.EngineID > 0 {
		query = query.Where("engine_id = ?", scope.EngineID)
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

func (c *DatabaseCleaner) ScanDuplicateFingerprints(ctx context.Context, tenantID uint, scope CleanupScope) (int, error) {
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
	if scope.EngineID > 0 {
		query += fmt.Sprintf(" AND engine_id = %d", scope.EngineID)
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

func (c *DatabaseCleaner) ExecuteSoftDelete(ctx context.Context, tenantID uint, invalidEngineIDs []uint, scope CleanupScope) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}

	if len(invalidEngineIDs) > 0 {
		nodeResult := c.db.Model(&models.MetaNode{}).Where("engine_id IN ?", invalidEngineIDs)
		if tenantID > 0 {
			nodeResult = nodeResult.Where("tenant_id = ?", tenantID)
		}
		if scope.EngineID > 0 {
			nodeResult = nodeResult.Where("engine_id = ?", scope.EngineID)
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
		if scope.EngineID > 0 {
			itemResult = itemResult.Where("engine_id = ?", scope.EngineID)
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

	if scope.EngineID > 0 {
		orphanIDs = orphanIDs.Where("mi.engine_id = ?", scope.EngineID)
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
	return c.ExecuteSoftDelete(ctx, tenantID, []uint{engineID}, CleanupScope{EngineID: engineID})
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

func (c *DatabaseCleaner) InvalidEngineIDs(ctx context.Context, tenantID uint) ([]uint, error) {
	return c.InvalidEngineIDsWithScope(ctx, tenantID, CleanupScope{})
}

func (c *DatabaseCleaner) InvalidEngineIDsWithScope(ctx context.Context, tenantID uint, scope CleanupScope) ([]uint, error) {
	var ids []uint
	if scope.EngineID > 0 {
		return []uint{scope.EngineID}, nil
	}

	allEngines, err := c.engineFacts(ctx, tenantID)
	if err != nil {
		if c.log != nil {
			c.log.Error("获取引擎列表失败", "error", err)
		}
		return nil, err
	}

	eligibilityByID := metaEngineEligibilityByID(allEngines)

	allEngineIDsInDB, err := c.referencedEngineIDs(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	for _, engineID := range allEngineIDsInDB {
		eligibility, exists := eligibilityByID[engineID]
		if !exists || eligibility.invalidReason != "" {
			ids = append(ids, engineID)
		}
	}
	return ids, nil
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

// PrepareEngineFacts freezes exact, authoritative observations before Owner locks.
func (c *DatabaseCleaner) PrepareEngineFacts(ctx context.Context, tenant uint, scope CleanupScope) (*DatabaseCleaner, error) {
	copy := *c
	if scope.EngineID == 0 {
		ids, err := c.referencedEngineIDs(ctx, tenant)
		if err != nil {
			return nil, err
		}
		values, err := c.readEngineFacts(ctx, tenant, ids)
		if err != nil {
			return nil, err
		}
		copy.snapshotTenant = tenant
		copy.snapshotEngines = values
		copy.snapshotReady = true
		copy.snapshotObserved = map[uint]bool{}
		for _, id := range ids {
			copy.snapshotObserved[id] = true
		}
	}
	return &copy, nil
}
func (c *DatabaseCleaner) WithDB(db *gorm.DB) *DatabaseCleaner {
	copy := *c
	copy.db = db
	return &copy
}
func (c *DatabaseCleaner) referencedEngineIDs(ctx context.Context, tenant uint) ([]uint, error) {
	if tenant == 0 {
		return nil, fmt.Errorf("cleanup Tenant required")
	}
	seen := map[uint]bool{}
	tables := []string{"meta.meta_node", "meta.meta_item"}
	if metaScanTaskTableExists(c.db) {
		tables = append(tables, "meta.scan_tasks")
	}
	for _, table := range tables {
		var ids []uint
		if err := c.db.WithContext(ctx).Table(table).Where("tenant_id=?", tenant).Distinct("engine_id").Pluck("engine_id", &ids).Error; err != nil {
			return nil, err
		}
		for _, id := range ids {
			if id == 0 {
				return nil, fmt.Errorf("cleanup Engine identity missing")
			}
			seen[id] = true
		}
	}
	ids := make([]uint, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}
func (c *DatabaseCleaner) readEngineFacts(ctx context.Context, tenant uint, ids []uint) ([]commonModels.Engine, error) {
	if c.systemClient == nil {
		return nil, fmt.Errorf("cleanup Engine authority unavailable")
	}
	values := make([]commonModels.Engine, 0, len(ids))
	for _, id := range ids {
		engine, err := c.systemClient.WithTenantID(tenant).GetEngine(ctx, id)
		if err != nil {
			if code, ok := commonClient.SystemAPIStatusCode(err); ok && code == http.StatusNotFound {
				continue
			}
			return nil, err
		}
		if engine == nil || engine.ID != id || engine.TenantID != nil && *engine.TenantID != tenant {
			return nil, fmt.Errorf("cleanup Engine identity mismatch")
		}
		switch engine.LifecycleState {
		case commonModels.EngineLifecycleActive:
			capabilities, err := engineselection.ParseCapabilities(engine.Capabilities)
			if err != nil || capabilities == nil {
				return nil, fmt.Errorf("cleanup Engine capabilities unavailable")
			}
		case commonModels.EngineLifecycleDisabled, commonModels.EngineLifecycleDeleting, commonModels.EngineLifecycleDeleted:
		default:
			return nil, fmt.Errorf("cleanup Engine lifecycle unavailable")
		}
		values = append(values, *engine)
	}
	return values, nil
}
func (c *DatabaseCleaner) engineFacts(ctx context.Context, tenant uint) ([]commonModels.Engine, error) {
	ids, err := c.referencedEngineIDs(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if c.snapshotReady {
		if c.snapshotTenant != tenant {
			return nil, fmt.Errorf("cleanup Tenant snapshot mismatch")
		}
		for _, id := range ids {
			if !c.snapshotObserved[id] {
				return nil, fmt.Errorf("cleanup Engine scope changed")
			}
		}
		return c.snapshotEngines, nil
	}
	return c.readEngineFacts(ctx, tenant, ids)
}
