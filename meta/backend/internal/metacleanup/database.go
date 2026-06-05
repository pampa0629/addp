package metacleanup

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

type DatabaseCleaner struct {
	db           *gorm.DB
	systemClient *commonClient.SystemClient
	log          *slog.Logger
}

func NewDatabaseCleaner(db *gorm.DB, systemClient *commonClient.SystemClient, log *slog.Logger) *DatabaseCleaner {
	return &DatabaseCleaner{db: db, systemClient: systemClient, log: log}
}

func (c *DatabaseCleaner) ScanInvalidEngines(ctx context.Context, tenantID uint) ([]models.InvalidEngineDetail, error) {
	var details []models.InvalidEngineDetail
	if c.systemClient == nil {
		if c.log != nil {
			c.log.Warn("SystemClient 未配置，跳过无效引擎检查")
		}
		return details, nil
	}

	allEngines, err := c.systemClient.ListEngines("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取引擎列表失败: %w", err)
	}

	validEngineIDs := make(map[uint]string)
	for _, engine := range allEngines {
		if engine.IsActive {
			validEngineIDs[engine.ID] = engine.Name
		}
	}

	type engineStats struct {
		EngineID      uint
		AffectedNodes int64
		AffectedItems int64
	}

	var stats []engineStats
	query := c.db.Table("meta.meta_node mn").
		Select("mn.engine_id, COUNT(DISTINCT mn.id) as affected_nodes, COUNT(DISTINCT mi.id) as affected_items").
		Joins("LEFT JOIN meta.meta_item mi ON mn.id = mi.node_id").
		Group("mn.engine_id")
	if tenantID > 0 {
		query = query.Where("mn.tenant_id = ?", tenantID)
	}

	if err := query.Scan(&stats).Error; err != nil {
		return nil, fmt.Errorf("查询引擎统计失败: %w", err)
	}

	for _, stat := range stats {
		engineName, exists := validEngineIDs[stat.EngineID]
		if !exists {
			details = append(details, models.InvalidEngineDetail{
				EngineID:      stat.EngineID,
				EngineName:    fmt.Sprintf("Engine#%d", stat.EngineID),
				AffectedNodes: int(stat.AffectedNodes),
				AffectedItems: int(stat.AffectedItems),
				Reason:        "引擎已删除或禁用",
			})
			continue
		}
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
	return details, nil
}

func (c *DatabaseCleaner) ScanOrphanItems(ctx context.Context, tenantID uint) ([]models.OrphanItemDetail, error) {
	var items []models.OrphanItemDetail

	query := `
		SELECT mi.id, mi.name, mi.node_id
		FROM meta.meta_item mi
		LEFT JOIN meta.meta_node mn ON mi.node_id = mn.id
		WHERE mn.id IS NULL
	`
	if tenantID > 0 {
		query += fmt.Sprintf(" AND mi.tenant_id = %d", tenantID)
	}
	query += " LIMIT 100"

	rows, err := c.db.Raw(query).Rows()
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

func (c *DatabaseCleaner) ScanSoftDeleted(ctx context.Context, tenantID uint) (int, int, error) {
	var nodeCount, itemCount int64

	nodeQuery := c.db.Model(&models.MetaNode{}).Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		nodeQuery = nodeQuery.Where("tenant_id = ?", tenantID)
	}
	if err := nodeQuery.Count(&nodeCount).Error; err != nil {
		return 0, 0, err
	}

	itemQuery := c.db.Model(&models.MetaItem{}).Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		itemQuery = itemQuery.Where("tenant_id = ?", tenantID)
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

	orphanSQL := `
		DELETE FROM meta.meta_item
		WHERE id IN (
			SELECT mi.id
			FROM meta.meta_item mi
			LEFT JOIN meta.meta_node mn ON mi.node_id = mn.id
			WHERE mn.id IS NULL
	`
	if tenantID > 0 {
		orphanSQL += fmt.Sprintf(" AND mi.tenant_id = %d", tenantID)
	}
	orphanSQL += ")"

	orphanResult := c.db.Exec(orphanSQL)
	if orphanResult.Error != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("软删除孤儿项失败: %v", orphanResult.Error))
	} else {
		result.DeletedItems += int(orphanResult.RowsAffected)
	}
	return result, nil
}

func (c *DatabaseCleaner) ExecuteHardDelete(ctx context.Context, tenantID uint) (*models.MetaCleanupExecuteResult, error) {
	result := &models.MetaCleanupExecuteResult{}

	nodeQuery := c.db.Unscoped().Where("deleted_at IS NOT NULL")
	if tenantID > 0 {
		nodeQuery = nodeQuery.Where("tenant_id = ?", tenantID)
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
	if err := itemQuery.Delete(&models.MetaItem{}).Error; err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("物理删除项失败: %v", err))
	} else {
		result.DeletedItems = int(itemQuery.RowsAffected)
	}
	return result, nil
}

func (c *DatabaseCleaner) InvalidEngineIDs(ctx context.Context, tenantID uint) []uint {
	var ids []uint
	if c.systemClient == nil {
		if c.log != nil {
			c.log.Warn("SystemClient 未配置，无法获取无效引擎列表")
		}
		return ids
	}

	allEngines, err := c.systemClient.ListEngines("", tenantID)
	if err != nil {
		if c.log != nil {
			c.log.Error("获取引擎列表失败", "error", err)
		}
		return ids
	}

	validEngineIDs := make(map[uint]bool)
	for _, engine := range allEngines {
		if engine.IsActive {
			validEngineIDs[engine.ID] = true
		}
	}

	var allEngineIDsInDB []uint
	query := c.db.Table("meta.meta_node").
		Select("DISTINCT engine_id").
		Where("deleted_at IS NULL")
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if err := query.Scan(&allEngineIDsInDB).Error; err != nil {
		if c.log != nil {
			c.log.Error("查询数据库引擎ID失败", "error", err)
		}
		return ids
	}

	for _, engineID := range allEngineIDsInDB {
		if !validEngineIDs[engineID] {
			ids = append(ids, engineID)
		}
	}
	return ids
}

func (c *DatabaseCleaner) InvalidFingerprints(ctx context.Context, invalidEngineIDs []uint) []string {
	if len(invalidEngineIDs) == 0 {
		return []string{}
	}

	var fingerprints []string
	err := c.db.Table("manager.quick_view").
		Where("engine_id IN ?", invalidEngineIDs).
		Distinct("fingerprint").
		Pluck("fingerprint", &fingerprints).Error
	if err != nil {
		if c.log != nil {
			c.log.Error("查询 manager.quick_view fingerprint 失败", "error", err)
		}
		return []string{}
	}

	if c.log != nil {
		c.log.Debug("获取无效引擎的 fingerprint",
			"engine_ids", invalidEngineIDs,
			"fingerprint_count", len(fingerprints))
	}
	return fingerprints
}

func (c *DatabaseCleaner) FingerprintsByEngine(ctx context.Context, engineID uint) []string {
	var fingerprints []string
	err := c.db.Table("manager.quick_view").
		Where("engine_id = ?", engineID).
		Distinct("fingerprint").
		Pluck("fingerprint", &fingerprints).Error
	if err != nil {
		if c.log != nil {
			c.log.Error("查询 manager.quick_view fingerprint 失败", "engine_id", engineID, "error", err)
		}
		return []string{}
	}
	return fingerprints
}
