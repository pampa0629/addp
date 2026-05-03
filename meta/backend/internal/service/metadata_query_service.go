package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/mappers/mysql"
	_ "github.com/addp/common/format/mappers/postgresql"
	_ "github.com/addp/common/format/mappers/spatialite"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// MetadataQueryService 元数据查询服务
// 提供Manager和Transfer模块的元数据查询接口
type MetadataQueryService struct {
	db             *gorm.DB
	repo           *ScanRepository
	spatialService *SpatialMetadataService
	engineService  *EngineService
	log            *slog.Logger
}

// NewMetadataQueryService 创建元数据查询服务
func NewMetadataQueryService(db *gorm.DB, spatialService *SpatialMetadataService, engineService *EngineService, log *slog.Logger) *MetadataQueryService {
	return &MetadataQueryService{
		db:             db,
		repo:           NewScanRepository(db),
		spatialService: spatialService,
		engineService:  engineService,
		log:            log,
	}
}

// ============================================================================
// 通用元数据查询接口
// ============================================================================

func (s *MetadataQueryService) ListItemsByEngine(engineID, tenantID uint) ([]models.MetaItemLite, error) {
	var items []models.MetaItem

	var nodeIDs []uint
	err := s.db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Pluck("id", &nodeIDs).Error
	if err != nil {
		return nil, err
	}

	if len(nodeIDs) == 0 {
		return []models.MetaItemLite{}, nil
	}

	err = s.db.Select("*").
		Where("tenant_id = ? AND node_id IN (?) AND deleted_at IS NULL", tenantID, nodeIDs).
		Order("name").
		Find(&items).Error
	if err != nil {
		return nil, err
	}

	result := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		result[i] = convertToMetaItemLite(item)
	}
	return result, nil
}

func (s *MetadataQueryService) ListItemsByNamespace(engineID, tenantID uint, namespace string) ([]models.MetaItemLite, error) {
	var node models.MetaNode
	err := s.db.Where("tenant_id = ? AND engine_id = ? AND name = ? AND parent_node_id IS NULL",
		tenantID, engineID, namespace).
		First(&node).Error
	if err != nil {
		return nil, err
	}

	var items []models.MetaItem
	err = s.db.Select("*").
		Where("tenant_id = ? AND node_id = ? AND deleted_at IS NULL", tenantID, node.ID).
		Order("name").
		Find(&items).Error
	if err != nil {
		return nil, err
	}

	result := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		result[i] = convertToMetaItemLite(item)
	}
	return result, nil
}

func (s *MetadataQueryService) GetItemFieldNames(engineID uint, namespace, itemName string, tenantID uint) ([]string, error) {
	fieldInfos, err := s.GetItemFieldDetailsByName(engineID, namespace, itemName, tenantID)
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

func (s *MetadataQueryService) GetItemFieldDetailsByName(engineID uint, namespace, itemName string, tenantID uint) ([]commonModels.FieldInfo, error) {
	s.log.Info("GetItemFieldDetailsByName 开始查询", "engineID", engineID, "namespace", namespace, "itemName", itemName, "tenantID", tenantID)

	var nodeIDs []uint
	if namespace != "" {
		var node models.MetaNode
		err := s.db.Where("tenant_id = ? AND engine_id = ? AND (name = ? OR full_name = ?) AND parent_node_id IS NULL",
			tenantID, engineID, namespace, namespace).
			First(&node).Error
		if err != nil {
			return nil, fmt.Errorf("namespace metadata not found: %w", err)
		}
		nodeIDs = []uint{node.ID}
	} else {
		err := s.db.Model(&models.MetaNode{}).
			Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
			Pluck("id", &nodeIDs).Error
		if err != nil {
			s.log.Error("查询节点ID失败", "error", err)
			return nil, fmt.Errorf("查询节点ID失败: %w", err)
		}
	}

	if len(nodeIDs) == 0 {
		s.log.Warn("没有找到任何节点")
		return []commonModels.FieldInfo{}, nil
	}

	var item models.MetaItem
	err := s.db.Where("tenant_id = ? AND node_id IN (?) AND (name = ? OR full_name = ?) AND deleted_at IS NULL",
		tenantID, nodeIDs, itemName, qualifiedName(namespace, itemName)).
		First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			s.log.Info("数据项未在元数据中找到，尝试从引擎动态查询", "namespace", namespace, "itemName", itemName)
			return s.queryFieldsFromDatabase(engineID, qualifiedName(namespace, itemName), tenantID)
		}
		return nil, fmt.Errorf("查询数据项元数据失败: %w", err)
	}

	return fieldsFromMetaItem(item)
}

