package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/addp/common/catalogview"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/dbbridge"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type graphNodeShapeOption struct {
	Name       string               `json:"name"`
	Kind       string               `json:"kind,omitempty"`
	Labels     []string             `json:"labels,omitempty"`
	Count      *int64               `json:"count,omitempty"`
	Properties []datatype.FieldInfo `json:"properties,omitempty"`
}

func authTokenFromContext(c *gin.Context) string {
	authToken := c.GetString("jwt_token")
	if authToken != "" {
		return authToken
	}
	if header := c.GetHeader("Authorization"); len(header) > 7 && header[:7] == "Bearer " {
		return header[7:]
	}
	return ""
}

// DataSourceHandler 处理数据源相关的代理请求
// 为 Service 前端提供统一的数据源访问接口，内部调用 System 和 Meta 模块
type DataSourceHandler struct {
	systemClient *commonClient.SystemClient
	metaClient   *commonClient.MetaClient
	metaBaseURL  string
}

// NewDataSourceHandler 创建新的数据源处理器
func NewDataSourceHandler(systemClient *commonClient.SystemClient, metaBaseURL string) *DataSourceHandler {
	return &DataSourceHandler{
		systemClient: systemClient,
		metaBaseURL:  metaBaseURL,
	}
}

// GetEngines 获取存储引擎列表
// GET /api/service/engines
// 内部调用：System API - GET /api/system/engines
func (h *DataSourceHandler) GetEngines(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing tenant_id in token"})
		return
	}

	// 调用 System API 获取引擎列表（空字符串表示获取所有类型）
	engines, err := h.systemClient.ListEngines("", tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engines: " + err.Error()})
		return
	}

	// 根据 API 规范：查询列表（无分页）直接返回数组
	c.JSON(http.StatusOK, engines)
}

// GetEngineTree 获取引擎的元数据树
// @Summary 获取引擎元数据树 | Get engine metadata tree
// @Tags 数据源 | Data Sources
// @Produce json
// @Param engine_id path int true "引擎ID | Engine ID"
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

	// 从 JWT token 中获取认证信息
	authToken := c.GetString("jwt_token")
	if authToken == "" {
		// 尝试从 Authorization header 提取
		if header := c.GetHeader("Authorization"); header != "" {
			if len(header) > 7 && header[:7] == "Bearer " {
				authToken = header[7:]
			}
		}
	}

	// 1. 获取引擎信息
	engine, err := h.systemClient.GetEngine(uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engine: " + err.Error()})
		return
	}

	// 2. 创建 MetaClient（使用用户的 JWT Token）
	metaClient := commonClient.NewMetaClient(h.metaBaseURL, authToken)

	// 3. 调用 Meta API 获取元数据树
	metadataTree, err := metaClient.GetMetadataTree(uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get metadata tree: " + err.Error()})
		return
	}

	// 4. 使用 TreeBuilder 转换为标准树结构
	treeBuilder := catalogview.NewTreeBuilder(nil)
	treeNode, err := treeBuilder.BuildFromMetadataTree(engine, metadataTree)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build tree: " + err.Error()})
		return
	}

	// 根据 API 规范：查询单个资源直接返回对象
	c.JSON(http.StatusOK, treeNode)
}

