package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/common/spatial"
	manageri18n "github.com/addp/manager/i18n"
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
// @Description 获取空间数据项（关系表）的源坐标 GeoJSON 预览数据和 CRS 元数据，后端不做 CRS transform | Get source-CRS GeoJSON preview data and CRS metadata without backend CRS transform
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param schema path string true "Schema | Schema"
// @Param table path string true "数据项名称 | Item name"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认1000，最大5000 | Page size, default 1000, max 5000"
// @Param geom_column query string false "几何列名，默认geom | Geometry column, default geom"
// @Success 200 {object} map[string]interface{} "GeoJSON preview contract"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines/{id}/spatial/{schema}/{table}/geojson [get]
// @Security BearerAuth
func (h *GeoJSONHandler) GetGeoJSON(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		invalidEngineID(c)
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
		managerError(c, http.StatusInternalServerError, manageri18n.MsgSystemClientNotInitialized)
		return
	}

	engine, err := h.systemClient.GetEngine(uint(engineID))
	if err != nil {
		logger.L().Error("Failed to get engine", "error", err)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgEngineNotFound)
		return
	}

	db, err := spatial.GetPostGISPool(engine, nil)
	if err != nil {
		logger.L().Error("Failed to get PostGIS pool", "error", err)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgDatabaseConnectionFailed)
		return
	}

	// 6. 查询 GeoJSON 数据
	offset := (page - 1) * pageSize
	query := spatial.BuildPostGISGeoJSONPageQuery(schema, table, geomColumn, pageSize, offset)

	var geojsonStr string
	err = db.Raw(query).Scan(&geojsonStr).Error
	if err != nil {
		logger.L().Error("Failed to query GeoJSON", "error", err)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgQueryFailed)
		return
	}

	// 调试：打印查询结果的前 200 个字符
	if len(geojsonStr) > 200 {
		logger.L().Debug("GeoJSON query result (first 200 chars)", "result", geojsonStr[:200])
	} else {
		logger.L().Debug("GeoJSON query result", "result", geojsonStr)
	}

	// 7. 查询原始 SRID 并解析 GeoJSON
	sourceSRID := 0
	sridQuery := spatial.BuildPostGISSRIDQuery(schema, table, geomColumn)
	if err := db.Raw(sridQuery).Scan(&sourceSRID).Error; err != nil {
		logger.L().Warn("Failed to query GeoJSON source SRID", "error", err, "schema", schema, "table", table, "geom_column", geomColumn)
		sourceSRID = 0
	}

	var geojson map[string]interface{}
	if err := json.Unmarshal([]byte(geojsonStr), &geojson); err != nil {
		logger.L().Error("Failed to parse GeoJSON", "error", err)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgInvalidGeoJSON)
		return
	}

	if features, ok := geojson["features"].([]interface{}); ok {
		logger.L().Debug("GeoJSON parsed successfully", "type", geojson["type"], "features_count", len(features))
	} else {
		logger.L().Debug("GeoJSON parsed successfully", "type", geojson["type"])
	}

	response := spatialPreviewContract(geomColumn, sourceSRID)
	response["geojson"] = geojson
	response["page"] = page
	response["page_size"] = pageSize
	c.JSON(http.StatusOK, response)
}

// GetGeoJSONMetadata 获取 GeoJSON 元数据（范围、记录数等）
// GET /api/engines/:id/spatial/:schema/:table/geojson/metadata
// @Summary 获取GeoJSON元数据 | Get GeoJSON metadata
// @Description 获取空间数据项（关系表）的源坐标元数据信息，后端不做 CRS transform | Get source-CRS spatial item metadata without backend CRS transform
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param schema path string true "Schema | Schema"
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
		invalidEngineID(c)
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")
	geomColumn := c.DefaultQuery("geom_column", "geom")

	// 2. 获取引擎连接信息
	if h.systemClient == nil {
		managerError(c, http.StatusInternalServerError, manageri18n.MsgSystemClientNotInitialized)
		return
	}

	engine, err := h.systemClient.GetEngine(uint(engineID))
	if err != nil {
		logger.L().Error("Failed to get engine", "error", err)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgEngineNotFound)
		return
	}

	db, err := spatial.GetPostGISPool(engine, nil)
	if err != nil {
		logger.L().Error("Failed to get PostGIS pool", "error", err)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgDatabaseConnectionFailed)
		return
	}

	// 5. 查询元数据
	type Metadata struct {
		Count            int64     `json:"count"`
		Extent           []float64 `json:"extent"` // [minX, minY, maxX, maxY]
		ExtentSRID       int       `json:"extent_srid"`
		GeometryColumn   string    `json:"geometry_column"`
		SourceSRID       int       `json:"source_srid"`
		SourceCRS        string    `json:"source_crs,omitempty"`
		TransformStatus  string    `json:"transform_status"`
		PreviewHint      string    `json:"preview_hint"`
		TransformMessage string    `json:"transform_message,omitempty"`
	}

	var metadata Metadata

	// 查询记录数
	// 重要：使用双引号括起 schema 和表名以保留大小写
	countQuery := spatial.BuildPostGISCountQuery(schema, table)
	if err := db.Raw(countQuery).Scan(&metadata.Count).Error; err != nil {
		logger.L().Error("Failed to query count", "error", err)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgQueryCountFailed)
		return
	}

	// 查询范围
	// 重要：使用双引号括起列名、表名和 schema 名以保留大小写
	extentQuery := spatial.BuildPostGISRawExtentQuery(schema, table, geomColumn)

	var minX, minY, maxX, maxY float64
	err = db.Raw(extentQuery).Row().Scan(&minX, &minY, &maxX, &maxY)
	if err != nil {
		logger.L().Error("Failed to query extent", "error", err)
		managerError(c, http.StatusInternalServerError, manageri18n.MsgQueryExtentFailed)
		return
	}

	// 单独查询原始 SRID (所有几何应该有相同的 SRID)
	// 重要：使用双引号括起列名、表名和 schema 名以保留大小写
	sridQuery := spatial.BuildPostGISSRIDQuery(schema, table, geomColumn)
	err = db.Raw(sridQuery).Scan(&metadata.SourceSRID).Error
	if err != nil {
		logger.L().Error("Failed to query SRID", "error", err)
		// SRID 查询失败不是致命错误，继续
		metadata.SourceSRID = 0
	}

	metadata.Extent = []float64{minX, minY, maxX, maxY}
	metadata.ExtentSRID = metadata.SourceSRID
	contract := spatialPreviewContract(geomColumn, metadata.SourceSRID)
	metadata.GeometryColumn = geomColumn
	if sourceCRS, ok := contract["source_crs"].(string); ok {
		metadata.SourceCRS = sourceCRS
	}
	if status, ok := contract["transform_status"].(string); ok {
		metadata.TransformStatus = status
	}
	if hint, ok := contract["preview_hint"].(string); ok {
		metadata.PreviewHint = hint
	}
	if message, ok := contract["transform_message"].(string); ok {
		metadata.TransformMessage = message
	}

	c.JSON(http.StatusOK, metadata)
}
