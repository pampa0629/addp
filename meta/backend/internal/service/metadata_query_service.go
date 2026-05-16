package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"gorm.io/gorm"
)

// MetadataQueryService 元数据查询服务
// 提供Manager和Transfer模块的元数据查询接口
type MetadataQueryService struct {
	db             *gorm.DB
	repo           *metaRepo.ScanRepository
	spatialService *SpatialMetadataService
	engineService  *EngineService
	log            *slog.Logger
}

// NewMetadataQueryService 创建元数据查询服务
func NewMetadataQueryService(db *gorm.DB, spatialService *SpatialMetadataService, engineService *EngineService, log *slog.Logger) *MetadataQueryService {
	return &MetadataQueryService{
		db:             db,
		repo:           metaRepo.NewScanRepository(db),
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
		result[i] = metaquery.ToMetaItemLite(item)
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
		result[i] = metaquery.ToMetaItemLite(item)
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
		tenantID, nodeIDs, itemName, metaquery.QualifiedName(namespace, itemName)).
		First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			s.log.Info("数据项未在元数据中找到，尝试从引擎动态查询", "namespace", namespace, "itemName", itemName)
			return s.queryFieldsFromDatabase(engineID, metaquery.QualifiedName(namespace, itemName), tenantID)
		}
		return nil, fmt.Errorf("查询数据项元数据失败: %w", err)
	}

	return metaquery.FieldsFromMetaItem(item)
}

func (s *MetadataQueryService) GetItemFieldDetailsByID(tenantID, itemID uint) ([]commonModels.FieldInfo, error) {
	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item metadata not found: %w", err)
	}
	return metaquery.FieldsFromMetaItem(item)
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
	schemaName, tablePart := metaquery.ParseTableName(tableName)
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
			IsSpatial:    metaquery.IsSpatialDataType(dataType),
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
	if level, ok := namespaceLevelForPlugin(p); ok && level.Term != "" {
		return level.Term
	}
	return plugin.CatalogTermDatabase
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
		topNodesLite[i] = metaquery.ToMetaNodeLite(node)
	}

	childNodesLite := make([]models.MetaNodeLite, len(childNodes))
	for i, node := range childNodes {
		childNodesLite[i] = metaquery.ToMetaNodeLite(node)
	}

	itemsLite := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		itemsLite[i] = metaquery.ToMetaItemLite(item)
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
		result := metaquery.ToMetaNodeLite(node)
		return &result, nil
	}

	// 尝试按 name 查询顶层节点
	err = s.db.Where("tenant_id = ? AND engine_id = ? AND name = ? AND parent_node_id IS NULL", tenantID, engineID, nodePath).
		First(&node).Error
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	result := metaquery.ToMetaNodeLite(node)
	return &result, nil
}

func (s *MetadataQueryService) GetItemByPath(tenantID, engineID uint, bucketName, objectPath string) (*models.MetaItemLite, error) {
	var item models.MetaItem

	// 构建完整路径
	fullPath := bucketName
	if objectPath != "" {
		fullPath = bucketName + "/" + strings.TrimPrefix(objectPath, "/")
	}

	err := s.db.Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Where("item_type IN ?", []string{"object", "file", "table"}).
		Where("full_name = ?", fullPath).
		First(&item).Error

	if err != nil {
		return nil, fmt.Errorf("item not found: %w", err)
	}

	result := metaquery.ToMetaItemLite(item)
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
		result[i] = metaquery.ToMetaNodeLite(node)
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
		result[i] = metaquery.ToMetaItemLite(item)
	}

	return result, nil
}

func (s *MetadataQueryService) GetItemSpatialMetadataByName(tenantID, engineID uint, namespace, itemName string) (*models.SpatialMetadataResponse, error) {
	var item models.MetaItem

	err := s.db.Where("tenant_id = ? AND engine_id = ? AND name = ? AND deleted_at IS NULL", tenantID, engineID, itemName).
		Where("(attributes #>> '{storage,schema_name}' = ? OR full_name = ?)",
			namespace, metaquery.QualifiedName(namespace, itemName)).
		First(&item).Error

	if err != nil {
		return nil, fmt.Errorf("item metadata not found: %w", err)
	}

	return metaquery.SpatialMetadataFromItem(item)
}

func (s *MetadataQueryService) GetItemSpatialMetadataByID(tenantID, itemID uint) (*models.SpatialMetadataResponse, error) {
	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item metadata not found: %w", err)
	}
	return metaquery.SpatialMetadataFromItem(item)
}

func (s *MetadataQueryService) GetMetaNodeByID(tenantID, nodeID uint) (*models.MetaNodeLite, error) {
	var node models.MetaNode

	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, nodeID).First(&node).Error; err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	result := metaquery.ToMetaNodeLite(node)
	return &result, nil
}

func (s *MetadataQueryService) GetItemByID(tenantID, itemID uint) (*models.MetaItemLite, error) {
	var item models.MetaItem

	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item not found: %w", err)
	}

	result := metaquery.ToMetaItemLite(item)
	return &result, nil
}
