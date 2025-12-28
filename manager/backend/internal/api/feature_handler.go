package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
	pq "github.com/lib/pq"
)

// FeatureHandler 处理要素相关的请求（用于地图与表格关联）
type FeatureHandler struct {
	resourceRepo *repository.ResourceRepository
	metadataRepo *repository.MetadataRepository
}

// NewFeatureHandler 创建要素处理器
func NewFeatureHandler(resourceRepo *repository.ResourceRepository, metadataRepo *repository.MetadataRepository) *FeatureHandler {
	return &FeatureHandler{
		resourceRepo: resourceRepo,
		metadataRepo: metadataRepo,
	}
}

// GetFeatureCentroid 获取要素的几何中心点（用于表格行定位到地图）
// GET /api/engines/:id/features/:feature_id/centroid?schema=xxx&table=xxx&geom=geom
func (h *FeatureHandler) GetFeatureCentroid(c *gin.Context) {
	// 1. 解析路径参数
	resourceIDStr := c.Param("id")
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
	resource, err := h.resourceRepo.GetByID(uint(engineID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return
	}

	// 4. 验证资源类型（只支持 PostgreSQL）
	if resource.EngineType != "postgresql" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only postgresql engines are supported"})
		return
	}

	// 5. 转换 Resource 类型为 common.Engine
	commonResource := &commonModels.Engine{
		ID:             resource.ID,
		Name:           resource.Name,
		EngineType:     resource.EngineType,
		ConnectionInfo: commonModels.ConnectionInfo(resource.ConnectionInfo),
		Description:    resource.Description,
		IsActive:       resource.IsActive,
		CreatedBy:      resource.CreatedBy,
	}
	if resource.TenantID != nil {
		tenantID := *resource.TenantID
		commonResource.TenantID = &tenantID
	}

	// 6. 构建数据库连接
	connStr, err := commonModels.BuildConnectionString(commonResource)
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

	// 7. 构建安全的 SQL 查询（使用 pq.QuoteIdentifier 防止 SQL 注入）
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

	// 8. 执行查询
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

	// 9. 返回中心点坐标
	c.JSON(http.StatusOK, gin.H{
		"lon": lon.Float64,
		"lat": lat.Float64,
	})
}
