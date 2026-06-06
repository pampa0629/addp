package repository

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

var (
	nodeUpsertLocksMu sync.Mutex
	nodeUpsertLocks   = map[string]*sync.Mutex{}
)

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

// GetNodeByID 根据 ID 获取节点
func (r *ScanRepository) GetNodeByID(nodeID uint) (*models.MetaNode, error) {
	var node models.MetaNode
	err := r.db.Where("id = ? AND deleted_at IS NULL", nodeID).First(&node).Error
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *ScanRepository) GetNodeByIDForTenant(tenantID, nodeID uint) (*models.MetaNode, error) {
	var node models.MetaNode
	if err := r.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, nodeID).First(&node).Error; err != nil {
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
