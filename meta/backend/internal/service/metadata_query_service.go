package service

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// MetadataQueryService 元数据查询服务
// 提供Manager和Transfer模块的元数据查询接口
type MetadataQueryService struct {
	db             *gorm.DB
	repo           *ScanRepository
	spatialService *SpatialMetadataService
	log            *slog.Logger
}

// NewMetadataQueryService 创建元数据查询服务
func NewMetadataQueryService(db *gorm.DB, spatialService *SpatialMetadataService, log *slog.Logger) *MetadataQueryService {
	return &MetadataQueryService{
		db:             db,
		repo:           NewScanRepository(db),
		spatialService: spatialService,
		log:            log,
	}
}

// ============================================================================
// Transfer 模块查询接口
// ============================================================================

func (s *MetadataQueryService) GetTablesByResource(engineID, tenantID uint) ([]models.MetaItem, error) {
	// 查询该资源下的所有 meta_item（表）
	var items []models.MetaItem

	// 先获取该资源下的所有节点ID（schemas/prefixes）
	var nodeIDs []uint
	err := s.db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Pluck("id", &nodeIDs).Error
	if err != nil {
		return nil, err
	}

	if len(nodeIDs) == 0 {
		return []models.MetaItem{}, nil
	}

	// 查询这些节点下的所有 items（表）
	// 注意：meta_item 表中的字段名是 node_id，不是 parent_node_id
	// 使用 Select 明确选择字段，确保 FullName 被加载
	err = s.db.Select("*").
		Where("tenant_id = ? AND node_id IN (?) AND deleted_at IS NULL", tenantID, nodeIDs).
		Order("name").
		Find(&items).Error
	if err != nil {
		return nil, err
	}

	return items, nil
}

func (s *MetadataQueryService) GetTableFields(engineID uint, tableName string, tenantID uint) ([]string, error) {
	fieldInfos, err := s.GetTableFieldDetails(engineID, tableName, tenantID)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(fieldInfos))
	for _, field := range fieldInfos {
		if field.Name != "" {
			names = append(names, field.Name)
		}
	}
	return names, nil
}

func (s *MetadataQueryService) GetTableFieldDetails(engineID uint, tableName string, tenantID uint) ([]TableFieldInfo, error) {
	// 1. 先获取该资源下的所有节点ID（schema nodes）
	var nodeIDs []uint
	err := s.db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Pluck("id", &nodeIDs).Error
	if err != nil {
		return nil, err
	}

	if len(nodeIDs) == 0 {
		return []TableFieldInfo{}, nil
	}

	// 2. 查询指定表名的 meta_item
	// 支持两种格式：带 schema 的全名（如 "products.items"）和不带 schema 的表名（如 "items"）
	var item models.MetaItem
	err = s.db.Where("tenant_id = ? AND node_id IN (?) AND (name = ? OR full_name = ?) AND deleted_at IS NULL",
		tenantID, nodeIDs, tableName, tableName).
		First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return []TableFieldInfo{}, fmt.Errorf("table '%s' not found in metadata", tableName)
		}
		return nil, err
	}

	// 3. 从 Attributes 中提取字段列表
	// 字段信息存储在 attributes.fields 中，格式为 []map[string]interface{}
	fieldsData, ok := item.Attributes["fields"]
	if !ok {
		return []TableFieldInfo{}, nil
	}

	fieldsList, ok := fieldsData.([]interface{})
	if !ok {
		return []TableFieldInfo{}, fmt.Errorf("invalid fields format in metadata")
	}

	// 4. 构造字段详细信息
	fieldInfos := make([]TableFieldInfo, 0, len(fieldsList))
	for _, fieldData := range fieldsList {
		fieldMap, ok := fieldData.(map[string]interface{})
		if !ok {
			continue
		}

		info := TableFieldInfo{
			Name:            toString(fieldMap["name"]),
			DataType:        toString(fieldMap["data_type"]),
			ColumnType:      toString(fieldMap["column_type"]),
			Comment:         toString(fieldMap["comment"]),
			IsNullable:      toBool(fieldMap["is_nullable"]),
			IsPrimaryKey:    toBool(fieldMap["is_primary_key"]),
			IsUniqueKey:     toBool(fieldMap["is_unique_key"]),
			OrdinalPosition: int(toInt(fieldMap["ordinal_position"])),
		}

		fieldInfos = append(fieldInfos, info)
	}

	return fieldInfos, nil
}

// ============================================================================
// Manager 模块查询接口
// ============================================================================