// GetNodeChildren 获取节点的子节点和项目（懒加载）
// @Summary 获取节点子项 | Get node children
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

	// 从 JWT token 中获取认证信息
	authToken := c.GetString("jwt_token")
	if authToken == "" {
		if header := c.GetHeader("Authorization"); header != "" {
			if len(header) > 7 && header[:7] == "Bearer " {
				authToken = header[7:]
			}
		}
	}

	// 创建 MetaClient
	metaClient := commonClient.NewMetaClient(h.metaBaseURL, authToken)

	// 1. 调用 Meta API 获取子节点（node → node，如 schema → sub-schema）
	children, err := metaClient.GetNodeChildren(uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get node children: " + err.Error()})
		return
	}

	// 2. 调用 Meta API 获取节点项目（node → items，如 schema → tables）
	items, err := metaClient.GetNodeItems(uint(nodeID))
	if err != nil {
		// items 获取失败不影响整体流程，只记录日志
		// 某些节点类型（如 table）可能没有 items
		items = []models.MetaItem{} // 使用空数组
	}

	// 3. 如果既没有子节点也没有项目，返回空数组
	if len(children) == 0 && len(items) == 0 {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	// 4. 获取引擎信息
	var engineID uint
	if len(children) > 0 {
		engineID = children[0].EngineID
	} else if len(items) > 0 {
		engineID = items[0].EngineID
	}

	engine, err := h.systemClient.GetEngine(engineID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engine: " + err.Error()})
		return
	}

	// 5. 使用 TreeBuilder 的统一方法转换 Items 为 MetaNodes
	treeBuilder := catalogview.NewTreeBuilder(nil)
	itemNodes := treeBuilder.ConvertMetaItems(items)

	// 6. 合并子节点和转换后的 Item 节点
	allNodes := make([]*models.MetaNode, 0, len(children)+len(itemNodes))

	// 添加子节点
	for i := range children {
		allNodes = append(allNodes, &children[i])
	}

	// 添加转换后的 Item 节点
	allNodes = append(allNodes, itemNodes...)

	// 7. 使用 TreeBuilder 转换为标准树节点
	treeNodes := treeBuilder.ConvertMetaNodes(engine, allNodes)

	// 根据 API 规范：查询列表（无分页）直接返回数组
	c.JSON(http.StatusOK, treeNodes)
}

// GetGraphNodeShapes 获取 graph item 的节点形状列表。
// @Summary 获取图节点形状 | Get graph node shapes
// @Tags 数据源 | Data Sources
// @Produce json
// @Param engine_id query int true "引擎ID | Engine ID"
// @Param database query string false "数据库名 | Database name"
// @Success 200 {array} graphNodeShapeOption
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /graphs/node-shapes [get]
// @Security BearerAuth
func (h *DataSourceHandler) GetGraphNodeShapes(c *gin.Context) {
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

	database := c.Query("database")
	if database == "" {
		database = "neo4j"
	}

	metaClient := commonClient.NewMetaClient(h.metaBaseURL, authTokenFromContext(c))
	item, err := metaClient.GetItemByCatalogPath(uint(engineID), fmt.Sprintf("%s.graph", database))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get graph item: " + err.Error()})
		return
	}

	graphInfo := datatype.GraphInfoFromPayload(commonJSON.Section(item.Attributes, "type_info.graph"))
	if graphInfo == nil {
		c.JSON(http.StatusOK, []graphNodeShapeOption{})
		return
	}

	shapes := make([]graphNodeShapeOption, 0, len(graphInfo.NodeShapes))
	for _, shape := range graphInfo.NodeShapes {
		shapes = append(shapes, graphNodeShapeOption{
			Name:       shape.Name,
			Kind:       shape.Kind,
			Labels:     append([]string(nil), shape.Labels...),
			Count:      shape.Count,
			Properties: append([]datatype.FieldInfo(nil), shape.Properties...),
		})
	}

	c.JSON(http.StatusOK, shapes)
}

// GetTableMetadata 获取表的元数据（用于检测几何列）
// @Summary 获取表元数据 | Get table metadata
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
func (h *DataSourceHandler) GetTableMetadata(c *gin.Context) {
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

	// 从 JWT token 中获取认证信息
	authToken := c.GetString("jwt_token")
	if authToken == "" {
		if header := c.GetHeader("Authorization"); header != "" {
			if len(header) > 7 && header[:7] == "Bearer " {
				authToken = header[7:]
			}
		}
	}

	// 创建 MetaClient
	metaClient := commonClient.NewMetaClient(h.metaBaseURL, authToken)

	catalogPath := fmt.Sprintf("%s.%s", schema, table)

	// 调用 Meta API 获取字段信息
	fields, err := metaClient.GetItemFieldsByCatalogPath(uint(engineID), catalogPath, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get table fields: " + err.Error()})
		return
	}

	var geometryColumn string
	var srid int
	var geometryType string
	hasGeometry := false

	if spatialMeta, err := metaClient.GetItemSpatialMetadataByCatalogPath(uint(engineID), catalogPath); err == nil && spatialMeta != nil {
		geometryColumn = spatialMeta.GeometryColumn
		srid = spatialMeta.SRID
		if len(spatialMeta.GeometryTypes) > 0 {
			geometryType = spatialMeta.GeometryTypes[0]
		}
		hasGeometry = geometryColumn != ""
	}

	// 根据 API 规范：查询单个资源直接返回对象
	c.JSON(http.StatusOK, gin.H{
		"has_geometry":    hasGeometry,
		"geometry_column": geometryColumn,
		"srid":            srid,
		"geometry_type":   geometryType,
		"fields":          fields,
	})
}

