package api

import (
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/addp/common/logger"
	"github.com/addp/service/internal/models"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// WMTS Handler - OGC WMTS 1.0.0 标准实现
// ============================================================================

// WMTSHandler 处理 WMTS 请求
type WMTSHandler struct {
	tileServiceService tileServiceLookup
}

// NewWMTSHandler 创建 WMTS 处理器
func NewWMTSHandler(tileServiceService tileServiceLookup) *WMTSHandler {
	return &WMTSHandler{
		tileServiceService: tileServiceService,
	}
}

// GetCapabilities 返回 WMTS 1.0.0 Capabilities XML
// @Summary 获取 WMTS Capabilities | Get WMTS Capabilities
// @Tags WMTS
// @Produce xml
// @Param serviceName path string true "服务名称 | Service name"
// @Param request query string false "请求类型 (GetCapabilities) | Request type (GetCapabilities)"
// @Success 200 {object} Capabilities "WMTS Capabilities XML | WMTS Capabilities XML"
// @Failure 401 {object} ExceptionReport "需要认证 | Authentication required"
// @Failure 403 {object} ExceptionReport "无权访问 | Access denied"
// @Router /wmts/{serviceName} [get]
func (h *WMTSHandler) GetCapabilities(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 查询服务
	tileService, err := h.tileServiceService.GetServiceModelByName(serviceName)
	if err != nil {
		logger.L().Error("WMTS 服务未找到",
			"service_name", serviceName,
			"error", err)
		c.XML(http.StatusNotFound, buildErrorXML("ServiceNotFound", "Service not found"))
		return
	}

	// 检查访问权限
	if status := serviceAccessStatus(c, tileService.PublicAccess, tileService.TenantID); status != 0 {
		if status == http.StatusUnauthorized {
			c.XML(status, buildErrorXML("Unauthorized", "Authentication required"))
		} else {
			c.XML(status, buildErrorXML("Forbidden", "Access denied"))
		}
		return
	}

	// 构建 Capabilities XML
	capabilities := buildWMTSCapabilities(tileService, c.Request.Host)

	c.Header("Content-Type", "application/xml")
	c.XML(http.StatusOK, capabilities)

	logger.L().Info("WMTS GetCapabilities 请求成功",
		"service_name", serviceName,
		"layers_count", len(tileService.Layers))
}

// ============================================================================
// WMTS 数据结构（OGC WMTS 1.0.0）
// ============================================================================

// Capabilities WMTS Capabilities 根元素
type Capabilities struct {
	XMLName xml.Name `xml:"Capabilities"`
	Version string   `xml:"version,attr"`
	XMLNS   string   `xml:"xmlns,attr"`
	OWS     string   `xml:"xmlns:ows,attr"`
	XLink   string   `xml:"xmlns:xlink,attr"`

	ServiceIdentification ServiceIdentification `xml:"ows:ServiceIdentification"`
	ServiceProvider       ServiceProvider       `xml:"ows:ServiceProvider"`
	OperationsMetadata    OperationsMetadata    `xml:"ows:OperationsMetadata"`
	Contents              Contents              `xml:"Contents"`
}

// ServiceIdentification 服务标识
type ServiceIdentification struct {
	Title       string   `xml:"ows:Title"`
	Abstract    string   `xml:"ows:Abstract,omitempty"`
	Keywords    []string `xml:"ows:Keywords>ows:Keyword,omitempty"`
	ServiceType string   `xml:"ows:ServiceType"`
}

// ServiceProvider 服务提供者
type ServiceProvider struct {
	ProviderName string `xml:"ows:ProviderName"`
}

// OperationsMetadata 操作元数据
type OperationsMetadata struct {
	Operations []Operation `xml:"ows:Operation"`
}

// Operation 操作定义
type Operation struct {
	Name string `xml:"name,attr"`
	DCP  DCP    `xml:"ows:DCP"`
}

// DCP 分布式计算平台
type DCP struct {
	HTTP HTTP `xml:"ows:HTTP"`
}

// HTTP HTTP 访问
type HTTP struct {
	Get Get `xml:"ows:Get"`
}

// Get GET 方法
type Get struct {
	Href string `xml:"xlink:href,attr"`
}

// Contents 内容定义
type Contents struct {
	Layers         []Layer         `xml:"Layer"`
	TileMatrixSets []TileMatrixSet `xml:"TileMatrixSet"`
}

// Layer 图层定义
type Layer struct {
	Title             string            `xml:"ows:Title"`
	Abstract          string            `xml:"ows:Abstract,omitempty"`
	Identifier        string            `xml:"ows:Identifier"`
	Style             Style             `xml:"Style"`
	Format            string            `xml:"Format"`
	TileMatrixSetLink TileMatrixSetLink `xml:"TileMatrixSetLink"`
	ResourceURL       ResourceURL       `xml:"ResourceURL"`
	WGS84BoundingBox  *WGS84BoundingBox `xml:"ows:WGS84BoundingBox,omitempty"`
}

// Style 样式定义
type Style struct {
	IsDefault  bool   `xml:"isDefault,attr"`
	Identifier string `xml:"ows:Identifier"`
}

// TileMatrixSetLink 瓦片矩阵集链接
type TileMatrixSetLink struct {
	TileMatrixSet string `xml:"TileMatrixSet"`
}

// ResourceURL 资源 URL 模板
type ResourceURL struct {
	Format       string `xml:"format,attr"`
	ResourceType string `xml:"resourceType,attr"`
	Template     string `xml:"template,attr"`
}

// WGS84BoundingBox WGS84 边界框
type WGS84BoundingBox struct {
	LowerCorner string `xml:"ows:LowerCorner"`
	UpperCorner string `xml:"ows:UpperCorner"`
}

// TileMatrixSet 瓦片矩阵集
type TileMatrixSet struct {
	Identifier   string       `xml:"ows:Identifier"`
	SupportedCRS string       `xml:"ows:SupportedCRS"`
	TileMatrices []TileMatrix `xml:"TileMatrix"`
}

// TileMatrix 瓦片矩阵
type TileMatrix struct {
	Identifier       string  `xml:"ows:Identifier"`
	ScaleDenominator float64 `xml:"ScaleDenominator"`
	TopLeftCorner    string  `xml:"TopLeftCorner"`
	TileWidth        int     `xml:"TileWidth"`
	TileHeight       int     `xml:"TileHeight"`
	MatrixWidth      int     `xml:"MatrixWidth"`
	MatrixHeight     int     `xml:"MatrixHeight"`
}

// ExceptionReport 异常报告
type ExceptionReport struct {
	XMLName   xml.Name  `xml:"ExceptionReport"`
	Version   string    `xml:"version,attr"`
	Exception Exception `xml:"Exception"`
}

// Exception 异常
type Exception struct {
	ExceptionCode string `xml:"exceptionCode,attr"`
	ExceptionText string `xml:"ExceptionText"`
}

// ============================================================================
// 辅助函数
// ============================================================================

// buildWMTSCapabilities 构建 WMTS Capabilities
func buildWMTSCapabilities(tileService *models.TileService, host string) *Capabilities {
	return &Capabilities{
		Version: "1.0.0",
		XMLNS:   "http://www.opengis.net/wmts/1.0",
		OWS:     "http://www.opengis.net/ows/1.1",
		XLink:   "http://www.w3.org/1999/xlink",
		ServiceIdentification: ServiceIdentification{
			Title:       tileService.Title,
			Abstract:    tileService.Description,
			ServiceType: "OGC WMTS",
		},
		ServiceProvider: ServiceProvider{
			ProviderName: "ADDP Platform",
		},
		OperationsMetadata: OperationsMetadata{
			Operations: []Operation{
				{
					Name: "GetCapabilities",
					DCP: DCP{
						HTTP: HTTP{
							Get: Get{
								Href: fmt.Sprintf("http://%s/wmts/%s", host, tileService.ServiceName),
							},
						},
					},
				},
				{
					Name: "GetTile",
					DCP: DCP{
						HTTP: HTTP{
							Get: Get{
								Href: fmt.Sprintf("http://%s/tiles/%s/", host, tileService.ServiceName),
							},
						},
					},
				},
			},
		},
		Contents: Contents{
			Layers:         buildWMTSLayers(tileService, host),
			TileMatrixSets: []TileMatrixSet{buildWebMercatorQuad()},
		},
	}
}

// buildWMTSLayers 构建 WMTS 图层列表
func buildWMTSLayers(tileService *models.TileService, host string) []Layer {
	layers := make([]Layer, len(tileService.Layers))

	for i, layer := range tileService.Layers {
		layers[i] = Layer{
			Title:      layer.Title,
			Abstract:   layer.Description,
			Identifier: layer.LayerName,
			Style: Style{
				IsDefault:  true,
				Identifier: "default",
			},
			Format: "application/vnd.mapbox-vector-tile",
			TileMatrixSetLink: TileMatrixSetLink{
				TileMatrixSet: "WebMercatorQuad",
			},
			ResourceURL: ResourceURL{
				Format:       "application/vnd.mapbox-vector-tile",
				ResourceType: "tile",
				Template: fmt.Sprintf("http://%s/tiles/%s/%s/{TileMatrix}/{TileCol}/{TileRow}.mvt",
					host, tileService.ServiceName, layer.LayerName),
			},
			WGS84BoundingBox: &WGS84BoundingBox{
				LowerCorner: "-180 -90",
				UpperCorner: "180 90",
			},
		}
	}

	return layers
}

// buildWebMercatorQuad 构建 WebMercatorQuad 瓦片矩阵集
func buildWebMercatorQuad() TileMatrixSet {
	matrices := make([]TileMatrix, 23) // 缩放级别 0-22

	for z := 0; z <= 22; z++ {
		// 计算 2^z 用于 scale denominator 计算
		shift := 1 << uint(z)
		matrices[z] = TileMatrix{
			Identifier:       fmt.Sprintf("%d", z),
			ScaleDenominator: 559082264.029 / float64(shift),
			TopLeftCorner:    "-20037508.3427892 20037508.3427892",
			TileWidth:        256,
			TileHeight:       256,
			MatrixWidth:      shift,
			MatrixHeight:     shift,
		}
	}

	return TileMatrixSet{
		Identifier:   "WebMercatorQuad",
		SupportedCRS: "urn:ogc:def:crs:EPSG::3857",
		TileMatrices: matrices,
	}
}

// buildErrorXML 构建错误 XML
func buildErrorXML(code, message string) *ExceptionReport {
	return &ExceptionReport{
		Version: "1.0.0",
		Exception: Exception{
			ExceptionCode: code,
			ExceptionText: message,
		},
	}
}
