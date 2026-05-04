package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
	pq "github.com/lib/pq"
)

// FeatureHandler 处理要素相关的请求（用于地图与表格关联）
type FeatureHandler struct {
	systemClient *commonClient.SystemClient
	metadataRepo *repository.MetadataRepository
}

// NewFeatureHandler 创建要素处理器
func NewFeatureHandler(systemClient *commonClient.SystemClient, metadataRepo *repository.MetadataRepository) *FeatureHandler {
	return &FeatureHandler{
		systemClient: systemClient,
		metadataRepo: metadataRepo,
	}
}

// GetFeatureCentroid 获取要素的几何中心点（用于表格行定位到地图）
// GET /api/manager/engines/:id/spatial/features/:feature_id/centroid?schema=xxx&table=xxx&geom=geom
// @Summary 获取要素几何中心点 | Get feature centroid
// @Description 获取指定要素的几何中心点坐标，用于表格行定位到地图 | Get the centroid coordinates of a feature for map positioning
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param feature_id path string true "要素ID | Feature ID"
// @Param schema query string true "命名空间 | Namespace"
// @Param table query string true "数据项名称 | Item name"
// @Param geom query string false "几何字段名，默认geom | Geometry column name, default geom"
// @Param primary_key query string false "主键字段名，默认id | Primary key column, default id"
// @Success 200 {object} map[string]interface{} "中心点坐标 | Centroid coordinates"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "要素不存在 | Feature not found"
// @Router /getfeaturecentroid [get]
// @Security BearerAuth
func (h *FeatureHandler) GetFeatureCentroid(c *gin.Context) {
	// 1. 解析路径参数
	engineIDStr := c.Param("id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id parameter"})
		return
	}

	featureIDStr := c.Param("feature_id")
	// 注意：featureID 可能不是数字（如 UUID），所以不转换类型

	// 2. 解析查询参数
	schema := c.Query("schema")
	if schema == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schema parameter is required"})
		return
	}

	table := c.Query("table")
	if table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "table parameter is required"})
		return
	}

	geomCol := c.DefaultQuery("geom", "geom")
	primaryKey := c.DefaultQuery("primary_key", "id")

	// 3. 获取资源信息
	if h.systemClient == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "system client not available"})
		return
	}

	resource, err := h.systemClient.GetEngine(uint(engineID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return
	}

	// 4. 验证资源类型（只支持 PostgreSQL）
	if resource.EngineType != "postgresql" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only postgresql engines are supported"})
		return
	}

	// 5. 构建数据库连接（resource 已经是 *commonModels.Engine 类型，无需转换）
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build connection string"})
		return
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to connect to database"})
		return
	}
	defer db.Close()

	// 6. 构建安全的 SQL 查询（使用 pq.QuoteIdentifier 防止 SQL 注入）
	qSchema := pq.QuoteIdentifier(schema)
	qTable := pq.QuoteIdentifier(table)
	qGeom := pq.QuoteIdentifier(geomCol)
	qPrimaryKey := pq.QuoteIdentifier(primaryKey)

	sqlStr := fmt.Sprintf(`
		SELECT
			ST_X(ST_Centroid(ST_Transform(%s, 4326))) AS lon,
			ST_Y(ST_Centroid(ST_Transform(%s, 4326))) AS lat
		FROM %s.%s
		WHERE %s = $1
	`, qGeom, qGeom, qSchema, qTable, qPrimaryKey)

	// 7. 执行查询
	var lon, lat sql.NullFloat64
	err = db.QueryRowContext(c.Request.Context(), sqlStr, featureIDStr).Scan(&lon, &lat)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "feature not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("query failed: %v", err)})
		return
	}

	if !lon.Valid || !lat.Valid {
		c.JSON(http.StatusNotFound, gin.H{"error": "feature has no valid geometry"})
		return
	}

	// 8. 返回中心点坐标
	c.JSON(http.StatusOK, gin.H{
		"lon": lon.Float64,
		"lat": lat.Float64,
	})
}

