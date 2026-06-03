package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/gin-gonic/gin"
)

// DataSourceHandler 数据源处理器
// 为 Transfer 前端提供统一的数据源访问接口
type DataSourceHandler struct {
	systemClient *commonClient.SystemClient
	metaClient   *commonClient.MetaClient
	treeBuilder  *resourcetree.TreeBuilder
}

// NewDataSourceHandler 创建数据源处理器
func NewDataSourceHandler(systemClient *commonClient.SystemClient, metaClient *commonClient.MetaClient) *DataSourceHandler {
	return &DataSourceHandler{
		systemClient: systemClient,
		metaClient:   metaClient,
		treeBuilder:  resourcetree.NewTreeBuilder(),
	}
}

type engineFilters struct {
	EngineTypes     []string
	DataSourceTypes []string
}

type tableMetadata struct {
	HasGeometry    bool             `json:"has_geometry"`
	GeometryColumn string           `json:"geometry_column,omitempty"`
	SRID           *int             `json:"srid,omitempty"`
	GeometryType   string           `json:"geometry_type,omitempty"`
	Extent         []float64        `json:"extent,omitempty"`
	Columns        []columnMetadata `json:"columns,omitempty"`
}

type columnMetadata struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// GetEngines 获取存储引擎列表
// @Summary 获取存储引擎列表 | List engines
// @Tags 数据源 | Data Sources
// @Produce json
// @Param engine_types query string false "引擎类型列表 | Engine types"
// @Param data_source_types query string false "数据源类型列表 | Data source types"
// @Success 200 {array} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /engines [get]
// @Security BearerAuth
func (h *DataSourceHandler) GetEngines(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id in token"})
		return
	}

	// 解析过滤条件
	var filters engineFilters

	// 引擎类型过滤
	if engineTypesStr := c.Query("engine_types"); engineTypesStr != "" {
		// 逗号分隔的引擎类型列表
		filters.EngineTypes = parseCommaSeparated(engineTypesStr)
	}

	// 数据源类型过滤
	if dataSourceTypesStr := c.Query("data_source_types"); dataSourceTypesStr != "" {
		filters.DataSourceTypes = parseCommaSeparated(dataSourceTypesStr)
	}

	engineTypeFilter := ""
	if len(filters.EngineTypes) == 1 {
		engineTypeFilter = filters.EngineTypes[0]
	}

	engines, err := h.systemClient.ListEngines(engineTypeFilter, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engines: " + err.Error()})
		return
	}

	enginePtrs := make([]*models.Engine, 0, len(engines))
	for i := range engines {
		enginePtrs = append(enginePtrs, &engines[i])
	}
	enginePtrs = filterEngines(enginePtrs, filters)

	// 直接返回数组（符合 ADDP API 规范）
	c.JSON(http.StatusOK, enginePtrs)
}

// GetEngineTree 获取引擎的元数据树
// @Summary 获取引擎元数据树 | Get engine metadata tree
// @Tags 数据源 | Data Sources
// @Produce json
// @Param engine_id path int true "引擎ID | Engine ID"
// @Param expand_depth query int false "展开深度 | Expand depth" default(1)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /engines/{engine_id}/tree [get]
// @Security BearerAuth
func (h *DataSourceHandler) GetEngineTree(c *gin.Context) {
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid engine_id"})
		return
	}

	tenantID := c.GetUint("tenant_id")
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id in token"})
		return
	}

	// 解析展开深度
	expandDepth := 1 // 默认展开一层
	if expandDepthStr := c.Query("expand_depth"); expandDepthStr != "" {
		if depth, err := strconv.Atoi(expandDepthStr); err == nil {
			expandDepth = depth
		}
	}

	engine, err := h.systemClient.GetEngine(uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engine: " + err.Error()})
		return
	}

	if expandDepth == 0 {
		tree, err := h.treeBuilder.BuildFromMeta(engine, nil, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build engine root: " + err.Error()})
			return
		}
		tree.HasChildren = true
		c.JSON(http.StatusOK, tree)
		return
	}

	metadataTree, err := h.metaClient.GetMetadataTree(uint(engineID))
	if err != nil {
		tree, buildErr := h.treeBuilder.BuildFromMeta(engine, nil, expandDepth)
		if buildErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build degraded tree: " + buildErr.Error()})
			return
		}
		tree.Label = engine.Name + " (元数据不可用)"
		tree.Metadata["degraded"] = true
		tree.HasChildren = false
		c.JSON(http.StatusOK, tree)
		return
	}

	tree, err := h.treeBuilder.BuildFromMetadataTree(engine, metadataTree)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build engine tree: " + err.Error()})
		return
	}

	// 直接返回对象
	c.JSON(http.StatusOK, tree)
}

// GetNodeChildren 获取节点的子节点（懒加载）
// @Summary 获取节点子节点 | Get node children
// @Tags 数据源 | Data Sources
// @Produce json
// @Param node_id path int true "节点ID | Node ID"
// @Success 200 {array} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /nodes/{node_id}/children [get]
// @Security BearerAuth
func (h *DataSourceHandler) GetNodeChildren(c *gin.Context) {
	nodeIDStr := c.Param("node_id")
	nodeID, err := strconv.ParseUint(nodeIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid node_id"})
		return
	}

	children, err := h.metaClient.GetNodeChildren(uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get node children: " + err.Error()})
		return
	}

	items, err := h.metaClient.GetNodeItems(uint(nodeID))
	if err != nil {
		items = []models.MetaItem{}
	}

	if len(children) == 0 && len(items) == 0 {
		c.JSON(http.StatusOK, []*resourcetree.TreeNode{})
		return
	}

	var engineID uint
	if len(children) > 0 {
		engineID = children[0].EngineID
	} else {
		engineID = items[0].EngineID
	}

	engine, err := h.systemClient.GetEngine(engineID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engine: " + err.Error()})
		return
	}

	metaNodePtrs := make([]*models.MetaNode, 0, len(children))
	for i := range children {
		metaNodePtrs = append(metaNodePtrs, &children[i])
	}

	itemNodes := h.treeBuilder.ConvertMetaItemsForEngine(engine.EngineType, items)
	allNodes := make([]*models.MetaNode, 0, len(metaNodePtrs)+len(itemNodes))
	allNodes = append(allNodes, metaNodePtrs...)
	allNodes = append(allNodes, itemNodes...)

	treeNodes := h.treeBuilder.ConvertMetaNodes(engine, allNodes)

	// 直接返回数组
	c.JSON(http.StatusOK, treeNodes)
}

