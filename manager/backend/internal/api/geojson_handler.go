package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/common/spatial"
	"github.com/gin-gonic/gin"
)

// GeoJSONHandler GeoJSON API 处理器
type GeoJSONHandler struct {
	systemClient *commonClient.SystemClient
}

// NewGeoJSONHandler 创建 GeoJSON 处理器
func NewGeoJSONHandler(systemClient *commonClient.SystemClient) *GeoJSONHandler {
	return &GeoJSONHandler{
		systemClient: systemClient,
	}
}

// GetGeoJSON 获取空间数据项的 GeoJSON 数据（轻量级，支持分页）
// GET /api/engines/:id/spatial/:schema/:table/geojson
// @Summary 获取GeoJSON数据 | Get GeoJSON data
// @Description 获取空间数据项（关系表）的GeoJSON格式数据，支持分页 | Get spatial item data in GeoJSON format with pagination support
// @Tags Manager
// @Produce application/geo+json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param schema path string true "命名空间 | Namespace"
// @Param table path string true "数据项名称 | Item name"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认1000，最大5000 | Page size, default 1000, max 5000"
// @Param geom_column query string false "几何列名，默认geom | Geometry column, default geom"
// @Success 200 {object} map[string]interface{} "GeoJSON FeatureCollection"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines/{id}/spatial/{schema}/{table}/geojson [get]
// @Security BearerAuth
func (h *GeoJSONHandler) GetGeoJSON(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")

	// 2. 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "1000"))
	geomColumn := c.DefaultQuery("geom_column", "geom") // 默认几何列名

	// 限制 pageSize
	if pageSize > 5000 {
		pageSize = 5000
	}
	if pageSize < 1 {
		pageSize = 100
	}

	// 3. 获取引擎连接信息
	if h.systemClient == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "system client not initialized"})
		return
	}

	engine, err := h.systemClient.GetEngine(uint(engineID))
	if err != nil {
		logger.L().Error("Failed to get engine", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get engine info"})
		return
	}

	db, err := spatial.GetPostGISPool(engine, nil)
	if err != nil {
		logger.L().Error("Failed to get PostGIS pool", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database connection failed"})
		return
	}

	// 6. 查询 GeoJSON 数据
	offset := (page - 1) * pageSize
	query := spatial.BuildPostGISGeoJSONPageQuery(schema, table, geomColumn, pageSize, offset)

	var geojsonStr string
	err = db.Raw(query).Scan(&geojsonStr).Error
	if err != nil {
		logger.L().Error("Failed to query GeoJSON", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	// 调试：打印查询结果的前 200 个字符
	if len(geojsonStr) > 200 {
		logger.L().Debug("GeoJSON query result (first 200 chars)", "result", geojsonStr[:200])
	} else {
		logger.L().Debug("GeoJSON query result", "result", geojsonStr)
	}

	// 7. 解析并返回 GeoJSON
	var geojson map[string]interface{}
	if err := json.Unmarshal([]byte(geojsonStr), &geojson); err != nil {
		logger.L().Error("Failed to parse GeoJSON", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid geojson"})
		return
	}

	logger.L().Debug("GeoJSON parsed successfully", "type", geojson["type"], "features_count", len(geojson["features"].([]interface{})))

	c.Header("Content-Type", "application/geo+json")
	c.JSON(http.StatusOK, geojson)
}

// GetGeoJSONMetadata 获取 GeoJSON 元数据（范围、记录数等）
// GET /api/engines/:id/spatial/:schema/:table/geojson/metadata
// @Summary 获取GeoJSON元数据 | Get GeoJSON metadata
// @Description 获取空间数据项（关系表）的元数据信息（记录数、地理范围、坐标系等）| Get spatial item metadata (record count, extent, SRID, etc.)
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param schema path string true "命名空间 | Namespace"
// @Param table path string true "数据项名称 | Item name"
// @Param geom_column query string false "几何列名，默认geom | Geometry column, default geom"
// @Success 200 {object} map[string]interface{} "元数据信息 | Metadata"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines/{id}/spatial/{schema}/{table}/geojson/metadata [get]
// @Security BearerAuth
func (h *GeoJSONHandler) GetGeoJSONMetadata(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")
	geomColumn := c.DefaultQuery("geom_column", "geom")

	// 2. 获取引擎连接信息
	if h.systemClient == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "system client not initialized"})
		return
	}

	engine, err := h.systemClient.GetEngine(uint(engineID))
	if err != nil {
		logger.L().Error("Failed to get engine", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get engine info"})
		return
	}

	db, err := spatial.GetPostGISPool(engine, nil)
	if err != nil {
		logger.L().Error("Failed to get PostGIS pool", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database connection failed"})
		return
	}

	// 5. 查询元数据
	type Metadata struct {
		Count  int64     `json:"count"`
		Extent []float64 `json:"extent"` // [minLng, minLat, maxLng, maxLat]
		SRID   int       `json:"srid"`
	}

	var metadata Metadata

	// 查询记录数
	// 重要：使用双引号括起 schema 和表名以保留大小写
	countQuery := spatial.BuildPostGISCountQuery(schema, table)
	if err := db.Raw(countQuery).Scan(&metadata.Count).Error; err != nil {
		logger.L().Error("Failed to query count", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query count failed"})
		return
	}

	// 查询范围
	// 重要：使用双引号括起列名、表名和 schema 名以保留大小写
	extentQuery := spatial.BuildPostGISExtentQuery(schema, table, geomColumn)

	var minLng, minLat, maxLng, maxLat float64
	err = db.Raw(extentQuery).Row().Scan(&minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		logger.L().Error("Failed to query extent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query extent failed"})
		return
	}

	// 单独查询原始 SRID (所有几何应该有相同的 SRID)
	// 重要：使用双引号括起列名、表名和 schema 名以保留大小写
	sridQuery := spatial.BuildPostGISSRIDQuery(schema, table, geomColumn)
	err = db.Raw(sridQuery).Scan(&metadata.SRID).Error
	if err != nil {
		logger.L().Error("Failed to query SRID", "error", err)
		// SRID 查询失败不是致命错误，继续
		metadata.SRID = 0
	}

	metadata.Extent = []float64{minLng, minLat, maxLng, maxLat}

	c.JSON(http.StatusOK, metadata)
}