// GetFeatureGeometry 获取要素的完整几何（用于地图高亮显示）
// GET /api/manager/engines/:id/spatial/features/:feature_id/geometry?schema=xxx&table=xxx&geom=geom
// @Summary 获取要素完整几何 | Get feature geometry
// @Description 获取指定要素的完整几何数据（GeoJSON格式），用于地图高亮显示 | Get full geometry of a feature in GeoJSON format for map highlighting
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param feature_id path string true "要素ID | Feature ID"
// @Param schema query string true "命名空间 | Namespace"
// @Param table query string true "数据项名称 | Item name"
// @Param geom query string false "几何字段名，默认geom | Geometry column name, default geom"
// @Param primary_key query string false "主键字段名，默认id | Primary key column, default id"
// @Success 200 {object} map[string]interface{} "几何数据及边界框 | Geometry data and bounding box"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "要素不存在 | Feature not found"
// @Router /getfeaturegeometry [get]
// @Security BearerAuth
func (h *FeatureHandler) GetFeatureGeometry(c *gin.Context) {
	// 1. 解析路径参数
	engineIDStr := c.Param("id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id parameter"})
		return
	}

	featureIDStr := c.Param("feature_id")

	// 2. 解析查询参数
	schema := c.Query("schema")
	if schema == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schema parameter is required"})
		return
	}

	table := c.Query("table")
	if table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "table parameter is required"})
		return
	}

	geomCol := c.DefaultQuery("geom", "geom")
	primaryKey := c.DefaultQuery("primary_key", "id")

	// 3. 获取资源信息
	if h.systemClient == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "system client not available"})
		return
	}

	resource, err := h.systemClient.GetEngine(uint(engineID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return
	}

	// 4. 验证资源类型（只支持 PostgreSQL）
	if resource.EngineType != "postgresql" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only postgresql engines are supported"})
		return
	}

	// 5. 构建数据库连接
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build connection string"})
		return
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to connect to database"})
		return
	}
	defer db.Close()

	// 6. 构建安全的 SQL 查询（获取 GeoJSON 格式的几何）
	qSchema := pq.QuoteIdentifier(schema)
	qTable := pq.QuoteIdentifier(table)
	qGeom := pq.QuoteIdentifier(geomCol)
	qPrimaryKey := pq.QuoteIdentifier(primaryKey)

	sqlStr := fmt.Sprintf(`
		SELECT
			ST_AsGeoJSON(ST_Transform(%s, 4326)) AS geojson,
			ST_X(ST_Centroid(ST_Transform(%s, 4326))) AS lon,
			ST_Y(ST_Centroid(ST_Transform(%s, 4326))) AS lat,
			ST_XMin(ST_Transform(%s, 4326)) AS min_lon,
			ST_YMin(ST_Transform(%s, 4326)) AS min_lat,
			ST_XMax(ST_Transform(%s, 4326)) AS max_lon,
			ST_YMax(ST_Transform(%s, 4326)) AS max_lat
		FROM %s.%s
		WHERE %s = $1
	`, qGeom, qGeom, qGeom, qGeom, qGeom, qGeom, qGeom, qSchema, qTable, qPrimaryKey)

	// 7. 执行查询
	var geojson sql.NullString
	var lon, lat, minLon, minLat, maxLon, maxLat sql.NullFloat64
	err = db.QueryRowContext(c.Request.Context(), sqlStr, featureIDStr).Scan(&geojson, &lon, &lat, &minLon, &minLat, &maxLon, &maxLat)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "feature not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("query failed: %v", err)})
		return
	}

	if !geojson.Valid {
		c.JSON(http.StatusNotFound, gin.H{"error": "feature has no valid geometry"})
		return
	}

	// 8. 返回几何、中心点坐标和边界框
	c.JSON(http.StatusOK, gin.H{
		"geojson": geojson.String,
		"centroid": gin.H{
			"lon": lon.Float64,
			"lat": lat.Float64,
		},
		"extent": []float64{minLon.Float64, minLat.Float64, maxLon.Float64, maxLat.Float64},
	})
}