// DetectTableMetadata 检测表元数据（几何列检测）
// @Summary 检测表元数据 | Detect table metadata
// @Tags 数据源 | Data Sources
// @Produce json
// @Param engine_id query int true "引擎ID | Engine ID"
// @Param schema query string true "Schema"
// @Param table query string true "表名 | Table"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tables/metadata [get]
// @Security BearerAuth
func (h *DataSourceHandler) DetectTableMetadata(c *gin.Context) {
	engineIDStr := c.Query("engine_id")
	if engineIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing engine_id parameter"})
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid engine_id"})
		return
	}

	schema := c.Query("schema")
	if schema == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing schema parameter"})
		return
	}

	table := c.Query("table")
	if table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing table parameter"})
		return
	}

	engine, err := h.systemClient.GetEngine(uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engine: " + err.Error()})
		return
	}

	metadata := &tableMetadata{HasGeometry: false}
	if isDatabaseEngine(engine.EngineType) {
		catalogPath := fmt.Sprintf("%s.%s", schema, table)
		if fields, err := h.metaClient.GetItemFieldsByCatalogPath(uint(engineID), catalogPath, true); err == nil {
			metadata.Columns = columnsFromFields(fields)
		}
		if spatialMeta, err := h.metaClient.GetItemSpatialMetadataByCatalogPath(uint(engineID), catalogPath); err == nil && spatialMeta != nil {
			metadata.GeometryColumn = spatialMeta.GeometryColumn
			metadata.HasGeometry = spatialMeta.GeometryColumn != ""
			if spatialMeta.SRID > 0 {
				srid := spatialMeta.SRID
				metadata.SRID = &srid
			}
			if len(spatialMeta.GeometryTypes) > 0 {
				metadata.GeometryType = spatialMeta.GeometryTypes[0]
			}
			if len(spatialMeta.Extent) > 0 {
				metadata.Extent = spatialMeta.Extent
			}
		}
	}

	// 直接返回对象
	c.JSON(http.StatusOK, metadata)
}

// 辅助函数

func filterEngines(engines []*models.Engine, filters engineFilters) []*models.Engine {
	if len(filters.EngineTypes) == 0 && len(filters.DataSourceTypes) == 0 {
		return engines
	}

	filtered := make([]*models.Engine, 0, len(engines))

	if len(filters.EngineTypes) > 0 {
		engineTypeSet := make(map[string]bool, len(filters.EngineTypes))
		for _, engineType := range filters.EngineTypes {
			engineTypeSet[strings.ToLower(engineType)] = true
		}
		for _, engine := range engines {
			if engineTypeSet[strings.ToLower(engine.EngineType)] {
				filtered = append(filtered, engine)
			}
		}
		engines = filtered
		filtered = make([]*models.Engine, 0, len(engines))
	}

	if len(filters.DataSourceTypes) > 0 {
		dataSourceTypeSet := make(map[string]bool, len(filters.DataSourceTypes))
		for _, dataSourceType := range filters.DataSourceTypes {
			dataSourceTypeSet[strings.ToLower(dataSourceType)] = true
		}
		for _, engine := range engines {
			if dataSourceTypeSet[getDataSourceNodeType(engine.EngineType)] {
				filtered = append(filtered, engine)
			}
		}
		return filtered
	}

	return engines
}

func isDatabaseEngine(engineType string) bool {
	switch strings.ToLower(engineType) {
	case "postgresql", "mysql", "doris", "clickhouse", "mongodb", "spark_sql":
		return true
	default:
		return false
	}
}

func getDataSourceNodeType(engineType string) string {
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

func columnsFromFields(fields []datatype.FieldInfo) []columnMetadata {
	columns := make([]columnMetadata, 0, len(fields))
	for _, field := range fields {
		columns = append(columns, columnMetadata{
			Name: field.Name,
			Type: string(field.Type),
		})
	}
	return columns
}

// parseCommaSeparated 解析逗号分隔的字符串
func parseCommaSeparated(s string) []string {
	if s == "" {
		return []string{}
	}

	result := []string{}
	for _, part := range splitAndTrim(s, ",") {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// splitAndTrim 分割并去除空格
func splitAndTrim(s, sep string) []string {
	parts := []string{}
	for _, part := range splitString(s, sep) {
		trimmed := trimString(part)
		parts = append(parts, trimmed)
	}
	return parts
}

// splitString 简单字符串分割（避免使用 strings.Split）
func splitString(s, sep string) []string {
	if s == "" {
		return []string{}
	}

	result := []string{}
	start := 0

	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}

	// 添加最后一段
	result = append(result, s[start:])
	return result
}

// trimString 去除字符串两端空格
func trimString(s string) string {
	start := 0
	end := len(s)

	// 去除前导空格
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	// 去除尾随空格
	for start < end && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}
