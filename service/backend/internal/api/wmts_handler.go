package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
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
          <ows:Get xlink:href="%s?service=WMTS&request=GetCapabilities"/>
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
	layer := c.Param("layer")
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
	publishedLayer, err := h.repo.GetLayerByName(service.ID, layer)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Layer not found"})
		return
	}

	if !publishedLayer.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Layer is disabled"})
		return
	}

	logger.L().Debug("WMTS GetTile requested", "service", serviceName, "layer", layer, "z", z, "x", x, "y", y)

	// TODO: 调用 Common 模块的 MVT 生成器
	// tileData, err := h.generateMVTTile(service, publishedLayer, z, x, y)

	// 这是一个占位符响应
	c.JSON(http.StatusOK, gin.H{"message": "WMTS GetTile not yet fully implemented"})
}
