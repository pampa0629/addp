package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

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

// ResourceCapabilityHandler 处理 Service 模块仍需自有计算的资源辅助能力。
// 资源选择、资源树和表级空间元数据统一走 Meta resource-tree / item API。
type ResourceCapabilityHandler struct {
	systemClient *commonClient.SystemClient
	metaBaseURL  string
}

// NewResourceCapabilityHandler 创建新的资源能力处理器。
func NewResourceCapabilityHandler(systemClient *commonClient.SystemClient, metaBaseURL string) *ResourceCapabilityHandler {
	return &ResourceCapabilityHandler{
		systemClient: systemClient,
		metaBaseURL:  metaBaseURL,
	}
}

// GetGraphNodeShapes 获取 graph item 的节点形状列表。
// @Summary 获取图节点形状 | Get graph node shapes
// @Tags 资源能力 | Resource Capabilities
// @Produce json
// @Param engine_id query int true "引擎ID | Engine ID"
// @Param database query string false "数据库名 | Database name"
// @Success 200 {array} graphNodeShapeOption
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /graphs/node-shapes [get]
// @Security BearerAuth
func (h *ResourceCapabilityHandler) GetGraphNodeShapes(c *gin.Context) {
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

	graphInfo := graphInfoFromMetaAttributes(item.Attributes)
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

func graphInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.GraphInfo {
	return datatype.GraphInfoFromPayload(commonJSON.Section(attrs, "type_info.graph"))
}

// GetSQLSpatialMetadata 检测 SQL 查询结果的空间元数据
// @Summary 获取 SQL 空间元数据 | Get SQL spatial metadata
// @Tags 资源能力 | Resource Capabilities
// @Accept json
// @Produce json
// @Param request body map[string]interface{} true "SQL 空间元数据请求 | SQL spatial metadata request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /sql/spatial-metadata [post]
// @Security BearerAuth
func (h *ResourceCapabilityHandler) GetSQLSpatialMetadata(c *gin.Context) {
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
func (h *ResourceCapabilityHandler) detectSQLSpatialFields(
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
func (h *ResourceCapabilityHandler) HealthCheck(c *gin.Context) {
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
		"message": fmt.Sprintf("Resource capability handler status: %s", status),
	})
}
