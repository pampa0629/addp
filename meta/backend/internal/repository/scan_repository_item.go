package repository

import (
	"fmt"
	"time"

	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"

	"gorm.io/gorm"
)

// UpsertItem 创建或更新数据项
// 使用 fingerprint 作为唯一标识
func (r *ScanRepository) UpsertItem(
	tenantID, engineID uint,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
	rowCount, sizeBytes *int64,
	dataUpdated *time.Time,
) (*models.MetaItem, error) {
	return r.UpsertItemWithDepth(
		tenantID, engineID, node, itemType, name, fullName,
		attrs, rowCount, sizeBytes, dataUpdated, models.ScannedDepthDeep,
	)
}

func (r *ScanRepository) UpsertItemWithDepth(
	tenantID, engineID uint,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
	rowCount, sizeBytes *int64,
	dataUpdated *time.Time,
	scanDepth string,
) (*models.MetaItem, error) {
	return r.UpsertItemSelective(
		tenantID, engineID, node, itemType, name, fullName,
		attrs, rowCount, sizeBytes, dataUpdated, scanDepth,
	)
}

func (r *ScanRepository) UpdateItemByIDWithDepth(
	tenantID, itemID, engineID uint,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
	rowCount, sizeBytes *int64,
	dataUpdated *time.Time,
	scanDepth string,
) (*models.MetaItem, error) {
	if itemID == 0 {
		return nil, fmt.Errorf("item_id is required")
	}
	if node == nil {
		return nil, fmt.Errorf("parent node is required")
	}

	var item models.MetaItem
	if err := r.db.Where("tenant_id = ? AND id = ?", tenantID, itemID).First(&item).Error; err != nil {
		return nil, err
	}

	attrs = metaattr.Normalize(attrs)
	now := time.Now()
	scannedDepth := mergeScannedDepth(item.ScannedDepth, scanDepth)
	updates := map[string]interface{}{
		"engine_id":       engineID,
		"node_id":         node.ID,
		"item_type":       itemType,
		"name":            name,
		"full_name":       fullName,
		"row_count":       rowCount,
		"size_bytes":      sizeBytes,
		"data_updated_at": dataUpdated,
		"scanned_at":      &now,
		"scanned_depth":   scannedDepth,
		"attributes":      attrs,
		"deleted_at":      nil,
	}
	if err := r.db.Model(&item).Updates(updates).Error; err != nil {
		return nil, err
	}

	item.EngineID = engineID
	item.NodeID = node.ID
	item.ItemType = itemType
	item.Name = name
	item.FullName = fullName
	item.RowCount = rowCount
	item.SizeBytes = sizeBytes
	item.DataUpdatedAt = dataUpdated
	item.ScannedAt = &now
	item.ScannedDepth = scannedDepth
	item.Attributes = attrs
	item.DeletedAt = gorm.DeletedAt{}
	return &item, nil
}

// UpsertItemSelective 选择性更新数据项
// 当 attrs 为 nil 时，不更新 attributes 字段（用于 basic 扫描保留 deep 扫描的元数据）
func (r *ScanRepository) UpsertItemSelective(
	tenantID, engineID uint,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
	rowCount, sizeBytes *int64,
	dataUpdated *time.Time,
	scanDepth string,
) (*models.MetaItem, error) {
	attrs = metaattr.Normalize(attrs)
	scannedDepth := mergeScannedDepth(models.ScannedDepthNone, scanDepth)

	// 生成数据指纹
	fingerprint, err := r.generateFingerprint(engineID, node, itemType, name, fullName, attrs)
	if err != nil {
		return nil, err
	}

	var item models.MetaItem
	// 使用 fingerprint 查找记录（唯一索引），包括软删除的记录
	err = r.db.Unscoped().Where("fingerprint = ?", fingerprint).First(&item).Error

	if err == gorm.ErrRecordNotFound {
		// 创建新记录
		now := time.Now()
		item = models.MetaItem{
			TenantID:      tenantID,
			EngineID:      engineID,
			NodeID:        node.ID,
			ItemType:      itemType,
			Name:          name,
			FullName:      fullName,
			Fingerprint:   fingerprint,
			Attributes:    models.JSONMap{},
			RowCount:      rowCount,
			SizeBytes:     sizeBytes,
			DataUpdatedAt: dataUpdated,
			ScannedAt:     &now,
			ScannedDepth:  scannedDepth,
		}
		if attrs != nil {
			item.Attributes = attrs
		}

		if err := r.db.Create(&item).Error; err != nil {
			return nil, err
		}

		return &item, nil
	} else if err != nil {
		return nil, err
	}

	// 更新已有记录（包括恢复软删除的记录）
	now := time.Now()
	scannedDepth = mergeScannedDepth(item.ScannedDepth, scanDepth)
	updates := map[string]interface{}{
		"node_id":         node.ID, // 允许 node_id 变化（数据移动）
		"item_type":       itemType,
		"name":            name,
		"full_name":       fullName,
		"row_count":       rowCount,
		"size_bytes":      sizeBytes,
		"data_updated_at": dataUpdated,
		"scanned_at":      &now,
		"scanned_depth":   scannedDepth,
		"deleted_at":      nil, // 恢复软删除的记录
	}

	// 只有当 attrs 不为 nil 时才更新 attributes（basic 扫描时保留已有的 deep 元数据）
	if attrs != nil {
		updates["attributes"] = attrs
		item.Attributes = attrs
	}

	if err := r.db.Unscoped().Model(&item).Updates(updates).Error; err != nil {
		return nil, err
	}

	// 更新内存中的对象
	item.NodeID = node.ID
	item.ItemType = itemType
	item.Name = name
	item.FullName = fullName
	item.RowCount = rowCount
	item.SizeBytes = sizeBytes
	item.DataUpdatedAt = dataUpdated
	item.ScannedAt = &now
	item.ScannedDepth = scannedDepth
	item.DeletedAt = gorm.DeletedAt{}

	return &item, nil
}