func (s *MetadataQueryService) GetItemFieldDetailsByID(tenantID, itemID uint) ([]commonModels.FieldInfo, error) {
	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item metadata not found: %w", err)
	}
	return fieldsFromMetaItem(item)
}

func fieldsFromMetaItem(item models.MetaItem) ([]commonModels.FieldInfo, error) {
	fieldsData, ok := item.Attributes["fields"]
	if !ok {
		return []commonModels.FieldInfo{}, nil
	}

	fieldsList, ok := fieldsData.([]interface{})
	if !ok {
		return []commonModels.FieldInfo{}, fmt.Errorf("invalid fields format in metadata")
	}

	fieldInfos := make([]commonModels.FieldInfo, 0, len(fieldsList))
	for _, fieldData := range fieldsList {
		fieldMap, ok := fieldData.(map[string]interface{})
		if !ok {
			continue
		}

		dataType := toString(fieldMap["data_type"])

		info := commonModels.FieldInfo{
			Name:         toString(fieldMap["name"]),
			DataType:     dataType,
			IsPrimaryKey: toBool(fieldMap["is_primary_key"]),
			IsNullable:   toBool(fieldMap["is_nullable"]),
			Comment:      toString(fieldMap["comment"]),
			// ← 关键: 从元数据中提取空间字段信息
			IsSpatial:    toBool(fieldMap["is_spatial"]),
			GeometryType: toString(fieldMap["geometry_type"]),
			SRID:         int(toInt(fieldMap["srid"])),
		}

		fieldInfos = append(fieldInfos, info)
	}

	return fieldInfos, nil
}

// queryFieldsFromDatabase 从数据库动态查询表的字段信息
func (s *MetadataQueryService) queryFieldsFromDatabase(engineID uint, tableName string, tenantID uint) ([]commonModels.FieldInfo, error) {
	s.log.Info("开始从数据库动态查询字段", "engineID", engineID, "tableName", tableName)

	// 1. 获取引擎连接信息
	resource, err := s.engineService.GetEngine(engineID, tenantID)
	if err != nil {
		s.log.Error("获取引擎连接信息失败", "error", err)
		return nil, fmt.Errorf("获取引擎连接信息失败: %w", err)
	}

	// 2. 获取插件
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		s.log.Error("获取插件失败", "engineType", resource.EngineType, "error", err)
		return nil, fmt.Errorf("获取插件失败: %w", err)
	}

	metadataProvider, ok := p.(plugin.ItemMetadataProvider)
	if !ok {
		return nil, fmt.Errorf("引擎 %s 不支持 item 元数据描述", resource.EngineType)
	}

	// 3. 解析表名（支持 schema.table 格式）
	schemaName, tablePart := parseTableName(tableName)
	if schemaName == "" {
		schemaName = "public" // 默认 schema
	}

	s.log.Info("解析表名", "原始表名", tableName, "schema", schemaName, "table", tablePart)

	fieldInfos, err := s.queryFieldsFromMetadataProvider(context.Background(), resource, metadataProvider, p, schemaName, tablePart)
	if err != nil {
		s.log.Error("查询字段失败", "schema", schemaName, "table", tablePart, "error", err)
		return nil, fmt.Errorf("查询表字段失败: %w", err)
	}
	s.log.Info("从 ItemMetadataProvider 查询到字段", "count", len(fieldInfos))
	return fieldInfos, nil
}

func (s *MetadataQueryService) queryFieldsFromMetadataProvider(
	ctx context.Context,
	resource *commonModels.Engine,
	metadataProvider plugin.ItemMetadataProvider,
	enginePlugin plugin.EnginePlugin,
	schemaName string,
	tableName string,
) ([]commonModels.FieldInfo, error) {
	item, err := metadataProvider.DescribeItem(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
		Segments: []plugin.CatalogSegment{
			{Term: namespaceTermForPlugin(enginePlugin), Kind: plugin.CatalogKindNamespace, Name: schemaName},
			{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: tableName},
		},
	}, plugin.MetadataOptions{})
	if err != nil {
		return nil, err
	}

	fieldInfos := make([]commonModels.FieldInfo, 0, len(item.Fields))
	for _, field := range item.Fields {
		dataType := field.NativeType
		if dataType == "" {
			dataType = field.Type
		}
		info := commonModels.FieldInfo{
			Name:         field.Name,
			DataType:     dataType,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
			IsSpatial:    isSpatialDataType(dataType),
		}
		if geometryType, ok := field.Attributes["geometry_type"].(string); ok {
			info.GeometryType = geometryType
		}
		if srid, ok := int64Stat(field.Attributes, "srid"); ok {
			info.SRID = int(srid)
		}
		fieldInfos = append(fieldInfos, info)
	}
	return fieldInfos, nil
}

