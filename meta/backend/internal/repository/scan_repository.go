package repository

import (
	"errors"
	"fmt"
	"sync"
	"time"

	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"

	"gorm.io/gorm"
)

// ScanRepository 数据访问层，负责 meta_node 和 meta_item 的 CRUD 操作
type ScanRepository struct {
	db *gorm.DB
}

var (
	nodeUpsertLocksMu sync.Mutex
	nodeUpsertLocks   = map[string]*sync.Mutex{}
)

// NewScanRepository 创建 Repository 实例
func NewScanRepository(db *gorm.DB) *ScanRepository {
	return &ScanRepository{db: db}
}

func lockNodeUpsert(tenantID, engineID uint, parentID *uint, nodeType, name string, fullName *string) func() {
	key := fmt.Sprintf("hierarchy:%d:%d:%s:%s:%s", tenantID, engineID, nodeType, parentKey(parentID), name)
	if fullName != nil {
		key = fmt.Sprintf("semantic:%d:%d:%s:%s", tenantID, engineID, nodeType, *fullName)
	}

	nodeUpsertLocksMu.Lock()
	lock := nodeUpsertLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		nodeUpsertLocks[key] = lock
	}
	nodeUpsertLocksMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func parentKey(parentID *uint) string {
	if parentID == nil {
		return ""
	}
	return fmt.Sprintf("%d", *parentID)
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
	nodeType, name string,
	fullName *string, // nil = 自动计算，非 nil = 显式指定（空字符串也有效）
	attrs models.JSONMap,
) (*models.MetaNode, error) {
	var parentID *uint
	depth := 1
	if parent != nil {
		parentID = &parent.ID
		depth = parent.Depth + 1
	}

	unlock := lockNodeUpsert(tenantID, engineID, parentID, nodeType, name, fullName)
	defer unlock()

	// 查询现有节点（包括软删除的记录）
	// 显式 full_name 表示调用方已经给出资源语义路径，优先用它做幂等键。
	// 这能覆盖 NFS 根节点显示名变化（"" -> "."）后刷新出两套相同目录树的问题。
	query := r.db.Unscoped().Where("engine_id = ? AND tenant_id = ? AND node_type = ?", engineID, tenantID, nodeType)
	if fullName != nil {
		query = query.Where("full_name = ?", *fullName)
	} else {
		query = query.Where("name = ?", name)
	}
	if parentID == nil {
		query = query.Where("parent_node_id IS NULL")
	} else if fullName == nil {
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
			ScanStatus:   "pending",
			Attributes:   models.JSONMap{},
		}
		if fullName != nil {
			node.FullName = *fullName
		} else {
			node.FullName = composeNodeFullName(node.Name, parent, ".")
		}
		if attrs != nil {
			node.Attributes = attrs
		}

		if err := r.db.Create(&node).Error; err != nil {
			if !errors.Is(err, gorm.ErrDuplicatedKey) {
				return nil, err
			}
			if existing, findErr := r.findNodeBySemanticOrHierarchy(tenantID, engineID, parentID, nodeType, name, fullName); findErr == nil {
				node = *existing
			} else {
				return nil, err
			}
		}

		// 更新 path 和 full_name
		path := composeNodePath(node.ID, parent)
		update := map[string]interface{}{
			"path":      path,
			"full_name": node.FullName,
		}
		node.Path = path

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

	if node.ParentNodeID == nil && parentID != nil || node.ParentNodeID != nil && (parentID == nil || *node.ParentNodeID != *parentID) {
		updates["parent_node_id"] = parentID
		node.ParentNodeID = parentID
	}

	if node.Name != name {
		updates["name"] = name
		node.Name = name
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

	expectedFullName := composeNodeFullName(name, parent, ".")
	if fullName != nil {
		expectedFullName = *fullName
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

func (r *ScanRepository) findNodeBySemanticOrHierarchy(
	tenantID, engineID uint,
	parentID *uint,
	nodeType, name string,
	fullName *string,
) (*models.MetaNode, error) {
	query := r.db.Unscoped().Where("engine_id = ? AND tenant_id = ? AND node_type = ?", engineID, tenantID, nodeType)
	if fullName != nil {
		query = query.Where("full_name = ?", *fullName)
	} else {
		query = query.Where("name = ?", name)
	}
	if parentID == nil {
		query = query.Where("parent_node_id IS NULL")
	} else if fullName == nil {
		query = query.Where("parent_node_id = ?", *parentID)
	}

	var node models.MetaNode
	if err := query.First(&node).Error; err != nil {
		return nil, err
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

	if status == "running" {
		update["scanned_at"] = now
	}
	return r.db.Model(node).Updates(update).Error
}

func mergeScannedDepth(current, requested string) string {
	if requested == models.ScannedDepthDeep {
		return models.ScannedDepthDeep
	}
	if current == models.ScannedDepthDeep {
		return models.ScannedDepthDeep
	}
	if requested == models.ScannedDepthBasic {
		return models.ScannedDepthBasic
	}
	if current == models.ScannedDepthBasic {
		return models.ScannedDepthBasic
	}
	return models.ScannedDepthNone
}

// FinalizeNodeState 最终化节点状态（扫描完成）
func (r *ScanRepository) FinalizeNodeState(
	node *models.MetaNode,
	status string,
	itemCount int,
	totalSize int64,
	errMsg string,
) error {
	return r.FinalizeNodeStateWithDepth(node, status, itemCount, totalSize, errMsg, "")
}

func (r *ScanRepository) FinalizeNodeStateWithDepth(
	node *models.MetaNode,
	status string,
	itemCount int,
	totalSize int64,
	errMsg string,
	scanDepth string,
) error {
	update := map[string]interface{}{
		"scan_status":      status,
		"item_count":       itemCount,
		"total_size_bytes": totalSize,
		"scan_error":       errMsg,
	}

	if status == "completed" {
		update["scanned_at"] = time.Now()
		update["scanned_depth"] = mergeScannedDepth(node.ScannedDepth, scanDepth)
	}
	return r.db.Model(node).Updates(update).Error
}

// HardDeleteItemsByNode 硬删除节点下的所有数据项
func (r *ScanRepository) HardDeleteItemsByNode(nodeID uint) error {
	return r.db.Unscoped().Where("node_id = ?", nodeID).Delete(&models.MetaItem{}).Error
}

// HardDeleteDescendantNodes 硬删除子孙节点及其下的所有数据项
func (r *ScanRepository) HardDeleteDescendantNodes(node *models.MetaNode) error {
	if node.Path == "" {
		return nil
	}
	prefix := fmt.Sprintf("%s/%%", node.Path)

	// 先找出所有子孙节点 ID
	var descendantIDs []uint
	if err := r.db.Unscoped().Model(&models.MetaNode{}).
		Where("path LIKE ?", prefix).
		Where("id <> ?", node.ID).
		Pluck("id", &descendantIDs).Error; err != nil {
		return err
	}

	if len(descendantIDs) == 0 {
		return nil
	}

	// 级联删除这些节点下的所有 items
	if err := r.db.Unscoped().
		Where("node_id IN ?", descendantIDs).
		Delete(&models.MetaItem{}).Error; err != nil {
		return err
	}

	// 删除子孙节点
	return r.db.Unscoped().
		Where("id IN ?", descendantIDs).
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