func (s *MetadataQueryService) GetMetadataTree(tenantID, engineID uint) (*models.MetadataTreeResponse, error) {
	// 查询顶层节点
	var topNodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL", tenantID, engineID).
		Find(&topNodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query top nodes: %w", err)
	}

	// 查询子节点
	var childNodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NOT NULL", tenantID, engineID).
		Find(&childNodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query child nodes: %w", err)
	}

	// 查询所有项
	var items []models.MetaItem
	if err := s.db.Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}

	// 转换为 Lite 模型
	topNodesLite := make([]models.MetaNodeLite, len(topNodes))
	for i, node := range topNodes {
		topNodesLite[i] = convertToMetaNodeLite(node)
	}

	childNodesLite := make([]models.MetaNodeLite, len(childNodes))
	for i, node := range childNodes {
		childNodesLite[i] = convertToMetaNodeLite(node)
	}

	itemsLite := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		itemsLite[i] = convertToMetaItemLite(item)
	}

	return &models.MetadataTreeResponse{
		TopNodes:   topNodesLite,
		ChildNodes: childNodesLite,
		Items:      itemsLite,
	}, nil
}

func (s *MetadataQueryService) GetNodeByPath(tenantID, engineID uint, nodePath string) (*models.MetaNodeLite, error) {
	var node models.MetaNode

	// 尝试按 full_name 查询
	err := s.db.Where("tenant_id = ? AND engine_id = ? AND full_name = ?", tenantID, engineID, nodePath).
		First(&node).Error
	if err == nil {
		result := convertToMetaNodeLite(node)
		return &result, nil
	}

	// 尝试按 name 查询顶层节点
	err = s.db.Where("tenant_id = ? AND engine_id = ? AND name = ? AND parent_node_id IS NULL", tenantID, engineID, nodePath).
		First(&node).Error
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	result := convertToMetaNodeLite(node)
	return &result, nil
}

func (s *MetadataQueryService) GetItemByPath(tenantID, engineID uint, bucketName, objectPath string) (*models.MetaItemLite, error) {
	var item models.MetaItem

	// 构建完整路径
	fullPath := bucketName
	if objectPath != "" {
		fullPath = bucketName + "/" + strings.TrimPrefix(objectPath, "/")
	}

	// 尝试多种路径匹配方式
	err := s.db.Where("tenant_id = ? AND engine_id = ? AND item_type = ?", tenantID, engineID, "object").
		Where("(attributes->>'bucket' = ? AND (attributes->>'path' = ? OR attributes->>'relative_path' = ? OR full_name = ?))",
			bucketName, fullPath, objectPath, fullPath).
		First(&item).Error

	if err != nil {
		return nil, fmt.Errorf("item not found: %w", err)
	}

	result := convertToMetaItemLite(item)
	return &result, nil
}

func (s *MetadataQueryService) GetNodeChildren(tenantID, nodeID uint) ([]models.MetaNodeLite, error) {
	var nodes []models.MetaNode

	// 先查询节点是否存在并验证租户
	var parentNode models.MetaNode
	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, nodeID).First(&parentNode).Error; err != nil {
		return nil, fmt.Errorf("parent node not found: %w", err)
	}

	if err := s.db.Where("tenant_id = ? AND parent_node_id = ?", tenantID, nodeID).
		Order("name").
		Find(&nodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query child nodes: %w", err)
	}

	result := make([]models.MetaNodeLite, len(nodes))
	for i, node := range nodes {
		result[i] = convertToMetaNodeLite(node)
	}

	return result, nil
}

func (s *MetadataQueryService) GetNodeItems(tenantID, nodeID uint) ([]models.MetaItemLite, error) {
	var items []models.MetaItem

	// 先查询节点是否存在并验证租户
	var parentNode models.MetaNode
	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, nodeID).First(&parentNode).Error; err != nil {
		return nil, fmt.Errorf("parent node not found: %w", err)
	}

	if err := s.db.Where("tenant_id = ? AND node_id = ?", tenantID, nodeID).
		Order("name").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to query node items: %w", err)
	}

	result := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		result[i] = convertToMetaItemLite(item)
	}

	return result, nil
}

