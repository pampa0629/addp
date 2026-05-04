package api

import (
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// TileConfigHandler 瓦片配置处理器
type TileConfigHandler struct {
	quickViewService *service.QuickViewService
	systemClient     *commonClient.SystemClient
	cfg              *config.Config
}

// NewTileConfigHandler 创建瓦片配置处理器
func NewTileConfigHandler(
	quickViewService *service.QuickViewService,
	systemClient *commonClient.SystemClient,
	cfg *config.Config,
) *TileConfigHandler {
	return &TileConfigHandler{
		quickViewService: quickViewService,
		systemClient:     systemClient,
		cfg:              cfg,
	}
}

// TileConfigResponse 瓦片配置响应
type TileConfigResponse struct {
	MinZoom           int       `json:"min_zoom"`                       // 最小 zoom 层级
	MaxZoom           int       `json:"max_zoom"`                       // 最大 zoom 层级
	Extent            []float64 `json:"extent,omitempty"`               // 地理范围（可选）
	SRID              int       `json:"srid,omitempty"`                 // 坐标系（可选）
	RecordCount       int64     `json:"record_count,omitempty"`         // 表记录数
	EstimatedTiles    int       `json:"estimated_tiles,omitempty"`      // 预估瓦片总数（minZoom-maxZoom）
	AvgRecordsPerTile float64   `json:"avg_records_per_tile,omitempty"` // maxZoom 层级的平均记录数/瓦片
}

// GetTileConfig 获取指定空间数据项的瓦片配置
// GET /api/engines/:id/spatial/:schema/:table/tile-config
//
// 返回值：
// - min_zoom: 根据数据范围计算的最小 zoom 层级
// - max_zoom: 根据记录数智能计算的最大 zoom 层级
// - extent: 数据的地理范围（用于调试）
// - srid: 数据的坐标系
// @Summary 获取瓦片配置 | Get tile configuration
// @Description 根据空间数据范围和记录数智能计算瓦片的最小/最大缩放级别 | Intelligently calculate min/max zoom levels based on spatial extent and record count
// @Tags Manager
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param schema path string true "命名空间 | Namespace"
// @Param table path string true "数据项名称 | Item name"
// @Success 200 {object} TileConfigResponse "瓦片配置信息 | Tile configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines/{id}/spatial/{schema}/{table}/tile-config [get]
// @Security BearerAuth
func (h *TileConfigHandler) GetTileConfig(c *gin.Context) {
	// 1. 解析参数
	engineIDStr := c.Param("id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
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
		uint(engineID),
		schema,
		table,
	)

	if err != nil {
		logger.L().Error("Failed to get quick view status for tile config",
			"error", err,
			"engine_id", engineID,
			"schema", schema,
			"table", table)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get tile config"})
		return
	}

	// 4. 获取 extent（优先从快显记录，回退到数据库查询）
	minZoom := 6       // 默认值
	extentSRID := 4326 // 默认 WGS84
	extent := []float64{}
	var recordCount int64 // 表记录数

	if qv != nil && len(qv.Extent) == 4 {
		// 快显记录中有 extent
		extent = qv.Extent
		extentSRID = qv.ExtentSRID
		if extentSRID == 0 {
			extentSRID = 4326 // 向后兼容
		}
		logger.L().Debug("Using extent from quick view",
			"extent", extent,
			"extent_srid", extentSRID,
			"table", table)
	} else {
		// 没有快显记录或 extent 为空，从数据库动态查询
		queriedExtent, geomCol, err := spatial.QueryExtent(
			c.Request.Context(),
			h.systemClient,
			uint(engineID),
			schema,
			table,
		)
		if err != nil {
			logger.L().Warn("Failed to query extent from database",
				"error", err,
				"table", table)
		} else if len(queriedExtent) == 4 {
			extent = queriedExtent
			extentSRID = 4326 // queryExtentFromDB 返回的总是 WGS84
			logger.L().Info("Queried extent from database (no quick view)",
				"extent", extent,
				"geometry_column", geomCol,
				"table", table)
		}
	}

	// 5. 计算 MinZoom
	if qv != nil && qv.Status == "completed" && qv.MinZoom != nil {
		// 如果快显已完成，使用预计算的 minZoom
		minZoom = *qv.MinZoom
		logger.L().Debug("Using configured MinZoom from quick view (completed)",
			"min_zoom", minZoom,
			"table", table,
			"status", qv.Status)
	} else if len(extent) == 4 {
		// 有 extent 但未启用快显：直接使用计算值
		calculatedMin := spatial.CalculateMinZoomFromExtent(extent, extentSRID)
		minZoom = calculatedMin
		if minZoom < 1 {
			minZoom = 1
		}
		logger.L().Info("Calculated flexible MinZoom from extent",
			"calculated_min", calculatedMin,
			"flexible_min", minZoom,
			"extent", extent,
			"table", table)
	}

	// 5. 智能计算 MaxZoom（基于记录数）
	maxZoom := h.cfg.PreCache.MaxZoom // 默认18

	// 如果快显已完成且有 maxZoom，使用预计算值
	if qv != nil && qv.Status == "completed" && qv.MaxZoom > 0 {
		maxZoom = qv.MaxZoom
		logger.L().Debug("Using pre-computed MaxZoom from quick view",
			"max_zoom", maxZoom,
			"table", table)
	} else if len(extent) == 4 {
		// 动态计算 maxZoom（基于记录数和数据范围）
		// 从 Meta API 获取表记录数（而不是直接查询 PostgreSQL）
		// 尝试从 Meta API 获取空间元数据（包含 row_count）
		spatialMeta, err := h.quickViewService.GetSpatialMetadataFromMeta(
			c.Request.Context(),
			tenantID,
			uint(engineID),
			schema,
			table,
		)
		if err != nil {
			logger.L().Warn("Failed to get spatial metadata from Meta, using default maxZoom",
				"error", err,
				"table", table,
				"default_max_zoom", maxZoom)
		} else {
			recordCount = spatialMeta.RecordCount
			logger.L().Info("Got record count from Meta API",
				"record_count", recordCount,
				"table", table)
		}

		if recordCount > 0 {
			// 使用智能计算函数
			calculatedMaxZoom := spatial.CalculateMaxZoomByRecordCount(
				recordCount,
				h.cfg.PreCache.TargetRecordsPerTile,
				[4]float64{extent[0], extent[1], extent[2], extent[3]},
				extentSRID,
			)

			// 确保不超过全局最大值
			if calculatedMaxZoom > h.cfg.PreCache.MaxZoom {
				calculatedMaxZoom = h.cfg.PreCache.MaxZoom
			}

			maxZoom = calculatedMaxZoom

			logger.L().Info("Calculated MaxZoom based on record count from Meta",
				"record_count", recordCount,
				"target_records_per_tile", h.cfg.PreCache.TargetRecordsPerTile,
				"calculated_max_zoom", maxZoom,
				"table", table)
		}
	}

	// 6. 对 minZoom 进行后处理（在 maxZoom 计算完成后）
	// 先用原始 minZoom 计算 maxZoom（确保 maxZoom 基于正确的起点）
	// 然后再对 minZoom 减 2，让用户在更低层级也能看到数据
	originalMinZoom := minZoom
	minZoom = minZoom - 2
	if minZoom < 3 {
		minZoom = 3
	}

	logger.L().Info("Adjusted MinZoom for display",
		"original_min_zoom", originalMinZoom,
		"adjusted_min_zoom", minZoom,
		"max_zoom", maxZoom,
		"table", table)

	// 7. 计算预估瓦片总数和平均记录数
	var estimatedTiles int
	var avgRecordsPerTile float64

	if len(extent) == 4 {
		// 计算从 minZoom 到 maxZoom 的总瓦片数
		for z := minZoom; z <= maxZoom; z++ {
			tiles := spatial.CalculateTilesForZoom(z, [4]float64{extent[0], extent[1], extent[2], extent[3]}, extentSRID)
			estimatedTiles += len(tiles)
		}

		// 计算 maxZoom 层级的平均记录数/瓦片
		if recordCount > 0 {
			maxZoomTiles := spatial.CalculateTilesForZoom(maxZoom, [4]float64{extent[0], extent[1], extent[2], extent[3]}, extentSRID)
			if len(maxZoomTiles) > 0 {
				avgRecordsPerTile = float64(recordCount) / float64(len(maxZoomTiles))
			}
		}
	}

	// 7. 返回配置
	response := TileConfigResponse{
		MinZoom:           minZoom,
		MaxZoom:           maxZoom,
		RecordCount:       recordCount,
		EstimatedTiles:    estimatedTiles,
		AvgRecordsPerTile: avgRecordsPerTile,
	}

	// 确保 extent 总是有效
	if len(extent) == 4 {
		// 有有效的 extent，直接使用
		response.Extent = extent
		response.SRID = extentSRID
	} else {
		// 没有有效的 extent，使用中国大致范围作为降级
		response.Extent = []float64{73.5, 3.8, 135.1, 53.6} // 中国大致范围 [minLon, minLat, maxLon, maxLat]
		response.SRID = 4326
		logger.L().Warn("No valid extent available, using default China extent",
			"table", table,
			"default_extent", response.Extent)
	}

	logger.L().Info("Tile config returned",
		"engine_id", engineID,
		"schema", schema,
		"table", table,
		"min_zoom", minZoom,
		"max_zoom", maxZoom,
		"has_valid_extent", len(extent) == 4)

	c.JSON(http.StatusOK, response)
}
