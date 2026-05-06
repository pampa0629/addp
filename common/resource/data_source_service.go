package resource

import (
	"fmt"
	"log"
	"strings"

	commonAttrs "github.com/addp/common/attributes"
	"github.com/addp/common/models"
)

// SystemClient System 模块客户端接口
// 提供引擎相关的查询功能
type SystemClient interface {
	// GetEngine 获取单个引擎信息
	GetEngine(engineID uint) (*models.Engine, error)

	// ListEngines 获取引擎列表
	// engineType: 过滤引擎类型（可选，空字符串表示不过滤）
	// tenantID: 过滤租户ID（可选，0 表示不过滤）
	ListEngines(engineType string, tenantID uint) ([]models.Engine, error)
}

// EngineFilters 引擎过滤条件
type EngineFilters struct {
	// EngineTypes 引擎类型列表（如 []string{"postgresql", "mysql"}）
	EngineTypes []string

	// DataSourceTypes 数据源类型列表（如 []string{"database", "object_storage"}）
	DataSourceTypes []string
}

// TableMetadata 表元数据
type TableMetadata struct {
	// HasGeometry 是否包含几何列
	HasGeometry bool `json:"has_geometry"`

	// GeometryColumn 几何列名称
	GeometryColumn string `json:"geometry_column,omitempty"`

	// SRID 空间参考系统ID
	SRID *int `json:"srid,omitempty"`

	// GeometryType 几何类型（Point, LineString, Polygon 等）
	GeometryType string `json:"geometry_type,omitempty"`

	// Extent 数据范围 [minX, minY, maxX, maxY]
	Extent []float64 `json:"extent,omitempty"`

	// Columns 列信息列表（可选）
	Columns []ColumnMetadata `json:"columns,omitempty"`
}

// ColumnMetadata 列元数据
type ColumnMetadata struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// DataSourceService 数据源服务
// 封装数据源选择相关的核心逻辑，供各模块复用
type DataSourceService struct {
	systemClient SystemClient
	metaClient   MetaClient
	treeBuilder  *TreeBuilder
}

// NewDataSourceService 创建数据源服务
func NewDataSourceService(systemClient SystemClient, metaClient MetaClient) *DataSourceService {
	return &DataSourceService{
		systemClient: systemClient,
		metaClient:   metaClient,
		treeBuilder:  NewTreeBuilder(metaClient),
	}
}

// GetEngines 获取引擎列表
// 支持按引擎类型和数据源类型过滤
func (s *DataSourceService) GetEngines(tenantID uint, filters EngineFilters) ([]*models.Engine, error) {
	log.Printf("[DataSourceService] GetEngines: tenantID=%d, filters=%+v", tenantID, filters)

	// 调用 System Client 获取引擎列表
	// 如果指定了单一引擎类型，传递给 API（减少网络开销）
	engineTypeFilter := ""
	if len(filters.EngineTypes) == 1 {
		engineTypeFilter = filters.EngineTypes[0]
	}

	engines, err := s.systemClient.ListEngines(engineTypeFilter, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list engines: %w", err)
	}

	// 转换为指针数组
	enginePtrs := make([]*models.Engine, 0, len(engines))
	for i := range engines {
		enginePtrs = append(enginePtrs, &engines[i])
	}

	// 客户端过滤（处理多个引擎类型或数据源类型）
	enginePtrs = s.filterEngines(enginePtrs, filters)

	log.Printf("[DataSourceService] GetEngines: returned %d engines", len(enginePtrs))
	return enginePtrs, nil
}

// GetEngineTree 获取引擎树
// expandDepth: 展开深度（-1 表示全部展开，0 表示只返回引擎根节点，1 表示展开一层）
func (s *DataSourceService) GetEngineTree(engineID uint, expandDepth int) (*TreeNode, error) {
	log.Printf("[DataSourceService] GetEngineTree: engineID=%d, expandDepth=%d", engineID, expandDepth)

	// 1. 获取引擎信息
	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 2. 如果只需要引擎根节点，直接返回
	if expandDepth == 0 {
		rootLocator := buildEngineRootLocator(engine.ID)
		return &TreeNode{
			ID:      rootLocator,
			Locator: rootLocator,
			Label:   engine.Name,
			Type:    "engine",
			Icon:    getEngineIcon(engine.EngineType),
			Metadata: map[string]interface{}{
				"engine_id":   engine.ID,
				"engine_type": engine.EngineType,
			},
			Children:    []*TreeNode{},
			HasChildren: true,
		}, nil
	}

	// 3. 获取元数据树
	metadataTree, err := s.metaClient.GetMetadataTree(engineID)
	if err != nil {
		// Meta 不可用时，返回降级的引擎根节点
		log.Printf("[DataSourceService] GetEngineTree: meta unavailable, returning degraded tree: %v", err)
		return s.buildDegradedTree(engine), nil
	}

	// 4. 使用 TreeBuilder 构建标准树结构
	tree, err := s.treeBuilder.BuildFromMetadataTree(engine, metadataTree)
	if err != nil {
		return nil, fmt.Errorf("failed to build tree: %w", err)
	}

	log.Printf("[DataSourceService] GetEngineTree: tree built successfully")
	return tree, nil
}

