package service

import (
	"fmt"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"

	"gorm.io/gorm"
)

// ScanRepository 数据访问层，负责 meta_node 和 meta_item 的 CRUD 操作
type ScanRepository struct {
	db *gorm.DB
}

// NewScanRepository 创建 Repository 实例
func NewScanRepository(db *gorm.DB) *ScanRepository {
	return &ScanRepository{db: db}
}

// ============================================================================
// 辅助函数
// ============================================================================

// composeNodePath 组合节点路径（层级结构：parent_path/node_id）
func composeNodePath(nodeID uint, parent *models.MetaNode) string {
	current := fmt.Sprintf("%d", nodeID)
	if parent == nil || parent.Path == "" {
		if parent == nil {
			return current
		}
		return fmt.Sprintf("%d/%s", parent.ID, current)
	}
	return fmt.Sprintf("%s/%s", parent.Path, current)
}

// composeNodeFullName 组合节点全名（parent.fullname + separator + name）
func composeNodeFullName(name string, parent *models.MetaNode, separator string) string {
	if parent == nil || parent.FullName == "" {
		return name
	}
	if separator == "" {
		separator = "."
	}
	return fmt.Sprintf("%s%s%s", parent.FullName, separator, name)
}

// ============================================================================
// Node CRUD 操作
// ============================================================================

