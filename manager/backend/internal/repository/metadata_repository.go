package repository

import (
	"errors"
	"fmt"
	"strings"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	pq "github.com/lib/pq"
	"gorm.io/gorm"
)

var ErrMetadataSchemaMissing = errors.New("meta schema not initialized")

type MetadataRepository struct {
	db *gorm.DB
}

func NewMetadataRepository(db *gorm.DB) *MetadataRepository {
	return &MetadataRepository{
		db: db,
	}
}

func isUndefinedTableError(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "42P01"
	}
	return false
}

// ListScannedNodesAndItems 获取指定资源的顶层节点、子节点和条目
func (r *MetadataRepository) ListScannedNodesAndItems(engineID uint, metaClient *commonClient.MetaClient) ([]models.MetaNodeLite, []models.MetaNodeLite, []models.MetaItemLite, error) {
	if engineID == 0 {
		return nil, nil, nil, fmt.Errorf("resourceID is required")
	}

	if metaClient == nil {
		return nil, nil, nil, fmt.Errorf("meta client not initialized, cannot query metadata")
	}

	// 通过 Meta API 获取元数据树
	tree, err := metaClient.GetMetadataTree(engineID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get metadata tree from Meta API: %w", err)
	}

	// 将 commonModels.MetaNode/MetaItem 转换为 models.MetaNodeLite/MetaItemLite
	topNodes := make([]models.MetaNodeLite, len(tree.TopNodes))
	for i, node := range tree.TopNodes {
		topNodes[i] = convertMetaNodeToLite(node)
	}

	childNodes := make([]models.MetaNodeLite, len(tree.ChildNodes))
	for i, node := range tree.ChildNodes {
		childNodes[i] = convertMetaNodeToLite(node)
	}

	items := make([]models.MetaItemLite, len(tree.Items))
	for i, item := range tree.Items {
		items[i] = convertMetaItemToLite(item)
	}

	return topNodes, childNodes, items, nil
}

// GetObjectMetadataItem 获取对象存储路径对应的元数据项记录
func (r *MetadataRepository) GetObjectMetadataItem(engineID uint, bucketName, objectPath string, metaClient *commonClient.MetaClient) (*models.MetaItemLite, error) {
	if metaClient == nil {
		return nil, fmt.Errorf("meta client not initialized, cannot query metadata")
	}

	catalogPath := catalogPathFromParts(bucketName, objectPath)
	item, err := metaClient.GetItemByCatalogPath(engineID, catalogPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata snapshot from Meta API: %w", err)
	}
	if item == nil {
		return nil, gorm.ErrRecordNotFound
	}

	lite := convertMetaItemToLite(*item)
	return &lite, nil
}

// GetObjectMetadataNode 获取对象存储节点（bucket/prefix）的元数据
func (r *MetadataRepository) GetObjectMetadataNode(engineID uint, bucketName, relativePath string, metaClient *commonClient.MetaClient) (*models.MetaNodeLite, error) {
	if metaClient == nil {
		return nil, fmt.Errorf("meta client not initialized, cannot query metadata")
	}

	catalogPath := catalogPathFromParts(bucketName, relativePath)
	node, err := metaClient.GetNodeByCatalogPath(engineID, catalogPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get node metadata from Meta API: %w", err)
	}
	if node == nil {
		return nil, gorm.ErrRecordNotFound
	}

	lite := convertMetaNodeToLite(*node)
	return &lite, nil
}

// GetNodeByName 根据资源ID和节点名称获取节点信息
func (r *MetadataRepository) GetNodeByName(engineID uint, nodeName string, metaClient *commonClient.MetaClient) (*models.MetaNodeLite, error) {
	if metaClient == nil {
		return nil, fmt.Errorf("meta client not initialized, cannot query metadata")
	}

	node, err := metaClient.GetNodeByCatalogPath(engineID, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get node by name from Meta API: %w", err)
	}

	lite := convertMetaNodeToLite(*node)
	return &lite, nil
}

func catalogPathFromParts(parts ...string) string {
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			trimmed = append(trimmed, part)
		}
	}
	return strings.Join(trimmed, "/")
}

// GetChildNodes 获取节点的直接子节点
func (r *MetadataRepository) GetChildNodes(parentNodeID uint, metaClient *commonClient.MetaClient) ([]models.MetaNodeLite, error) {
	if metaClient == nil {
		return nil, fmt.Errorf("meta client not initialized, cannot query metadata")
	}

	// 通过 Meta API 获取子节点
	nodes, err := metaClient.GetNodeChildren(parentNodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get child nodes from Meta API: %w", err)
	}

	lites := make([]models.MetaNodeLite, len(nodes))
	for i, node := range nodes {
		lites[i] = convertMetaNodeToLite(node)
	}

	return lites, nil
}

// GetNodeItems 获取节点下的所有子项（表/对象）
func (r *MetadataRepository) GetNodeItems(nodeID uint, metaClient *commonClient.MetaClient) ([]models.MetaItemLite, error) {
	if metaClient == nil {
		return nil, fmt.Errorf("meta client not initialized, cannot query metadata")
	}

	// 通过 Meta API 获取节点下的项目
	items, err := metaClient.GetNodeItems(nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node items from Meta API: %w", err)
	}

	lites := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		lites[i] = convertMetaItemToLite(item)
	}

	return lites, nil
}

// convertMetaNodeToLite 将 commonModels.MetaNode 转换为 models.MetaNodeLite
func convertMetaNodeToLite(node commonModels.MetaNode) models.MetaNodeLite {
	return models.MetaNodeLite{
		ID:             node.ID,
		EngineID:       node.EngineID,
		ParentNodeID:   node.ParentNodeID,
		NodeType:       node.NodeType,
		Name:           node.Name,
		FullName:       node.FullName,
		Path:           node.Path,
		Depth:          node.Depth,
		LastScanAt:     node.LastScanAt,
		ItemCount:      node.ItemCount,
		TotalSizeBytes: node.TotalSizeBytes,
		Attributes:     node.Attributes,
	}
}

// convertMetaItemToLite 将 commonModels.MetaItem 转换为 models.MetaItemLite
func convertMetaItemToLite(item commonModels.MetaItem) models.MetaItemLite {
	return models.MetaItemLite{
		ID:              item.ID,
		EngineID:        item.EngineID,
		NodeID:          item.NodeID,
		ItemType:        item.ItemType,
		Name:            item.Name,
		FullName:        item.FullName,
		RowCount:        item.RowCount,
		SizeBytes:       item.SizeBytes,
		ObjectSizeBytes: item.ObjectSizeBytes,
		LastModifiedAt:  item.DataUpdatedAt,
		Attributes:      item.Attributes,
	}
}