// GetNodeChildren 获取节点的子节点
// 用于懒加载场景
func (s *DataSourceService) GetNodeChildren(nodeID uint) ([]*TreeNode, error) {
	log.Printf("[DataSourceService] GetNodeChildren: nodeID=%d", nodeID)

	// 1. 获取子节点（MetaNode）
	metaNodes, err := s.metaClient.GetNodeChildren(nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node children: %w", err)
	}

	// 2. 获取子项目（MetaItem，如表、对象等）
	metaItems, err := s.metaClient.GetNodeItems(nodeID)
	if err != nil {
		// 如果获取 items 失败，仅记录日志，不中断流程
		log.Printf("[DataSourceService] GetNodeChildren: failed to get items (non-fatal): %v", err)
		metaItems = []models.MetaItem{}
	}

	// 3. 获取引擎信息（从第一个节点或 item 中提取 engine_id）
	var engineID uint
	if len(metaNodes) > 0 {
		engineID = metaNodes[0].EngineID
	} else if len(metaItems) > 0 {
		engineID = metaItems[0].EngineID
	} else {
		// 没有子节点，返回空数组
		log.Printf("[DataSourceService] GetNodeChildren: no children found")
		return []*TreeNode{}, nil
	}

	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 4. 转换为 TreeNode
	children := make([]*TreeNode, 0, len(metaNodes)+len(metaItems))

	// 4.1 转换 MetaNode
	metaNodePtrs := make([]*models.MetaNode, len(metaNodes))
	for i := range metaNodes {
		metaNodePtrs[i] = &metaNodes[i]
	}
	children = append(children, s.treeBuilder.ConvertMetaNodes(engine, metaNodePtrs)...)

	// 4.2 转换 MetaItem（将 Item 转换为 Node，再转换为 TreeNode）
	virtualNodes := s.treeBuilder.ConvertMetaItems(metaItems)
	virtualNodePtrs := make([]*models.MetaNode, len(virtualNodes))
	for i := range virtualNodes {
		virtualNodePtrs[i] = virtualNodes[i]
	}
	children = append(children, s.treeBuilder.ConvertMetaNodes(engine, virtualNodePtrs)...)

	log.Printf("[DataSourceService] GetNodeChildren: returned %d children (%d nodes + %d items)",
		len(children), len(metaNodes), len(metaItems))
	return children, nil
}

