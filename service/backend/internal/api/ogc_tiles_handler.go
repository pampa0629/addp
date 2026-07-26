package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
	"github.com/addp/service/internal/models"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// OGC Tiles API Handler - OGC API - Tiles 1.0 标准实现
// ============================================================================

// OGCTilesHandler 处理 OGC Tiles API 请求
type OGCTilesHandler struct {
	tileServiceService  tileServiceLookup
	tileEndpointHandler *TileEndpointHandler
}

// NewOGCTilesHandler 创建 OGC Tiles API 处理器
func NewOGCTilesHandler(
	tileServiceService tileServiceLookup,
	tileEndpointHandler *TileEndpointHandler,
) *OGCTilesHandler {
	return &OGCTilesHandler{
		tileServiceService:  tileServiceService,
		tileEndpointHandler: tileEndpointHandler,
	}
}

// ============================================================================
// Landing Page
// ============================================================================

// GetLandingPage 返回 OGC Tiles API Landing Page
// @Summary OGC Tiles API Landing Page | OGC Tiles API Landing Page
// @Tags OGC Tiles
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Success 200 {object} map[string]interface{} "Landing Page | Landing Page"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "public"
// @Router /ogc/tiles/{serviceName} [get]
func (h *OGCTilesHandler) GetLandingPage(c *gin.Context) {
	serviceName := c.Param("serviceName")
	host := c.Request.Host

	tileService, ok := h.getAuthorizedService(c, serviceName)
	if !ok {
		return
	}

	// 构建 Landing Page
	landingPage := map[string]interface{}{
		"title":       tileService.Title,
		"description": tileService.Description,
		"links": []map[string]string{
			{
				"href":  fmt.Sprintf("http://%s/ogc/tiles/%s", host, serviceName),
				"rel":   "self",
				"type":  "application/json",
				"title": "This document",
			},
			{
				"href":  fmt.Sprintf("http://%s/ogc/tiles/%s/conformance", host, serviceName),
				"rel":   "conformance",
				"type":  "application/json",
				"title": "OGC API conformance classes",
			},
			{
				"href":  fmt.Sprintf("http://%s/ogc/tiles/%s/tileMatrixSets", host, serviceName),
				"rel":   "http://www.opengis.net/def/rel/ogc/1.0/tiling-schemes",
				"type":  "application/json",
				"title": "Tile Matrix Sets",
			},
			{
				"href":  fmt.Sprintf("http://%s/ogc/tiles/%s/tiles", host, serviceName),
				"rel":   "http://www.opengis.net/def/rel/ogc/1.0/tilesets-vector",
				"type":  "application/json",
				"title": "Tilesets",
			},
		},
	}

	c.JSON(http.StatusOK, landingPage)

	logger.L().Info("OGC Tiles Landing Page 请求成功",
		"service_name", serviceName)
}

// ============================================================================
// Conformance
// ============================================================================

// GetConformance 返回 OGC API 符合性声明
// @Summary OGC API Conformance | OGC API Conformance
// @Tags OGC Tiles
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Success 200 {object} map[string]interface{} "符合性声明 | Conformance declaration"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "public"
// @Router /ogc/tiles/{serviceName}/conformance [get]
func (h *OGCTilesHandler) GetConformance(c *gin.Context) {
	if _, ok := h.getAuthorizedService(c, c.Param("serviceName")); !ok {
		return
	}

	conformance := map[string]interface{}{
		"conformsTo": []string{
			"http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/core",
			"http://www.opengis.net/spec/ogcapi-common-2/1.0/conf/collections",
			"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/core",
			"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tileset",
			"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tilesets-list",
		},
	}

	c.JSON(http.StatusOK, conformance)

	logger.L().Debug("OGC Tiles Conformance 请求成功",
		"service_name", c.Param("serviceName"))
}

// ============================================================================
// Tile Matrix Sets
// ============================================================================

