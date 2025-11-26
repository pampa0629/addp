package api

import (
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// TileConfigHandler 瓦片配置处理器
type TileConfigHandler struct {
	quickViewService *service.QuickViewService
}

// NewTileConfigHandler 创建瓦片配置处理器
func NewTileConfigHandler(quickViewService *service.QuickViewService) *TileConfigHandler {
	return &TileConfigHandler{
		quickViewService: quickViewService,
	}
}

// TileConfigResponse 瓦片配置响应
type TileConfigResponse struct {
	MinZoom int       `json:"min_zoom"` // 最小 zoom 层级
	MaxZoom int       `json:"max_zoom"` // 最大 zoom 层级
	Extent  []float64 `json:"extent,omitempty"` // 地理范围（可选）
	SRID    int       `json:"srid,omitempty"`   // 坐标系（可选）
}

// GetTileConfig 获取指定表的瓦片配置
// GET /api/resources/:id/spatial/:schema/:table/tile-config
//
// 返回值：
// - min_zoom: 根据数据范围计算的最小 zoom 层级
// - max_zoom: 最大 zoom 层级（固定 18）
// - extent: 数据的地理范围（用于调试）
// - srid: 数据的坐标系
func (h *TileConfigHandler) GetTileConfig(c *gin.Context) {
	// 1. 解析参数
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

	// 2. 获取租户 ID（从 JWT 中间件注入）
	var tenantID uint
	if tid, exists := c.Get("tenant_id"); exists {
		if tidUint, ok := tid.(uint); ok {
			tenantID = tidUint
		}
	}

	// 3. 获取快显状态（包含 extent 和 srid）
	qv, err := h.quickViewService.GetStatus(
		c.Request.Context(),
		tenantID,
		uint(resourceID),
		schema,
		table,
	)

	if err != nil {
		logger.L().Error("Failed to get quick view status for tile config",
			"error", err,
			"resource_id", resourceID,
			"schema", schema,
			"table", table)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get tile config"})
		return
	}

	// 4. 计算 MinZoom
	minZoom := 6 // 默认值
	srid := 4326  // 默认 WGS84

	if qv.MinZoom != nil {
		// 使用快显配置的 MinZoom（手动指定或已计算）
		minZoom = *qv.MinZoom
		logger.L().Debug("Using configured MinZoom from quick view",
			"min_zoom", minZoom,
			"table", table)
	} else if len(qv.Extent) == 4 {
		// 基于 extent 动态计算
		// 注意：extent 应该从 Meta 的 spatial_metadata 中获取，包含 SRID
		// 这里从 quick_view 表读取（TODO: 后续从 Meta 获取更准确的 SRID）
		minZoom = spatial.CalculateMinZoomFromExtent(qv.Extent, srid)
		logger.L().Info("Calculated MinZoom from extent",
			"min_zoom", minZoom,
			"extent", qv.Extent,
			"srid", srid,
			"table", table)
	} else {
		logger.L().Warn("No extent available, using default MinZoom",
			"default", minZoom,
			"table", table)
	}

	// 5. 设置 MaxZoom
	maxZoom := 18
	if qv.MaxZoom > 0 {
		maxZoom = qv.MaxZoom
	}

	// 6. 返回配置
	response := TileConfigResponse{
		MinZoom: minZoom,
		MaxZoom: maxZoom,
		Extent:  qv.Extent,
		SRID:    srid,
	}

	logger.L().Info("Tile config returned",
		"resource_id", resourceID,
		"schema", schema,
		"table", table,
		"min_zoom", minZoom,
		"max_zoom", maxZoom)

	c.JSON(http.StatusOK, response)
}