func namespaceTermForPlugin(p plugin.EnginePlugin) string {
	if modelProvider, ok := p.(plugin.CatalogModelProvider); ok {
		model := modelProvider.CatalogModel()
		if len(model.Levels) > 0 && model.Levels[0].Term != "" {
			return model.Levels[0].Term
		}
	}
	return plugin.CatalogTermDatabase
}

// parseTableName 解析表名，支持 schema.table 格式
func parseTableName(tableName string) (schema, table string) {
	parts := strings.SplitN(tableName, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}

func qualifiedName(namespace, itemName string) string {
	if namespace == "" {
		return itemName
	}
	if strings.Contains(itemName, ".") || strings.Contains(itemName, "/") {
		return itemName
	}
	return namespace + "." + itemName
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

	// 查询所有项（只返回 node_id 存在于 meta_node 中的 items，过滤孤立记录）
	var items []models.MetaItem
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_id IN (SELECT id FROM metadata.meta_node WHERE tenant_id = ? AND engine_id = ?)",
		tenantID, engineID, tenantID, engineID).
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
	err := s.db.Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Where("item_type IN ?", []string{"object", "lake_table"}).
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

func (s *MetadataQueryService) GetItemSpatialMetadataByName(tenantID, engineID uint, namespace, itemName string) (*models.SpatialMetadataResponse, error) {
	var item models.MetaItem

	err := s.db.Where("tenant_id = ? AND engine_id = ? AND name = ? AND deleted_at IS NULL", tenantID, engineID, itemName).
		Where("(attributes->>'schema' = ? OR attributes->>'namespace' = ? OR full_name = ?)",
			namespace, namespace, qualifiedName(namespace, itemName)).
		First(&item).Error

	if err != nil {
		return nil, fmt.Errorf("item metadata not found: %w", err)
	}

	return spatialMetadataFromItem(item)
}

func (s *MetadataQueryService) GetItemSpatialMetadataByID(tenantID, itemID uint) (*models.SpatialMetadataResponse, error) {
	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item metadata not found: %w", err)
	}
	return spatialMetadataFromItem(item)
}

