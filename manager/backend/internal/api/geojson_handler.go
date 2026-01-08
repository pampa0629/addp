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
	query := fmt.Sprintf(`
		SELECT jsonb_build_object(
			'type', 'FeatureCollection',
			'features', jsonb_agg(
				jsonb_build_object(
					'type', 'Feature',
					'id', row_number() OVER (),
					'geometry', ST_AsGeoJSON(%s)::jsonb,
					'properties', to_jsonb(row.*) - '%s'
				)
			)
		) as geojson
		FROM (
			SELECT * FROM %s.%s
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

	// 7. 解析并返回 GeoJSON
	var geojson map[string]interface{}
	if err := json.Unmarshal([]byte(geojsonStr), &geojson); err != nil {
		logger.L().Error("Failed to parse GeoJSON", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid geojson"})
		return
	}

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
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", schema, table)
	if err := db.Raw(countQuery).Scan(&metadata.Count).Error; err != nil {
		logger.L().Error("Failed to query count", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query count failed"})
		return
	}

	// 查询范围和 SRID
	extentQuery := fmt.Sprintf(`
		SELECT
			ST_XMin(ST_Extent(ST_Transform(%s, 4326))) as min_lng,
			ST_YMin(ST_Extent(ST_Transform(%s, 4326))) as min_lat,
			ST_XMax(ST_Extent(ST_Transform(%s, 4326))) as max_lng,
			ST_YMax(ST_Extent(ST_Transform(%s, 4326))) as max_lat,
			ST_SRID(%s) as srid
		FROM %s.%s
	`, geomColumn, geomColumn, geomColumn, geomColumn, geomColumn, schema, table)

	var minLng, minLat, maxLng, maxLat float64
	err = db.Raw(extentQuery).Row().Scan(&minLng, &minLat, &maxLng, &maxLat, &metadata.SRID)
	if err != nil {
		logger.L().Error("Failed to query extent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query extent failed"})
		return
	}

	metadata.Extent = []float64{minLng, minLat, maxLng, maxLat}

	c.JSON(http.StatusOK, metadata)
}