// GetTableSpatialMetadata 获取表的空间元数据（用于空间服务发布）
// @Summary 获取表空间元数据 | Get table spatial metadata
// @Tags 数据源 | Data Sources
// @Produce json
// @Param engine_id query int true "引擎ID | Engine ID"
// @Param schema query string true "Schema"
// @Param table query string true "表名 | Table"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /tables/spatial-metadata [get]
// @Security BearerAuth
func (h *DataSourceHandler) GetTableSpatialMetadata(c *gin.Context) {
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

	// 从 JWT token 中获取认证信息
	authToken := c.GetString("jwt_token")
	if authToken == "" {
		if header := c.GetHeader("Authorization"); header != "" {
			if len(header) > 7 && header[:7] == "Bearer " {
				authToken = header[7:]
			}
		}
	}

	// 创建 MetaClient
	metaClient := commonClient.NewMetaClient(h.metaBaseURL, authToken)

	// 调用 Meta API 获取空间元数据
	spatialMeta, err := metaClient.GetItemSpatialMetadataByCatalogPath(uint(engineID), fmt.Sprintf("%s.%s", schema, table))
	if err != nil {
		// 如果表不是空间表，返回空结果而不是错误
		c.JSON(http.StatusOK, gin.H{
			"has_geometry": false,
		})
		return
	}

	// 获取第一个几何类型（如果有多个）
	geometryType := ""
	if len(spatialMeta.GeometryTypes) > 0 {
		geometryType = spatialMeta.GeometryTypes[0]
	}

	// 根据 API 规范：查询单个资源直接返回对象
	c.JSON(http.StatusOK, gin.H{
		"has_geometry":    true,
		"geometry_column": spatialMeta.GeometryColumn,
		"srid":            spatialMeta.SRID,
		"geometry_type":   geometryType,
		"extent":          spatialMeta.Extent,
	})
}

