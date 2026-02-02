package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/ogc/common"
	"github.com/addp/service/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// WMTSHandler WMTS 服务处理器
type WMTSHandler struct {
	repo *repository.InternalServiceRepository
	db   *gorm.DB
}

// NewWMTSHandler 创建新的 WMTS 处理器
func NewWMTSHandler(repo *repository.InternalServiceRepository, db *gorm.DB) *WMTSHandler {
	return &WMTSHandler{
		repo: repo,
		db:   db,
	}
}

// GetCapabilities 处理 GetCapabilities 请求
// GET /ogc/wmts/:serviceName?service=WMTS&request=GetCapabilities
func (h *WMTSHandler) GetCapabilities(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 查询服务
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// 验证是否启用 WMTS
	if !service.EnabledWMTS {
		c.JSON(http.StatusForbidden, gin.H{"error": "WMTS is not enabled for this service"})
		return
	}

	baseURL := "http://" + c.Request.Host + "/ogc/wmts/" + serviceName

	// TODO: 生成完整的 WMTS Capabilities XML
	// 这里返回简化的响应
	capsXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Capabilities xmlns="http://www.opengis.net/wmts/1.0.0" xmlns:ows="http://www.opengis.net/ows/1.1" version="1.0.0">
  <ows:ServiceIdentification>
    <ows:Title>%s</ows:Title>
    <ows:Abstract>%s</ows:Abstract>
    <ows:ServiceType>WMTS</ows:ServiceType>
    <ows:ServiceTypeVersion>1.0.0</ows:ServiceTypeVersion>
  </ows:ServiceIdentification>
  <ows:OperationsMetadata>
    <ows:Operation name="GetCapabilities">
      <ows:DCP>
        <ows:HTTP>
          <ows:Get xlink:href="%s?service=WMTS&amp;request=GetCapabilities"/>
        </ows:HTTP>
      </ows:DCP>
    </ows:Operation>
    <ows:Operation name="GetTile">
      <ows:DCP>
        <ows:HTTP>
          <ows:Get xlink:href="%s/tile/{{layer}}/{{z}}/{{x}}/{{y}}.mvt"/>
        </ows:HTTP>
      </ows:DCP>
    </ows:Operation>
  </ows:OperationsMetadata>
  <Contents>
    <Layer>
      <ows:Title>Layers</ows:Title>
      <!-- Layer definitions will be added here -->
    </Layer>
    <TileMatrixSet>
      <ows:Identifier>WebMercatorQuad</ows:Identifier>
      <!-- Tile matrix definitions will be added here -->
    </TileMatrixSet>
  </Contents>
</Capabilities>`, service.Title, service.Abstract, baseURL, baseURL)

	c.Header("Content-Type", "application/xml")
	c.String(http.StatusOK, capsXML)

	logger.L().Debug("WMTS GetCapabilities requested", "service", serviceName)
}

// GetTile 处理 GetTile 请求
// GET /ogc/wmts/:serviceName/tile/:layer/:z/:x/:y.mvt
func (h *WMTSHandler) GetTile(c *gin.Context) {
	serviceName := c.Param("serviceName")
	layerName := c.Param("layer")
	zStr := c.Param("z")
	xStr := c.Param("x")
	yStr := c.Param("y")

	// 解析 z, x, y 参数
	z, err := strconv.Atoi(zStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid z parameter"})
		return
	}

	x, err := strconv.Atoi(xStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid x parameter"})
		return
	}

	y, err := strconv.Atoi(yStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid y parameter"})
		return
	}

	// 验证缩放级别范围（0-22）
	if z < 0 || z > 22 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Zoom level must be between 0 and 22"})
		return
	}

	// 查询服务和图层
	service, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	if !service.EnabledWMTS {
		c.JSON(http.StatusForbidden, gin.H{"error": "WMTS is not enabled for this service"})
		return
	}

	// 查询图层
	publishedLayer, err := h.repo.GetLayerByName(service.ID, layerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Layer not found"})
		return
	}

	if !publishedLayer.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Layer is disabled"})
		return
	}

	// 检查图层是否有几何列（非空间数据不支持 WMTS）
	if publishedLayer.GeometryColumn == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Layer does not contain spatial data",
			"hint":  "WMTS requires geometry column",
		})
		return
	}

	logger.L().Debug("WMTS GetTile requested",
		"service", serviceName,
		"layer", layerName,
		"z", z, "x", x, "y", y)

	// 生成 MVT 瓦片
	mvtData, err := h.generateMVTTile(service, publishedLayer, z, x, y)
	if err != nil {
		logger.L().Error("Failed to generate MVT tile",
			"error", err,
			"service", serviceName,
			"layer", layerName,
			"z", z, "x", x, "y", y)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate tile"})
		return
	}

	// 空瓦片（无数据）
	if mvtData == nil || len(mvtData) == 0 {
		c.Status(http.StatusNoContent) // 204 No Content
		return
	}

	// 设置缓存控制头
	cacheControl := "public, max-age=3600"
	if publishedLayer.CacheControl != "" {
		cacheControl = publishedLayer.CacheControl
	}
	c.Header("Cache-Control", cacheControl)

	// 返回 MVT 数据
	c.Data(http.StatusOK, "application/vnd.mapbox-vector-tile", mvtData)

	logger.L().Debug("WMTS GetTile returned",
		"service", serviceName,
		"layer", layerName,
		"z", z, "x", x, "y", y,
		"size", len(mvtData))
}

// generateMVTTile 生成 MVT 瓦片（内部方法）
func (h *WMTSHandler) generateMVTTile(
	service *models.InternalService,
	layer *models.InternalServiceLayer,
	z, x, y int,
) ([]byte, error) {
	// TODO: 从 service.EngineID 获取对应的数据库连接
	// 当前使用默认数据库连接
	db := h.db

	// 调用 common 模块的 MVT 生成器
	return common.GenerateMVTTile(db, layer, z, x, y)
}