// GetTileMatrixSets 返回可用的瓦片矩阵集列表
// @Summary 获取 Tile Matrix Sets | Get Tile Matrix Sets
// @Tags OGC Tiles
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Success 200 {object} map[string]interface{} "瓦片矩阵集列表 | Tile Matrix Set list"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "public"
// @Router /ogc/tiles/{serviceName}/tileMatrixSets [get]
func (h *OGCTilesHandler) GetTileMatrixSets(c *gin.Context) {
	host := c.Request.Host
	serviceName := c.Param("serviceName")
	if _, ok := h.getAuthorizedService(c, serviceName); !ok {
		return
	}

	tileMatrixSets := map[string]interface{}{
		"tileMatrixSets": []map[string]interface{}{
			{
				"id":    "WebMercatorQuad",
				"title": "Web Mercator",
				"uri":   "http://www.opengis.net/def/tilematrixset/OGC/1.0/WebMercatorQuad",
				"crs":   "http://www.opengis.net/def/crs/EPSG/0/3857",
				"links": []map[string]string{
					{
						"href": fmt.Sprintf("http://%s/ogc/tiles/%s/tileMatrixSets/WebMercatorQuad",
							host, serviceName),
						"rel":  "http://www.opengis.net/def/rel/ogc/1.0/tiling-scheme",
						"type": "application/json",
					},
				},
			},
		},
	}

	c.JSON(http.StatusOK, tileMatrixSets)

	logger.L().Debug("OGC Tiles TileMatrixSets 请求成功",
		"service_name", serviceName)
}

// GetTileMatrixSet 返回单个瓦片矩阵集详情
// @Summary 获取单个 Tile Matrix Set | Get single Tile Matrix Set
// @Tags OGC Tiles
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Param tileMatrixSetId path string true "瓦片矩阵集 ID | Tile Matrix Set ID"
// @Success 200 {object} map[string]interface{} "瓦片矩阵集详情 | Tile Matrix Set details"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "public"
// @Router /ogc/tiles/{serviceName}/tileMatrixSets/{tileMatrixSetId} [get]
func (h *OGCTilesHandler) GetTileMatrixSet(c *gin.Context) {
	if _, ok := h.getAuthorizedService(c, c.Param("serviceName")); !ok {
		return
	}

	tileMatrixSetId := c.Param("tileMatrixSetId")

	if tileMatrixSetId != "WebMercatorQuad" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tile Matrix Set not found"})
		return
	}

	// 构建 Web Mercator Quad 定义
	tileMatrices := make([]map[string]interface{}, 23)
	for z := 0; z <= 22; z++ {
		// 计算 2^z 用于scale和cell size计算
		shift := 1 << uint(z)
		tileMatrices[z] = map[string]interface{}{
			"id":               fmt.Sprintf("%d", z),
			"scaleDenominator": 559082264.029 / float64(shift),
			"cellSize":         156543.033928041 / float64(shift),
			"cornerOfOrigin":   "topLeft",
			"pointOfOrigin":    []float64{-20037508.3427892, 20037508.3427892},
			"tileWidth":        256,
			"tileHeight":       256,
			"matrixWidth":      shift,
			"matrixHeight":     shift,
		}
	}

	tileMatrixSet := map[string]interface{}{
		"id":           "WebMercatorQuad",
		"title":        "Web Mercator",
		"uri":          "http://www.opengis.net/def/tilematrixset/OGC/1.0/WebMercatorQuad",
		"crs":          "http://www.opengis.net/def/crs/EPSG/0/3857",
		"tileMatrices": tileMatrices,
	}

	c.JSON(http.StatusOK, tileMatrixSet)

	logger.L().Debug("OGC Tiles TileMatrixSet 详情请求成功",
		"tile_matrix_set_id", tileMatrixSetId)
}

// ============================================================================
// Tilesets
// ============================================================================