func spatialMetadataFromItem(item models.MetaItem) (*models.SpatialMetadataResponse, error) {
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
		// 提取 geometry_types
		if geomTypes, ok := spatialData["geometry_types"].([]interface{}); ok {
			spatialMeta.GeometryTypes = make([]string, 0, len(geomTypes))
			for _, v := range geomTypes {
				if s, ok := v.(string); ok {
					spatialMeta.GeometryTypes = append(spatialMeta.GeometryTypes, s)
				}
			}
		}
	}

	// 提取 table_metadata
	if tableMeta, ok := item.Attributes["table_metadata"].(map[string]interface{}); ok {
		if pk, ok := tableMeta["primary_key"].(string); ok {
			spatialMeta.PrimaryKey = pk
		} else if pkArray, ok := tableMeta["primary_key"].([]interface{}); ok {
			if len(pkArray) > 0 {
				if pkStr, ok := pkArray[0].(string); ok {
					spatialMeta.PrimaryKey = pkStr
				}
			}
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

	// 提取表记录数（从 meta_item.row_count）
	if item.RowCount != nil {
		spatialMeta.RowCount = *item.RowCount
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

func (s *MetadataQueryService) GetItemByID(tenantID, itemID uint) (*models.MetaItemLite, error) {
	var item models.MetaItem

	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item not found: %w", err)
	}

	result := convertToMetaItemLite(item)
	return &result, nil
}

// ============================================================================
// 辅助类型定义
// ============================================================================

// SpatialInfo 空间字段详细信息（用于动态查询）
type SpatialInfo struct {
	GeometryType string
	SRID         int
}

// ============================================================================
// 辅助函数
// ============================================================================

// isSpatialDataType 判断数据类型是否为空间类型
func isSpatialDataType(dataType string) bool {
	spatialTypes := []string{
		"geometry", "geography",
		"point", "linestring", "polygon",
		"multipoint", "multilinestring", "multipolygon",
		"geometrycollection",
	}
	lowerType := strings.ToLower(dataType)
	for _, t := range spatialTypes {
		if lowerType == t || strings.HasPrefix(lowerType, t+"(") {
			return true
		}
	}
	return false
}

// detectSpatialInfo 从数据库动态检测空间字段信息（仅 PostgreSQL）
func (s *MetadataQueryService) detectSpatialInfo(db *gorm.DB, schema, table, column string) *SpatialInfo {
	var result struct {
		GeometryType string
		SRID         int
	}

	// 使用 geometry_columns 系统视图获取空间信息
	query := `
		SELECT
			COALESCE(type, '') as geometry_type,
			COALESCE(srid, 0) as srid
		FROM geometry_columns
		WHERE f_table_schema = $1
		  AND f_table_name = $2
		  AND f_geometry_column = $3
		LIMIT 1
	`

	err := db.Raw(query, schema, table, column).Scan(&result).Error
	if err != nil {
		s.log.Warn("检测空间字段信息失败", "schema", schema, "table", table, "column", column, "error", err)
		return nil
	}

	if result.GeometryType == "" {
		return nil
	}

	return &SpatialInfo{
		GeometryType: result.GeometryType,
		SRID:         result.SRID,
	}
}

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

// standardizeFieldType 标准化字段类型
// 根据原始数据类型和列类型，使用 common/format 类型映射器进行标准化
func standardizeFieldType(dataType, columnType string) string {
	if dataType == "" && columnType == "" {
		return string(format.FieldTypeUnknown)
	}

	// 优先使用 data_type，因为它通常更简洁
	typeToMap := dataType
	if typeToMap == "" {
		typeToMap = columnType
	}

	typeToMap = strings.ToLower(strings.TrimSpace(typeToMap))

	// 智能检测数据库类型并使用对应的 TypeMapper
	var standardType format.FieldType

	// 1. 优先尝试几何类型（PostGIS/SpatiaLite）
	if isGeometryType(typeToMap) {
		// 根据具体类型选择映射器
		if containsAny(typeToMap, "multipolygon", "multilinestring", "multipoint", "geometrycollection") {
			// 使用 PostgreSQL mapper（支持更多几何类型）
			if mapper := format.GetTypeMapper("postgresql"); mapper != nil {
				standardType = mapper.ToCommon(typeToMap)
			}
		} else {
			// 常规几何类型，PostgreSQL 和 SpatiaLite 都支持
			if mapper := format.GetTypeMapper("postgresql"); mapper != nil {
				standardType = mapper.ToCommon(typeToMap)
			}
		}
	} else {
		// 2. 非几何类型，尝试各种映射器
		// 优先尝试 PostgreSQL（最常用）
		if mapper := format.GetTypeMapper("postgresql"); mapper != nil {
			standardType = mapper.ToCommon(typeToMap)
			if standardType != format.FieldTypeUnknown {
				return string(standardType)
			}
		}

		// 尝试 MySQL
		if mapper := format.GetTypeMapper("mysql"); mapper != nil {
			standardType = mapper.ToCommon(typeToMap)
			if standardType != format.FieldTypeUnknown {
				return string(standardType)
			}
		}

		// 尝试 SpatiaLite/SQLite
		if mapper := format.GetTypeMapper("spatialite"); mapper != nil {
			standardType = mapper.ToCommon(typeToMap)
			if standardType != format.FieldTypeUnknown {
				return string(standardType)
			}
		}
	}

	// 如果所有映射器都返回 unknown，返回默认值
	if standardType == "" || standardType == format.FieldTypeUnknown {
		return string(format.FieldTypeString)
	}

	return string(standardType)
}

// isGeometryType 检查是否为几何类型
func isGeometryType(typeName string) bool {
	typeLower := strings.ToLower(typeName)
	geometryKeywords := []string{
		"geometry", "point", "linestring", "polygon",
		"multipoint", "multilinestring", "multipolygon",
		"geometrycollection", "geom", "geog",
	}

	for _, keyword := range geometryKeywords {
		if strings.Contains(typeLower, keyword) {
			return true
		}
	}
	return false
}

// containsAny 检查字符串是否包含任意一个子串
func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