// generateFingerprint 生成数据指纹
func (r *ScanRepository) generateFingerprint(
	engineID uint,
	node *models.MetaNode,
	itemType, name, fullName string,
	attrs models.JSONMap,
) (string, error) {
	if attrs != nil {
		// 对象存储：使用 bucket/path+name
		if bucket := commonJSON.String(attrs, "storage", "bucket"); bucket != "" {
			fileName := commonJSON.String(attrs, "storage", "name")
			dir := commonJSON.String(attrs, "storage", "path")

			// 两步计算指纹：先拼接 full_name，再计算指纹
			fullName := commonModels.JoinObjectPath(bucket, dir, fileName)
			return commonModels.GenerateItemFingerprint(engineID, fullName), nil
		}

		// 关系数据库：使用 schema.table
		if schema := commonJSON.String(attrs, "storage", "schema_name"); schema != "" {
			// 两步计算指纹：先拼接 full_name，再计算指纹
			fullName := fmt.Sprintf("%s.%s", schema, name)
			return commonModels.GenerateItemFingerprint(engineID, fullName), nil
		}

		// 其他类型：使用 fullName
		return commonModels.GenerateItemFingerprint(engineID, fullName), nil
	}

	// 无 attributes，需要查找已有记录获取指纹
	var existingItem models.MetaItem
	err := r.db.Where("engine_id = ? AND node_id = ? AND item_type = ? AND name = ?",
		engineID, node.ID, itemType, name).First(&existingItem).Error

	if err == nil {
		return existingItem.Fingerprint, nil
	}

	// 如果找不到已有记录且 attrs 为 nil，无法生成 fingerprint
	return "", fmt.Errorf("cannot generate fingerprint without attributes for new item")
}

// SoftDeleteItemsNotInList 软删除不在列表中的数据项（用于增量扫描）
func (r *ScanRepository) SoftDeleteItemsNotInList(nodeID uint, keepFingerprintList []string) error {
	if len(keepFingerprintList) == 0 {
		// 没有要保留的项，软删除所有
		return r.db.Model(&models.MetaItem{}).
			Where("node_id = ? AND deleted_at IS NULL", nodeID).
			Update("deleted_at", time.Now()).Error
	}

	// 软删除不在保留列表中的项
	return r.db.Model(&models.MetaItem{}).
		Where("node_id = ? AND deleted_at IS NULL", nodeID).
		Where("fingerprint NOT IN ?", keepFingerprintList).
		Update("deleted_at", time.Now()).Error
}

// GetItemsByNode 获取节点下的所有数据项
func (r *ScanRepository) GetItemsByNode(nodeID uint) ([]*models.MetaItem, error) {
	var items []*models.MetaItem
	err := r.db.Where("node_id = ? AND deleted_at IS NULL", nodeID).Find(&items).Error
	return items, err
}

func (r *ScanRepository) GetItemsByNodeAndType(tenantID, engineID, nodeID uint, itemType string) ([]models.MetaItem, error) {
	var items []models.MetaItem
	err := r.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, engineID, nodeID, itemType).Find(&items).Error
	return items, err
}

func (r *ScanRepository) GetItemsByNodeAndTypeMap(tenantID, engineID, nodeID uint, itemType string) map[string]*models.MetaItem {
	items, err := r.GetItemsByNodeAndType(tenantID, engineID, nodeID, itemType)
	if err != nil {
		return map[string]*models.MetaItem{}
	}
	result := make(map[string]*models.MetaItem, len(items))
	for i := range items {
		result[items[i].Name] = &items[i]
	}
	return result
}

func (r *ScanRepository) FindItemByFullName(tenantID, engineID uint, fullName string) (*models.MetaItem, bool, error) {
	var item models.MetaItem
	err := r.db.Where("tenant_id = ? AND engine_id = ? AND full_name = ? AND deleted_at IS NULL", tenantID, engineID, fullName).First(&item).Error
	if err == nil {
		return &item, true, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	return nil, false, err
}

func (r *ScanRepository) GetItemByID(tenantID, itemID uint) (*models.MetaItem, error) {
	var item models.MetaItem
	if err := r.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ScanRepository) GetItemByFingerprint(tenantID uint, fingerprint string) (*models.MetaItem, error) {
	var item models.MetaItem
	if err := r.db.Where("tenant_id = ? AND fingerprint = ? AND deleted_at IS NULL", tenantID, fingerprint).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}
