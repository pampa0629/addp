package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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

// GetGeoJSON 获取表的 GeoJSON 数据（轻量级，支持分页）
// GET /api/engines/:id/spatial/:schema/:table/geojson
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

	// 4. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(engine)
	if err != nil {
		logger.L().Error("Failed to build connection string", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to connect to database"})
		return
	}

	// 5. 连接数据库
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
	if err != nil {
		logger.L().Error("Failed to connect to database", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database connection failed"})
		return
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	// 6. 查询 GeoJSON 数据
	offset := (page - 1) * pageSize

	// 使用 ST_AsGeoJSON 直接生成 GeoJSON 格式的几何
	// 注意：必须在子查询中先计算 row_number()，不能在聚合函数内使用窗口函数
	// COALESCE 确保空结果返回空数组而不是 null
	// 重要：使用双引号括起列名、表名和 schema 名以保留大小写
	query := fmt.Sprintf(`
		SELECT jsonb_build_object(
			'type', 'FeatureCollection',
			'features', COALESCE(
				jsonb_agg(
					jsonb_build_object(
						'type', 'Feature',
						'id', row.row_id,
						'geometry', ST_AsGeoJSON(row."%s")::jsonb,
						'properties', to_jsonb(row.*) - '%s' - 'row_id'
					)
				),
				'[]'::jsonb
			)
		) as geojson
		FROM (
			SELECT
				row_number() OVER () as row_id,
				*
			FROM "%s"."%s"
			ORDER BY ctid
			LIMIT %d OFFSET %d
		) row
	`, geomColumn, geomColumn, schema, table, pageSize, offset)

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

	// 3. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(engine)
	if err != nil {
		logger.L().Error("Failed to build connection string", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to connect to database"})
		return
	}

	// 4. 连接数据库
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
	if err != nil {
		logger.L().Error("Failed to connect to database", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database connection failed"})
		return
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	// 5. 查询元数据
	type Metadata struct {
		Count  int64     `json:"count"`
		Extent []float64 `json:"extent"` // [minLng, minLat, maxLng, maxLat]
		SRID   int       `json:"srid"`
	}

	var metadata Metadata

	// 查询记录数
	// 重要：使用双引号括起 schema 和表名以保留大小写
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."%s"`, schema, table)
	if err := db.Raw(countQuery).Scan(&metadata.Count).Error; err != nil {
		logger.L().Error("Failed to query count", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query count failed"})
		return
	}

	// 查询范围
	// 重要：使用双引号括起列名、表名和 schema 名以保留大小写
	extentQuery := fmt.Sprintf(`
		SELECT
			ST_XMin(extent) as min_lng,
			ST_YMin(extent) as min_lat,
			ST_XMax(extent) as max_lng,
			ST_YMax(extent) as max_lat
		FROM (
			SELECT ST_Extent(ST_Transform("%s", 4326)) as extent
			FROM "%s"."%s"
		) subquery
	`, geomColumn, schema, table)

	var minLng, minLat, maxLng, maxLat float64
	err = db.Raw(extentQuery).Row().Scan(&minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		logger.L().Error("Failed to query extent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query extent failed"})
		return
	}

	// 单独查询原始 SRID (所有几何应该有相同的 SRID)
	// 重要：使用双引号括起列名、表名和 schema 名以保留大小写
	sridQuery := fmt.Sprintf(`SELECT ST_SRID("%s") FROM "%s"."%s" LIMIT 1`, geomColumn, schema, table)
	err = db.Raw(sridQuery).Scan(&metadata.SRID).Error
	if err != nil {
		logger.L().Error("Failed to query SRID", "error", err)
		// SRID 查询失败不是致命错误，继续
		metadata.SRID = 0
	}

	metadata.Extent = []float64{minLng, minLat, maxLng, maxLat}

	c.JSON(http.StatusOK, metadata)
}
