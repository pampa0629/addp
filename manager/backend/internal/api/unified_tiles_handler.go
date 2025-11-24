package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// UnifiedTilesHandler 统一的 MVT 瓦片处理器
// RESTful API: GET /api/resources/{resource_id}/spatial/tiles/{schema}/{table}/{z}/{x}/{y}.mvt
type UnifiedTilesHandler struct {
	service *service.UnifiedMVTService
}

// NewUnifiedTilesHandler 创建统一的瓦片处理器
func NewUnifiedTilesHandler(service *service.UnifiedMVTService) *UnifiedTilesHandler {
	return &UnifiedTilesHandler{
		service: service,
	}
}

// GetTile 获取 MVT 瓦片
// GET /api/resources/:id/spatial/tiles/:schema/:table/:z/:x/:y.mvt
// Query params (optional):
//   - geom: 几何列名（默认 "geom"）
//   - srid: 空间参考系（默认 4326）
//   - cols: 返回列，逗号分隔（最多 8 列）
func (h *UnifiedTilesHandler) GetTile(c *gin.Context) {
	// 1. 解析路径参数
	resourceIDStr := c.Param("id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id parameter"})
		return
	}

	schema := c.Param("schema")
	if schema == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schema parameter is required"})
		return
	}

	table := c.Param("table")
	if table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "table parameter is required"})
		return
	}

	zStr := c.Param("z")
	z, err := strconv.Atoi(zStr)
	if err != nil || z < 0 || z > 22 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid z parameter (must be 0-22)"})
		return
	}

	xStr := c.Param("x")
	x, err := strconv.Atoi(xStr)
	if err != nil || x < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid x parameter"})
		return
	}

	yStr := c.Param("y")
	// Log for debugging
	fmt.Printf("DEBUG: Raw y parameter: '%s'\n", yStr)
	// The route is now /:y without .mvt, so no need to trim
	fmt.Printf("DEBUG: Using y parameter as-is: '%s'\n", yStr)
	y, err := strconv.Atoi(yStr)
	if err != nil || y < 0 {
		fmt.Printf("DEBUG: Failed to parse y='%s': err=%v\n", yStr, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid y parameter"})
		return
	}
	fmt.Printf("DEBUG: Parsed y=%d\n", y)

	// 2. 解析查询参数（可选）
	geomCol := c.DefaultQuery("geom", "geom")

	sridStr := c.DefaultQuery("srid", "4326")
	srid, err := strconv.Atoi(sridStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid srid parameter"})
		return
	}

	// 解析列列表
	var cols []string
	colsParam := c.Query("cols")
	if colsParam != "" {
		cols = strings.Split(colsParam, ",")
		// 去除空白字符
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
	}

	// 3. 获取租户 ID（从 JWT 中间件注入）
	var tenantID *uint
	if tid, exists := c.Get("tenant_id"); exists {
		if tidUint, ok := tid.(uint); ok {
			tenantID = &tidUint
		}
	}

	// 4. 调用统一服务获取瓦片
	response, err := h.service.GetTile(
		c.Request.Context(),
		tenantID,
		uint(resourceID),
		schema,
		table,
		geomCol,
		cols,
		z, x, y,
		srid,
	)
	if err != nil {
		fmt.Printf("ERROR: GetTile failed: %v\n", err)
		fmt.Printf("ERROR: Resource=%d, Schema=%s, Table=%s, Z=%d, X=%d, Y=%d\n", resourceID, schema, table, z, x, y)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 5. 设置响应头
	c.Header("Content-Type", "application/vnd.mapbox-vector-tile")

	if response.FromCache {
		// 缓存命中（数据已经是 gzip 格式）
		c.Header("Content-Encoding", "gzip")
		c.Header("Cache-Control", "public, max-age=86400") // 1 天
		c.Header("X-Cache-Status", "HIT")
	} else {
		// 实时生成（未压缩）
		c.Header("Cache-Control", "public, max-age=60") // 1 分钟
		c.Header("X-Cache-Status", "MISS")
		c.Header("X-Generation-Time", response.Duration.String())
	}

	// 6. 返回瓦片数据
	c.Data(http.StatusOK, "application/vnd.mapbox-vector-tile", response.Data)
}