// GetSQLSpatialMetadata 检测 SQL 查询结果的空间元数据
// @Summary 获取 SQL 空间元数据 | Get SQL spatial metadata
// @Tags 数据源 | Data Sources
// @Accept json
// @Produce json
// @Param request body map[string]interface{} true "SQL 空间元数据请求 | SQL spatial metadata request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /sql/spatial-metadata [post]
// @Security BearerAuth
func (h *DataSourceHandler) GetSQLSpatialMetadata(c *gin.Context) {
	var req struct {
		EngineID uint   `json:"engine_id" binding:"required"`
		SQL      string `json:"sql" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// 1. 获取引擎信息
	engine, err := h.systemClient.GetEngine(req.EngineID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get engine: " + err.Error()})
		return
	}

	// 2. 转换为 common models 并创建连接池
	commonEngine := &models.Engine{
		ID:             engine.ID,
		EngineType:     engine.EngineType,
		ConnectionInfo: models.ConnectionInfo(engine.ConnectionInfo),
	}

	db, err := dbbridge.GetOrCreatePool(commonEngine, dbbridge.DefaultPoolConfig())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to database: " + err.Error()})
		return
	}

	// 3. 检测 SQL 查询结果中的空间字段
	spatialMeta, err := h.detectSQLSpatialFields(c.Request.Context(), db, engine.EngineType, req.SQL)
	if err != nil {
		// 如果 SQL 查询结果不包含空间字段，返回空结果而不是错误
		c.JSON(http.StatusOK, gin.H{
			"has_geometry": false,
		})
		return
	}

	// 根据 API 规范：查询单个资源直接返回对象
	c.JSON(http.StatusOK, gin.H{
		"has_geometry":    spatialMeta.HasGeometry,
		"geometry_column": spatialMeta.GeometryColumn,
		"srid":            spatialMeta.SRID,
		"geometry_type":   spatialMeta.GeometryType,
		"geometry_types":  spatialMeta.GeometryTypes,
		"extent":          spatialMeta.Extent,
	})
}

// detectSQLSpatialFields 检测 SQL 查询结果中的空间字段
func (h *DataSourceHandler) detectSQLSpatialFields(
	ctx context.Context,
	db *gorm.DB,
	engineType string,
	sql string,
) (*SQLSpatialMetadata, error) {
	// 仅支持 PostgreSQL（PostGIS）
	if engineType != "postgresql" {
		return nil, fmt.Errorf("spatial detection only supported for PostgreSQL")
	}

	// 1. 执行 SQL 查询（LIMIT 1）获取列信息
	testSQL := fmt.Sprintf("SELECT * FROM (%s) AS subquery LIMIT 1", sql)
	rows, err := db.WithContext(ctx).Raw(testSQL).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	defer rows.Close()

	// 获取列类型
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// 2. 查找几何列
	var geometryColumn string
	for _, colType := range columnTypes {
		dataType := colType.DatabaseTypeName()
		// PostGIS 几何类型：geometry, geography
		if dataType == "geometry" || dataType == "geography" {
			geometryColumn = colType.Name()
			break
		}
	}

	if geometryColumn == "" {
		return nil, fmt.Errorf("no geometry column found")
	}

	// 3. 查询几何列的 SRID 和类型
	// 使用 ST_SRID 和 ST_GeometryType 函数
	metaSQL := fmt.Sprintf(`
		SELECT
			ST_SRID(%s) AS srid,
			ST_GeometryType(%s) AS geom_type
		FROM (%s) AS subquery
		WHERE %s IS NOT NULL
		LIMIT 1
	`, geometryColumn, geometryColumn, sql, geometryColumn)

	var srid int
	var geomTypeRaw string
	err = db.WithContext(ctx).Raw(metaSQL).Row().Scan(&srid, &geomTypeRaw)
	if err != nil {
		// 如果查询失败（可能是空表），使用默认值
		srid = 4326
		geomTypeRaw = "GEOMETRY"
	}

	// PostGIS 返回的类型格式为 "ST_Point", "ST_LineString" 等
	// 需要移除 "ST_" 前缀
	geometryType := geomTypeRaw
	if len(geomTypeRaw) > 3 && geomTypeRaw[:3] == "ST_" {
		geometryType = geomTypeRaw[3:]
	}

	// 4. 计算空间范围（extent）
	extentSQL := fmt.Sprintf(`
		SELECT
			ST_XMin(extent) AS min_x,
			ST_YMin(extent) AS min_y,
			ST_XMax(extent) AS max_x,
			ST_YMax(extent) AS max_y
		FROM (
			SELECT ST_Extent(%s) AS extent
			FROM (%s) AS subquery
		) AS extent_query
	`, geometryColumn, sql)

	var minX, minY, maxX, maxY *float64
	err = db.WithContext(ctx).Raw(extentSQL).Row().Scan(&minX, &minY, &maxX, &maxY)

	var extent map[string]interface{}
	if err == nil && minX != nil && minY != nil && maxX != nil && maxY != nil {
		extent = map[string]interface{}{
			"minX": *minX,
			"minY": *minY,
			"maxX": *maxX,
			"maxY": *maxY,
		}
	}

	return &SQLSpatialMetadata{
		HasGeometry:    true,
		GeometryColumn: geometryColumn,
		SRID:           srid,
		GeometryType:   geometryType,
		GeometryTypes:  []string{geometryType},
		Extent:         extent,
	}, nil
}

// SQLSpatialMetadata SQL 查询结果的空间元数据
type SQLSpatialMetadata struct {
	HasGeometry    bool                   `json:"has_geometry"`
	GeometryColumn string                 `json:"geometry_column"`
	SRID           int                    `json:"srid"`
	GeometryType   string                 `json:"geometry_type"`
	GeometryTypes  []string               `json:"geometry_types"`
	Extent         map[string]interface{} `json:"extent"`
}

// HealthCheck 健康检查
func (h *DataSourceHandler) HealthCheck(c *gin.Context) {
	// 检查 System 和 Meta 服务的连通性
	systemOK := h.systemClient != nil
	metaOK := h.metaBaseURL != ""

	status := "healthy"
	if !systemOK || !metaOK {
		status = "degraded"
	}

	c.JSON(http.StatusOK, gin.H{
		"status": status,
		"checks": gin.H{
			"system": systemOK,
			"meta":   metaOK,
		},
		"message": fmt.Sprintf("DataSource handler status: %s", status),
	})
}
