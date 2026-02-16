package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// TileEndpointHandler 瓦片端点处理器
// ============================================================================

// TileEndpointHandler 处理瓦片服务的公开端点
type TileEndpointHandler struct {
	tileServiceService *service.TileServiceService
	staticTileService  *service.StaticTileService
	dynamicTileService *service.DynamicTileService
	tileCacheService   *service.TileCacheService
}

// NewTileEndpointHandler 创建瓦片端点处理器
func NewTileEndpointHandler(
	tileServiceService *service.TileServiceService,
	staticTileService *service.StaticTileService,
	dynamicTileService *service.DynamicTileService,
	tileCacheService *service.TileCacheService,
) *TileEndpointHandler {
	return &TileEndpointHandler{
		tileServiceService: tileServiceService,
		staticTileService:  staticTileService,
		dynamicTileService: dynamicTileService,
		tileCacheService:   tileCacheService,
	}
}

// GetXYZTile 处理 XYZ Tiles 请求
// @Summary 获取 XYZ 瓦片
// @Tags Tiles
// @Produce application/vnd.mapbox-vector-tile,image/png,image/jpeg
// @Param serviceName path string true "服务名称"
// @Param layerName path string true "图层名称"
// @Param z path int true "缩放级别"
// @Param x path int true "瓦片 X 坐标"
// @Param y path int true "瓦片 Y 坐标"
// @Param format path string true "瓦片格式 (mvt, png, jpg)"
// @Success 200 {file} binary
// @Router /tiles/{serviceName}/{layerName}/{z}/{x}/{y}.{format} [get]
func (h *TileEndpointHandler) GetXYZTile(c *gin.Context) {
	ctx := c.Request.Context()

	// 1. 解析路径参数
	serviceName := c.Param("serviceName")
	layerName := c.Param("layerName")
	zStr := c.Param("z")
	xStr := c.Param("x")

	// yformat 是通配符参数，格式为 "/12.mvt"，需要解析出 y 和 format
	yformat := c.Param("yformat")
	yformat = strings.TrimPrefix(yformat, "/") // 移除前导斜杠

	// 分离 y 和 format（例如 "12.mvt" -> y=12, format=mvt）
	parts := strings.Split(yformat, ".")
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tile format"})
		return
	}
	yStr := parts[0]
	format := parts[1]

	// 解析坐标
	z, err := strconv.Atoi(zStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid zoom level"})
		return
	}

	x, err := strconv.Atoi(xStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid X coordinate"})
		return
	}

	y, err := strconv.Atoi(yStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Y coordinate"})
		return
	}

	// 2. 查询瓦片服务（不过滤租户，因为可能是公开服务）
	tileService, err := h.tileServiceService.GetServiceModelByName(serviceName)
	if err != nil {
		logger.L().Error("瓦片服务不存在",
			"service_name", serviceName,
			"error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// 3. 检查访问权限
	if !tileService.PublicAccess {
		// 非公开服务需要 JWT 认证
		_, exists := c.Get("tenant_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
			return
		}

		// 验证租户权限
		userTenantID, _ := c.Get("tenant_id")
		if userTenantID.(uint) != tileService.TenantID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}
	}

	// 4. 查找图层
	var layer *models.TileServiceLayer
	for i := range tileService.Layers {
		if tileService.Layers[i].LayerName == layerName {
			layer = &tileService.Layers[i]
			break
		}
	}

	if layer == nil {
		logger.L().Error("图层不存在",
			"service_name", serviceName,
			"layer_name", layerName)
		c.JSON(http.StatusNotFound, gin.H{"error": "Layer not found"})
		return
	}

	// 5. 根据图层类型处理
	var tileData []byte
	var fromCache bool

	if layer.LayerType == "static" {
		// 静态瓦片：从 MinIO 读取
		tileData, err = h.staticTileService.GetStaticTile(ctx, layer, z, x, y, format)
		fromCache = true
		if err != nil {
			logger.L().Error("读取静态瓦片失败",
				"service_name", serviceName,
				"layer_name", layerName,
				"z", z, "x", x, "y", y,
				"error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get tile"})
			return
		}
	} else if layer.LayerType == "dynamic" {
		// 动态瓦片：检查缓存 → 实时生成 → 保存缓存

		// 5.1 检查缓存配置
		config := layer.LayerConfig

		cacheEnabled := false
		cacheTTL := 3600
		if cacheConfig, ok := config["cache"].(map[string]interface{}); ok {
			if enabled, ok := cacheConfig["enabled"].(bool); ok {
				cacheEnabled = enabled
			}
			if ttl, ok := cacheConfig["ttl"].(float64); ok {
				cacheTTL = int(ttl)
			}
		}

		// 5.2 尝试从缓存读取
		if cacheEnabled {
			tileData, err = h.tileCacheService.GetCachedTile(ctx, tileService.TenantID, tileService.ID, layer.ID, z, x, y)
			if err == nil && len(tileData) > 0 {
				fromCache = true
				logger.L().Debug("✅ 缓存命中", "z", z, "x", x, "y", y, "size", len(tileData))
			}
		}

		// 5.3 缓存未命中，实时生成
		if tileData == nil || len(tileData) == 0 {
			startTime := time.Now()
			tileData, err = h.dynamicTileService.GetDynamicTile(ctx, layer, z, x, y)
			duration := time.Since(startTime)

			if err != nil {
				logger.L().Error("动态瓦片生成失败",
					"service_name", serviceName,
					"layer_name", layerName,
					"z", z, "x", x, "y", y,
					"error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate tile"})
				return
			}

			// 5.4 异步保存到缓存
			if cacheEnabled && len(tileData) > 0 {
				go func() {
					bgCtx := context.Background()
					if err := h.tileCacheService.PutCachedTile(
						bgCtx,
						tileService.TenantID,
						tileService.ID,
						layer.ID,
						z, x, y,
						tileData,
						cacheTTL,
					); err != nil {
						logger.L().Error("缓存保存失败",
							"service_id", tileService.ID,
							"layer_id", layer.ID,
							"z", z, "x", x, "y", y,
							"error", err)
					}
				}()

				logger.L().Info("✅ 瓦片已生成并异步缓存",
					"z", z, "x", x, "y", y,
					"duration", duration,
					"size", len(tileData))
			}

			fromCache = false
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown layer type"})
		return
	}

	// 6. 返回瓦片
	c.Header("X-Cache", map[bool]string{true: "HIT", false: "MISS"}[fromCache])
	contentType := getContentType(format)
	c.Data(http.StatusOK, contentType, tileData)

	logger.L().Debug("瓦片请求成功",
		"service_name", serviceName,
		"layer_name", layerName,
		"z", z, "x", x, "y", y,
		"format", format,
		"size", len(tileData))
}

// getContentType 根据格式返回 Content-Type
func getContentType(format string) string {
	switch format {
	case "mvt":
		return "application/vnd.mapbox-vector-tile"
	case "png":
		return "image/png"
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