// DetectTableMetadata 检测表元数据
// 主要用于检测几何列信息
func (s *DataSourceService) DetectTableMetadata(engineID uint, schema, table string) (*TableMetadata, error) {
	log.Printf("[DataSourceService] DetectTableMetadata: engineID=%d, schema=%s, table=%s",
		engineID, schema, table)

	// 获取引擎信息
	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 只有数据库类型的引擎才可能有表元数据
	if !isDatabaseEngine(engine.EngineType) {
		return &TableMetadata{
			HasGeometry: false,
		}, nil
	}

	// 构建表的完整路径
	tablePath := fmt.Sprintf("%s.%s", schema, table)

	// 通过 Meta 获取表节点
	tableNode, err := s.metaClient.GetNodeByPath(engineID, tablePath)
	if err != nil {
		// 如果 Meta 中没有该表，返回基础元数据
		log.Printf("[DataSourceService] DetectTableMetadata: table not found in meta (non-fatal): %v", err)
		return &TableMetadata{
			HasGeometry: false,
		}, nil
	}

	// 从 attributes 中提取几何列信息
	metadata := &TableMetadata{
		HasGeometry: false,
	}

	if tableNode.Attributes != nil {
		spatial := commonAttrs.Section(tableNode.Attributes, "capabilities.spatial")
		if spatial != nil {
			metadata.GeometryColumn = commonAttrs.InterfaceString(spatial["primary_geometry_column"])
			if columns, ok := spatial["geometry_columns"].([]interface{}); ok && len(columns) > 0 {
				if column, ok := columns[0].(map[string]interface{}); ok {
					if metadata.GeometryColumn == "" {
						metadata.GeometryColumn = commonAttrs.InterfaceString(column["name"])
					}
					metadata.GeometryType = commonAttrs.InterfaceString(column["geometry_type"])
					if srid := commonAttrs.InterfaceInt64(column["srid"]); srid > 0 {
						sridInt := int(srid)
						metadata.SRID = &sridInt
					}
				}
			}
			if columns, ok := spatial["geometry_columns"].([]map[string]interface{}); ok && len(columns) > 0 {
				if metadata.GeometryColumn == "" {
					metadata.GeometryColumn = commonAttrs.InterfaceString(columns[0]["name"])
				}
				metadata.GeometryType = commonAttrs.InterfaceString(columns[0]["geometry_type"])
				if srid := commonAttrs.InterfaceInt64(columns[0]["srid"]); srid > 0 {
					sridInt := int(srid)
					metadata.SRID = &sridInt
				}
			}
			metadata.HasGeometry = metadata.GeometryColumn != ""
			metadata.Extent = commonAttrs.InterfaceFloat64Slice(spatial["extent"])
		}
	}

	log.Printf("[DataSourceService] DetectTableMetadata: hasGeometry=%v, column=%s",
		metadata.HasGeometry, metadata.GeometryColumn)
	return metadata, nil
}

// 辅助方法

// filterEngines 过滤引擎列表
func (s *DataSourceService) filterEngines(engines []*models.Engine, filters EngineFilters) []*models.Engine {
	// 如果没有过滤条件，直接返回
	if len(filters.EngineTypes) == 0 && len(filters.DataSourceTypes) == 0 {
		return engines
	}

	filtered := make([]*models.Engine, 0, len(engines))

	// 构建引擎类型集合（用于快速查找）
	engineTypeSet := make(map[string]bool)
	for _, t := range filters.EngineTypes {
		engineTypeSet[strings.ToLower(t)] = true
	}

	// 按引擎类型过滤
	if len(filters.EngineTypes) > 0 {
		for _, engine := range engines {
			if engineTypeSet[strings.ToLower(engine.EngineType)] {
				filtered = append(filtered, engine)
			}
		}
		engines = filtered
		filtered = make([]*models.Engine, 0, len(engines))
	}

	// 按数据源类型过滤
	if len(filters.DataSourceTypes) > 0 {
		dataSourceTypeSet := make(map[string]bool)
		for _, t := range filters.DataSourceTypes {
			dataSourceTypeSet[strings.ToLower(t)] = true
		}

		for _, engine := range engines {
			engineCategory := getEngineCategory(engine.EngineType)
			if dataSourceTypeSet[engineCategory] {
				filtered = append(filtered, engine)
			}
		}
		return filtered
	}

	return engines
}

// buildDegradedTree 构建降级树（Meta 不可用时）
func (s *DataSourceService) buildDegradedTree(engine *models.Engine) *TreeNode {
	rootLocator := buildEngineRootLocator(engine.ID)
	return &TreeNode{
		ID:      rootLocator,
		Locator: rootLocator,
		Label:   engine.Name + " (元数据不可用)",
		Type:    "engine",
		Icon:    getEngineIcon(engine.EngineType),
		Metadata: map[string]interface{}{
			"engine_id":   engine.ID,
			"engine_type": engine.EngineType,
			"degraded":    true,
		},
		Children:    []*TreeNode{},
		HasChildren: false,
	}
}

// isDatabaseEngine 判断是否为数据库引擎
func isDatabaseEngine(engineType string) bool {
	databaseTypes := map[string]bool{
		"postgresql": true,
		"mysql":      true,
		"doris":      true,
		"clickhouse": true,
		"mongodb":    true,
		"spark_sql":  true,
	}
	return databaseTypes[strings.ToLower(engineType)]
}

// getEngineCategory 获取引擎类别
func getEngineCategory(engineType string) string {
	switch strings.ToLower(engineType) {
	case "postgresql", "mysql", "doris", "clickhouse", "mongodb", "spark_sql":
		return "database"
	case "minio", "s3":
		return "object_storage"
	case "python_workflow", "spark_workflow":
		return "compute"
	default:
		return "unknown"
	}
}