// GetTilesets 返回瓦片集列表
// @Summary 获取 Tilesets | Get Tilesets
// @Tags OGC Tiles
// @Produce json
// @Param serviceName path string true "服务名称 | Service name"
// @Success 200 {object} map[string]interface{} "瓦片集列表 | Tileset list"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "public"
// @Router /ogc/tiles/{serviceName}/tiles [get]
func (h *OGCTilesHandler) GetTilesets(c *gin.Context) {
	serviceName := c.Param("serviceName")
	host := c.Request.Host

	tileService, ok := h.getAuthorizedService(c, serviceName)
	if !ok {
		return
	}

	// 构建瓦片集列表
	tilesets := make([]map[string]interface{}, len(tileService.Layers))
	for i, layer := range tileService.Layers {
		tilesets[i] = map[string]interface{}{
			"title":       layer.Title,
			"description": layer.Description,
			"dataType":    "vector",
			"tileMatrixSetLinks": []map[string]string{
				{
					"tileMatrixSet":    "WebMercatorQuad",
					"tileMatrixSetURI": "http://www.opengis.net/def/tilematrixset/OGC/1.0/WebMercatorQuad",
				},
			},
			"links": []map[string]string{
				{
					"href": fmt.Sprintf("http://%s/ogc/tiles/%s/tiles/%s/WebMercatorQuad/{tileMatrix}/{tileRow}/{tileCol}",
						host, serviceName, layer.LayerName),
					"rel":       "item",
					"type":      "application/vnd.mapbox-vector-tile",
					"templated": "true",
				},
			},
		}
	}

	response := map[string]interface{}{
		"tilesets": tilesets,
	}

	c.JSON(http.StatusOK, response)

	logger.L().Info("OGC Tiles Tilesets 请求成功",
		"service_name", serviceName,
		"layers_count", len(tileService.Layers))
}

// ============================================================================
// Get Tile
// ============================================================================

// GetTile 返回单个瓦片（复用 XYZ Tiles 逻辑）
// @Summary 获取单个瓦片 | Get single tile
// @Tags OGC Tiles
// @Produce application/vnd.mapbox-vector-tile
// @Param serviceName path string true "服务名称 | Service name"
// @Param layer path string true "图层名称 | Layer name"
// @Param tileMatrixSetId path string true "瓦片矩阵集 ID | Tile Matrix Set ID"
// @Param tileMatrix path string true "瓦片矩阵（缩放级别）| Tile matrix (zoom level)"
// @Param tileRow path string true "瓦片行 | Tile row"
// @Param tileCol path string true "瓦片列 | Tile column"
// @Success 200 {file} application/vnd.mapbox-vector-tile "瓦片数据 | Tile data"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "public"
// @Router /ogc/tiles/{serviceName}/tiles/{layer}/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol} [get]
func (h *OGCTilesHandler) GetTile(c *gin.Context) {
	serviceName := c.Param("serviceName")
	layerName := c.Param("layer")
	tileMatrixSetId := c.Param("tileMatrixSetId")
	tileMatrixStr := c.Param("tileMatrix")
	tileRowStr := c.Param("tileRow")
	tileColStr := c.Param("tileCol")

	// 验证瓦片矩阵集
	if tileMatrixSetId != "WebMercatorQuad" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported Tile Matrix Set"})
		return
	}

	// 解析参数（OGC Tiles API: tileMatrix=z, tileRow=y, tileCol=x）
	z, err := strconv.Atoi(tileMatrixStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tile matrix"})
		return
	}

	y, err := strconv.Atoi(tileRowStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tile row"})
		return
	}

	x, err := strconv.Atoi(tileColStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tile col"})
		return
	}

	// 设置参数到上下文，复用 XYZ Tiles 逻辑
	c.Params = append(c.Params[:0],
		gin.Param{Key: "serviceName", Value: serviceName},
		gin.Param{Key: "layerName", Value: layerName},
		gin.Param{Key: "z", Value: tileMatrixStr},
		gin.Param{Key: "x", Value: tileColStr},
		gin.Param{Key: "yformat", Value: fmt.Sprintf("/%d.mvt", y)},
	)

	// 复用 XYZ Tiles Handler
	h.tileEndpointHandler.GetXYZTile(c)

	logger.L().Debug("OGC Tiles GetTile 请求转发到 XYZ Tiles",
		"service_name", serviceName,
		"layer_name", layerName,
		"z", z, "x", x, "y", y)
}

func (h *OGCTilesHandler) getAuthorizedService(c *gin.Context, serviceName string) (*models.TileService, bool) {
	tileService, err := h.tileServiceService.GetServiceModelByName(serviceName)
	if err != nil {
		logger.L().Error("OGC Tiles 服务未找到",
			"service_name", serviceName,
			"error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return nil, false
	}
	if !requireJSONServiceAccess(c, tileService.PublicAccess, tileService.TenantID) {
		return nil, false
	}
	return tileService, true
}