func (s *MetadataQueryService) GetTableSpatialMetadata(tenantID, engineID uint, schema, table string) (*models.SpatialMetadataResponse, error) {
	var item models.MetaItem

	// 查询表元数据
	err := s.db.Where("tenant_id = ? AND engine_id = ? AND item_type = ? AND name = ?", tenantID, engineID, "table", table).
		Where("attributes->>'schema' = ?", schema).
		First(&item).Error

	if err != nil {
		return nil, fmt.Errorf("table metadata not found: %w", err)
	}

	// 提取空间元数据
	spatialMeta := &models.SpatialMetadataResponse{
		Fields: []models.FieldInfo{},
	}

	// 提取 spatial_metadata
	if spatialData, ok := item.Attributes["spatial_metadata"].(map[string]interface{}); ok {
		if geomCol, ok := spatialData["geometry_column"].(string); ok {
			spatialMeta.GeometryColumn = geomCol
		}
		if srid, ok := spatialData["srid"].(float64); ok {
			spatialMeta.SRID = int(srid)
		}
		if extentSRID, ok := spatialData["extent_srid"].(float64); ok {
			spatialMeta.ExtentSRID = int(extentSRID)
		}
		if extent, ok := spatialData["extent"].([]interface{}); ok {
			spatialMeta.Extent = make([]float64, len(extent))
			for i, v := range extent {
				if f, ok := v.(float64); ok {
					spatialMeta.Extent[i] = f
				}
			}
		}
	}

	// 提取 table_metadata
	if tableMeta, ok := item.Attributes["table_metadata"].(map[string]interface{}); ok {
		if pk, ok := tableMeta["primary_key"].(string); ok {
			spatialMeta.PrimaryKey = pk
		}
	}

	// 提取字段信息
	if fields, ok := item.Attributes["fields"].([]interface{}); ok {
		for _, f := range fields {
			if fieldMap, ok := f.(map[string]interface{}); ok {
				fieldInfo := models.FieldInfo{
					Name:         toString(fieldMap["name"]),
					DataType:     toString(fieldMap["data_type"]),
					IsPrimaryKey: toBool(fieldMap["is_primary_key"]),
				}
				spatialMeta.Fields = append(spatialMeta.Fields, fieldInfo)
			}
		}
	}

	return spatialMeta, nil
}

func (s *MetadataQueryService) GetMetaNodeByID(tenantID, nodeID uint) (*models.MetaNodeLite, error) {
	var node models.MetaNode

	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, nodeID).First(&node).Error; err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	result := convertToMetaNodeLite(node)
	return &result, nil
}

// ============================================================================
// 辅助类型定义
// ============================================================================

// TableFieldInfo 表字段信息（用于 Transfer 模块）
type TableFieldInfo struct {
	Name            string `json:"name"`
	DataType        string `json:"data_type"`
	ColumnType      string `json:"column_type,omitempty"`
	IsNullable      bool   `json:"is_nullable"`
	IsPrimaryKey    bool   `json:"is_primary_key"`
	IsUniqueKey     bool   `json:"is_unique_key"`
	OrdinalPosition int    `json:"ordinal_position"`
	Comment         string `json:"comment,omitempty"`
}

// ============================================================================
// 辅助函数
// ============================================================================

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(v)
		return lower == "true" || lower == "1" || lower == "yes"
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	default:
		return false
	}
}

// convertToMetaNodeLite 转换为 MetaNodeLite
func convertToMetaNodeLite(node models.MetaNode) models.MetaNodeLite {
	var scannedAt *string
	if node.ScannedAt != nil {
		formatted := node.ScannedAt.UTC().Format("2006-01-02T15:04:05Z")
		scannedAt = &formatted
	}

	return models.MetaNodeLite{
		ID:             node.ID,
		TenantID:       node.TenantID,
		EngineID:       node.EngineID,
		ParentNodeID:   node.ParentNodeID,
		NodeType:       node.NodeType,
		Name:           node.Name,
		FullName:       node.FullName,
		Depth:          node.Depth,
		Path:           node.Path,
		ScanStatus:     node.ScanStatus,
		ScannedAt:      scannedAt,
		ItemCount:      node.ItemCount,
		TotalSizeBytes: node.TotalSizeBytes,
		Attributes:     node.Attributes,
	}
}

// convertToMetaItemLite 转换为 MetaItemLite
func convertToMetaItemLite(item models.MetaItem) models.MetaItemLite {
	var dataUpdatedAt *string
	if item.DataUpdatedAt != nil {
		formatted := item.DataUpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
		dataUpdatedAt = &formatted
	}

	return models.MetaItemLite{
		ID:            item.ID,
		TenantID:      item.TenantID,
		EngineID:      item.EngineID,
		NodeID:        item.NodeID,
		ItemType:      item.ItemType,
		Name:          item.Name,
		FullName:      item.FullName,
		RowCount:      item.RowCount,
		SizeBytes:     item.SizeBytes,
		DataUpdatedAt: dataUpdatedAt,
		Attributes:    item.Attributes,
	}
}

func toInt(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		// 尝试解析字符串为整数
		var result int64
		fmt.Sscanf(v, "%d", &result)
		return result
	default:
		return 0
	}
}