// UpsertNode 创建或更新节点
// 使用 (tenant_id, engine_id, parent_node_id, name) 作为唯一标识（不包含 node_type）
func (r *ScanRepository) UpsertNode(
	tenantID, engineID uint,
	parent *models.MetaNode,
	nodeType, name, fullName string,
	attrs models.JSONMap,
) (*models.MetaNode, error) {
	var parentID *uint
	depth := 1
	if parent != nil {
		parentID = &parent.ID
		depth = parent.Depth + 1
	}

	// 查询现有节点（包括软删除的记录）
	// 【优化】去掉 node_type 条件，简化唯一性约束为 (tenant_id, engine_id, parent_node_id, name)
	query := r.db.Unscoped().Where("engine_id = ? AND tenant_id = ? AND name = ?", engineID, tenantID, name)
	if parentID == nil {
		query = query.Where("parent_node_id IS NULL")
	} else {
		query = query.Where("parent_node_id = ?", *parentID)
	}

	var node models.MetaNode
	err := query.First(&node).Error

	if err == gorm.ErrRecordNotFound {
		// 创建新节点
		node = models.MetaNode{
			TenantID:     tenantID,
			EngineID:     engineID,
			ParentNodeID: parentID,
			NodeType:     nodeType,
			Name:         name,
			Depth:        depth,
			ScanStatus:   "未扫描",
			Attributes:   models.JSONMap{},
		}
		if fullName != "" {
			node.FullName = fullName
		}
		if attrs != nil {
			node.Attributes = attrs
		}

		if err := r.db.Create(&node).Error; err != nil {
			return nil, err
		}

		// 更新 path 和 full_name
		path := composeNodePath(node.ID, parent)
		update := map[string]interface{}{"path": path}
		node.Path = path

		if node.FullName == "" {
			node.FullName = composeNodeFullName(node.Name, parent, ".")
			update["full_name"] = node.FullName
		}

		if err := r.db.Model(&node).Updates(update).Error; err != nil {
			return nil, err
		}

		return &node, nil
	} else if err != nil {
		return nil, err
	}

	// 更新现有节点
	updates := map[string]interface{}{
		"deleted_at": nil, // 恢复软删除的节点
	}

	// 【优化】如果 node_type 不同，也更新它（支持节点类型变更）
	if node.NodeType != nodeType {
		updates["node_type"] = nodeType
		node.NodeType = nodeType
	}

	if node.Depth != depth {
		updates["depth"] = depth
		node.Depth = depth
	}

	path := composeNodePath(node.ID, parent)
	if node.Path != path {
		updates["path"] = path
		node.Path = path
	}

	expectedFullName := fullName
	if expectedFullName == "" {
		expectedFullName = composeNodeFullName(name, parent, ".")
	}
	if node.FullName != expectedFullName {
		updates["full_name"] = expectedFullName
		node.FullName = expectedFullName
	}

	if attrs != nil && len(attrs) > 0 {
		updates["attributes"] = attrs
		node.Attributes = attrs
	}

	if len(updates) > 0 {
		if err := r.db.Unscoped().Model(&node).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return &node, nil
}

// ResetNodeState 重置节点状态（开始扫描）
func (r *ScanRepository) ResetNodeState(node *models.MetaNode, status string) error {
	now := time.Now()
	update := map[string]interface{}{
		"scan_status": status,
		"scan_error":  "", // 清除错误信息
	}

	if status == "扫描中" {
		update["scanned_at"] = now
	}
	return r.db.Model(node).Updates(update).Error
}

// FinalizeNodeState 最终化节点状态（扫描完成）
func (r *ScanRepository) FinalizeNodeState(
	node *models.MetaNode,
	status string,
	itemCount int,
	totalSize int64,
	errMsg string,
) error {
	update := map[string]interface{}{
		"scan_status":      status,
		"item_count":       itemCount,
		"total_size_bytes": totalSize,
		"scan_error":       errMsg,
	}

	if status == "已扫描" {
		update["scanned_at"] = time.Now()
	}
	return r.db.Model(node).Updates(update).Error
}

// HardDeleteItemsByNode 硬删除节点下的所有数据项
func (r *ScanRepository) HardDeleteItemsByNode(nodeID uint) error {
	return r.db.Unscoped().Where("node_id = ?", nodeID).Delete(&models.MetaItem{}).Error
}

// HardDeleteDescendantNodes 硬删除子孙节点
func (r *ScanRepository) HardDeleteDescendantNodes(node *models.MetaNode) error {
	if node.Path == "" {
		return nil
	}
	prefix := fmt.Sprintf("%s/%%", node.Path)
	return r.db.Unscoped().
		Where("path LIKE ?", prefix).
		Where("id <> ?", node.ID).
		Delete(&models.MetaNode{}).Error
}

// ============================================================================
// Item CRUD 操作
// ============================================================================

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
	return r.UpsertItemSelective(
		tenantID, engineID, node, itemType, name, fullName,
		attrs, rowCount, sizeBytes, dataUpdated,
	)
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
) (*models.MetaItem, error) {
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
	updates := map[string]interface{}{
		"node_id":         node.ID, // 允许 node_id 变化（数据移动）
		"full_name":       fullName,
		"row_count":       rowCount,
		"size_bytes":      sizeBytes,
		"data_updated_at": dataUpdated,
		"scanned_at":      &now,
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
	item.FullName = fullName
	item.RowCount = rowCount
	item.SizeBytes = sizeBytes
	item.DataUpdatedAt = dataUpdated
	item.ScannedAt = &now
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
		// 对象存储：使用 bucket/path
		if bucket, ok := attrs["bucket"].(string); ok {
			path := ""
			if p, ok := attrs["path"].(string); ok {
				path = p
			} else if rp, ok := attrs["relative_path"].(string); ok {
				path = rp
			}
			return commonModels.GenerateObjectFingerprint(engineID, bucket, path), nil
		}

		// 关系数据库：使用 schema.table
		if schema, ok := attrs["schema_name"].(string); ok {
			return commonModels.GenerateTableFingerprint(engineID, schema, name), nil
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

// GetNodeByID 根据 ID 获取节点
func (r *ScanRepository) GetNodeByID(nodeID uint) (*models.MetaNode, error) {
	var node models.MetaNode
	err := r.db.Where("id = ? AND deleted_at IS NULL", nodeID).First(&node).Error
	if err != nil {
		return nil, err
	}
	return &node, nil
}

// GetChildNodes 获取子节点列表
func (r *ScanRepository) GetChildNodes(parentID uint) ([]*models.MetaNode, error) {
	var nodes []*models.MetaNode
	err := r.db.Where("parent_node_id = ? AND deleted_at IS NULL", parentID).
		Order("node_type, name").
		Find(&nodes).Error
	return nodes, err
}
