package registry

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

type ExternalServiceService struct {
	repo       *repository.ExternalServiceRepository
	httpClient *http.Client
}

func NewExternalServiceService(repo *repository.ExternalServiceRepository) *ExternalServiceService {
	return &ExternalServiceService{
		repo: repo,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CreateService 创建外部服务
func (s *ExternalServiceService) CreateService(ctx context.Context, req *models.CreateExternalServiceRequest, tenantID, userID uint) (*models.ExternalService, error) {
	// 创建服务实体
	service := &models.ExternalService{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		ServiceType: req.ServiceType,
		URL:         strings.TrimRight(req.URL, "/"), // 去除尾部斜杠
		AuthType:    req.AuthType,
		AuthConfig:  req.AuthConfig,
		Status:      "active",
		CreatedBy:   userID,
	}

	if req.HealthCheckURL != "" {
		service.HealthCheck = models.HealthCheckURL(req.HealthCheckURL)
	}

	// 如果是 OGC 服务，自动获取元数据
	if isOGCService(req.ServiceType) {
		metadata, layers, err := s.fetchOGCMetadata(ctx, service)
		if err != nil {
			// 元数据获取失败不阻止创建，记录错误状态
			service.Status = "error"
			service.Metadata = models.JSONB{"error": err.Error()}
		} else {
			service.Metadata = metadata
			service.Layers = layers
		}
	}

	// 保存到数据库
	if err := s.repo.Create(service); err != nil {
		return nil, fmt.Errorf("failed to create external service: %w", err)
	}

	return service, nil
}

// GetService 获取外部服务详情
func (s *ExternalServiceService) GetService(ctx context.Context, serviceID, tenantID uint) (*models.ExternalService, error) {
	service, err := s.repo.GetByTenantAndID(tenantID, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}
	return service, nil
}

// ListServices 列出外部服务
func (s *ExternalServiceService) ListServices(ctx context.Context, tenantID uint, serviceType string, page, pageSize int) ([]models.ExternalService, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	return s.repo.List(tenantID, serviceType, page, pageSize)
}

// UpdateService 更新外部服务
func (s *ExternalServiceService) UpdateService(ctx context.Context, serviceID, tenantID uint, req *models.UpdateExternalServiceRequest) (*models.ExternalService, error) {
	service, err := s.repo.GetByTenantAndID(tenantID, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	// 更新字段
	if req.Name != nil {
		service.Name = *req.Name
	}
	if req.Description != nil {
		service.Description = *req.Description
	}
	if req.URL != nil {
		service.URL = strings.TrimRight(*req.URL, "/")
	}
	if req.AuthType != nil {
		service.AuthType = *req.AuthType
	}
	if req.AuthConfig != nil {
		service.AuthConfig = *req.AuthConfig
	}
	if req.Status != nil {
		service.Status = *req.Status
	}
	if req.HealthCheckURL != nil {
		service.HealthCheck = models.HealthCheckURL(*req.HealthCheckURL)
	}

	if err := s.repo.Update(service); err != nil {
		return nil, fmt.Errorf("failed to update service: %w", err)
	}

	return service, nil
}

// DeleteService 删除外部服务
func (s *ExternalServiceService) DeleteService(ctx context.Context, serviceID, tenantID uint) error {
	// 验证服务属于该租户
	if _, err := s.repo.GetByTenantAndID(tenantID, serviceID); err != nil {
		return fmt.Errorf("service not found: %w", err)
	}

	return s.repo.Delete(serviceID)
}

// RefreshMetadata 刷新服务元数据
func (s *ExternalServiceService) RefreshMetadata(ctx context.Context, serviceID, tenantID uint) (*models.ExternalService, error) {
	service, err := s.repo.GetByTenantAndID(tenantID, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	if !isOGCService(service.ServiceType) {
		return nil, fmt.Errorf("metadata refresh only supported for OGC services")
	}

	// 获取最新元数据
	metadata, layers, err := s.fetchOGCMetadata(ctx, service)
	if err != nil {
		// 更新错误状态
		_ = s.repo.UpdateHealthCheck(serviceID, "error")
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	// 更新元数据
	if err := s.repo.UpdateMetadata(serviceID, metadata); err != nil {
		return nil, fmt.Errorf("failed to update metadata: %w", err)
	}

	// 更新图层（删除旧的，创建新的）
	if err := s.repo.DeleteLayersByServiceID(serviceID); err != nil {
		return nil, fmt.Errorf("failed to delete old layers: %w", err)
	}

	if len(layers) > 0 {
		for i := range layers {
			layers[i].ServiceID = serviceID
		}
		if err := s.repo.BulkCreateLayers(layers); err != nil {
			return nil, fmt.Errorf("failed to create layers: %w", err)
		}
	}

	// 更新健康状态
	_ = s.repo.UpdateHealthCheck(serviceID, "active")

	// 返回更新后的服务
	return s.repo.GetByID(serviceID)
}

// ProxyRequest 代理请求到外部服务
func (s *ExternalServiceService) ProxyRequest(ctx context.Context, service *models.ExternalService, path string, query string) ([]byte, string, int, error) {
	// 构建目标 URL
	targetURL := fmt.Sprintf("%s%s", service.URL, path)
	if query != "" {
		targetURL = fmt.Sprintf("%s?%s", targetURL, query)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加认证头
	if err := s.addAuthHeaders(req, service); err != nil {
		return nil, "", 0, fmt.Errorf("failed to add auth headers: %w", err)
	}

	// 发送请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	return body, contentType, resp.StatusCode, nil
}

// fetchOGCMetadata 获取 OGC 服务元数据
func (s *ExternalServiceService) fetchOGCMetadata(ctx context.Context, service *models.ExternalService) (models.JSONB, []models.ExternalServiceLayer, error) {
	var capabilitiesURL string

	switch service.ServiceType {
	case "wms":
		capabilitiesURL = fmt.Sprintf("%s?service=WMS&request=GetCapabilities&version=1.3.0", service.URL)
	case "wfs":
		capabilitiesURL = fmt.Sprintf("%s?service=WFS&request=GetCapabilities&version=2.0.0", service.URL)
	case "wmts":
		capabilitiesURL = fmt.Sprintf("%s?service=WMTS&request=GetCapabilities&version=1.0.0", service.URL)
	case "ogc_api":
		capabilitiesURL = fmt.Sprintf("%s/", service.URL)
	default:
		return nil, nil, fmt.Errorf("unsupported service type: %s", service.ServiceType)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", capabilitiesURL, nil)
	if err != nil {
		return nil, nil, err
	}

	// 添加认证
	if err := s.addAuthHeaders(req, service); err != nil {
		return nil, nil, err
	}

	// 发送请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("capabilities request failed with status: %d", resp.StatusCode)
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	// 根据服务类型解析元数据
	var metadata models.JSONB
	var layers []models.ExternalServiceLayer

	switch service.ServiceType {
	case "wms":
		metadata, layers, err = s.parseWMSCapabilities(body, service.ID)
	case "wfs":
		metadata, layers, err = s.parseWFSCapabilities(body, service.ID)
	case "wmts":
		metadata, layers, err = s.parseWMTSCapabilities(body, service.ID)
	case "ogc_api":
		metadata, layers, err = s.parseOGCAPICollections(body, service.ID)
	default:
		// 默认行为：保存原始响应
		metadata = models.JSONB{
			"capabilities_url": capabilitiesURL,
			"raw_response":     string(body[:min(1000, len(body))]),
			"fetched_at":       time.Now().Format(time.RFC3339),
		}
		layers = []models.ExternalServiceLayer{}
	}

	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse capabilities: %w", err)
	}

	return metadata, layers, nil
}

// parseWMSCapabilities 解析 WMS GetCapabilities XML 响应
func (s *ExternalServiceService) parseWMSCapabilities(body []byte, serviceID uint) (models.JSONB, []models.ExternalServiceLayer, error) {
	var capabilities WMSCapabilities
	if err := xml.Unmarshal(body, &capabilities); err != nil {
		return nil, nil, fmt.Errorf("failed to parse WMS XML: %w", err)
	}

	// 构建元数据
	metadata := models.JSONB{
		"service_title":       capabilities.Service.Title,
		"service_abstract":    capabilities.Service.Abstract,
		"version":             capabilities.Version,
		"online_resource":     capabilities.Service.OnlineResource.Href,
		"contact_organization": capabilities.Service.ContactInformation.Organization,
		"fetched_at":          time.Now().Format(time.RFC3339),
	}

	// 提取图层列表（递归处理嵌套图层）
	var layers []models.ExternalServiceLayer
	extractWMSLayers(&layers, capabilities.Capability.Layer, serviceID, "")

	return metadata, layers, nil
}

// extractWMSLayers 递归提取 WMS 图层
func extractWMSLayers(layers *[]models.ExternalServiceLayer, layer WMSLayer, serviceID uint, parentName string) {
	layerName := layer.Name
	if layerName == "" {
		// 如果是容器图层（没有 Name），使用 Title
		layerName = layer.Title
	}

	// 构建完整图层名称（考虑层级关系）
	fullName := layerName
	if parentName != "" {
		fullName = parentName + "/" + layerName
	}

	// 只有具有 Name 的图层才可以被请求
	if layer.Name != "" {
		// 提取 BBox（使用 EX_GeographicBoundingBox 或第一个 BoundingBox）
		var bbox models.JSONB
		if layer.EXGeographicBoundingBox != nil {
			bbox = models.JSONB{
				"west":  layer.EXGeographicBoundingBox.WestLongitude,
				"east":  layer.EXGeographicBoundingBox.EastLongitude,
				"south": layer.EXGeographicBoundingBox.SouthLatitude,
				"north": layer.EXGeographicBoundingBox.NorthLatitude,
			}
		} else if len(layer.BoundingBox) > 0 {
			bb := layer.BoundingBox[0]
			bbox = models.JSONB{
				"minx": bb.Minx,
				"miny": bb.Miny,
				"maxx": bb.Maxx,
				"maxy": bb.Maxy,
				"crs":  bb.CRS,
			}
		}

		// 提取 CRS 列表
		var crs string
		if len(layer.CRS) > 0 {
			crs = layer.CRS[0] // 默认使用第一个 CRS
		}

		// 提取样式信息
		stylesJSON := models.JSONB{}
		if len(layer.Style) > 0 {
			var styles []map[string]string
			for _, style := range layer.Style {
				styles = append(styles, map[string]string{
					"name":  style.Name,
					"title": style.Title,
				})
			}
			stylesJSON["styles"] = styles
		}

		*layers = append(*layers, models.ExternalServiceLayer{
			ServiceID:   serviceID,
			LayerName:   layer.Name,
			DisplayName: layer.Title,
			GeometryType: "raster", // WMS 图层通常是栅格
			CRS:         crs,
			BBox:        bbox,
			Metadata: models.JSONB{
				"abstract":    layer.Abstract,
				"queryable":   layer.Queryable,
				"opaque":      layer.Opaque,
				"styles":      stylesJSON,
				"min_scale":   layer.MinScaleDenominator,
				"max_scale":   layer.MaxScaleDenominator,
			},
			Enabled: true,
		})
	}

	// 递归处理子图层
	for _, subLayer := range layer.Layers {
		extractWMSLayers(layers, subLayer, serviceID, fullName)
	}
}

// parseWFSCapabilities 解析 WFS GetCapabilities XML 响应
func (s *ExternalServiceService) parseWFSCapabilities(body []byte, serviceID uint) (models.JSONB, []models.ExternalServiceLayer, error) {
	var capabilities WFSCapabilities
	if err := xml.Unmarshal(body, &capabilities); err != nil {
		return nil, nil, fmt.Errorf("failed to parse WFS XML: %w", err)
	}

	// 构建元数据
	metadata := models.JSONB{
		"service_title":    capabilities.ServiceIdentification.Title,
		"service_abstract": capabilities.ServiceIdentification.Abstract,
		"version":          capabilities.Version,
		"provider_name":    capabilities.ServiceProvider.ProviderName,
		"fetched_at":       time.Now().Format(time.RFC3339),
	}

	// 提取要素类型列表
	var layers []models.ExternalServiceLayer
	for _, ft := range capabilities.FeatureTypeList.FeatureTypes {
		// 提取 BBox
		var bbox models.JSONB
		if ft.WGS84BoundingBox != nil {
			// 解析坐标字符串 "minx miny" 和 "maxx maxy"
			lowerCorner := strings.Fields(ft.WGS84BoundingBox.LowerCorner)
			upperCorner := strings.Fields(ft.WGS84BoundingBox.UpperCorner)
			if len(lowerCorner) == 2 && len(upperCorner) == 2 {
				bbox = models.JSONB{
					"west":  lowerCorner[0],
					"south": lowerCorner[1],
					"east":  upperCorner[0],
					"north": upperCorner[1],
				}
			}
		}

		// 提取默认 CRS
		var crs string
		if ft.DefaultCRS != "" {
			crs = ft.DefaultCRS
		} else if len(ft.OtherCRS) > 0 {
			crs = ft.OtherCRS[0]
		}

		// 提取输出格式
		outputFormats := ft.OutputFormats
		if len(outputFormats) == 0 {
			outputFormats = []string{"application/gml+xml"} // WFS 默认格式
		}

		layers = append(layers, models.ExternalServiceLayer{
			ServiceID:    serviceID,
			LayerName:    ft.Name,
			DisplayName:  ft.Title,
			GeometryType: "vector", // WFS 图层是矢量数据
			CRS:          crs,
			BBox:         bbox,
			Metadata: models.JSONB{
				"abstract":       ft.Abstract,
				"keywords":       ft.Keywords,
				"output_formats": outputFormats,
				"other_crs":      ft.OtherCRS,
			},
			Enabled: true,
		})
	}

	return metadata, layers, nil
}

// parseWMTSCapabilities 解析 WMTS GetCapabilities XML 响应
func (s *ExternalServiceService) parseWMTSCapabilities(body []byte, serviceID uint) (models.JSONB, []models.ExternalServiceLayer, error) {
	var capabilities WMTSCapabilities
	if err := xml.Unmarshal(body, &capabilities); err != nil {
		return nil, nil, fmt.Errorf("failed to parse WMTS XML: %w", err)
	}

	// 构建元数据
	metadata := models.JSONB{
		"service_title":    capabilities.ServiceIdentification.Title,
		"service_abstract": capabilities.ServiceIdentification.Abstract,
		"version":          capabilities.Version,
		"fetched_at":       time.Now().Format(time.RFC3339),
	}

	// 提取图层列表
	var layers []models.ExternalServiceLayer
	for _, layer := range capabilities.Contents.Layers {
		// 提取 BBox
		var bbox models.JSONB
		if layer.WGS84BoundingBox != nil {
			lowerCorner := strings.Fields(layer.WGS84BoundingBox.LowerCorner)
			upperCorner := strings.Fields(layer.WGS84BoundingBox.UpperCorner)
			if len(lowerCorner) == 2 && len(upperCorner) == 2 {
				bbox = models.JSONB{
					"west":  lowerCorner[0],
					"south": lowerCorner[1],
					"east":  upperCorner[0],
					"north": upperCorner[1],
				}
			}
		}

		// 提取样式和TileMatrixSet
		var styles []string
		for _, style := range layer.Styles {
			styles = append(styles, style.Identifier)
		}

		var tileMatrixSets []string
		for _, tms := range layer.TileMatrixSetLinks {
			tileMatrixSets = append(tileMatrixSets, tms.TileMatrixSet)
		}

		layers = append(layers, models.ExternalServiceLayer{
			ServiceID:    serviceID,
			LayerName:    layer.Identifier,
			DisplayName:  layer.Title,
			GeometryType: "tile", // WMTS 是切片图层
			CRS:          "", // WMTS 使用 TileMatrixSet，不直接用 CRS
			BBox:         bbox,
			Metadata: models.JSONB{
				"abstract":          layer.Abstract,
				"formats":           layer.Formats,
				"styles":            styles,
				"tile_matrix_sets":  tileMatrixSets,
			},
			Enabled: true,
		})
	}

	return metadata, layers, nil
}

// parseOGCAPICollections 解析 OGC API Features collections JSON 响应
func (s *ExternalServiceService) parseOGCAPICollections(body []byte, serviceID uint) (models.JSONB, []models.ExternalServiceLayer, error) {
	var collections OGCAPICollections
	if err := json.Unmarshal(body, &collections); err != nil {
		return nil, nil, fmt.Errorf("failed to parse OGC API JSON: %w", err)
	}

	// 构建元数据
	metadata := models.JSONB{
		"title":       collections.Title,
		"description": collections.Description,
		"fetched_at":  time.Now().Format(time.RFC3339),
	}

	// 提取集合列表
	var layers []models.ExternalServiceLayer
	for _, coll := range collections.Collections {
		// 提取 BBox
		var bbox models.JSONB
		if len(coll.Extent.Spatial.BBox) > 0 && len(coll.Extent.Spatial.BBox[0]) >= 4 {
			bbox = models.JSONB{
				"west":  coll.Extent.Spatial.BBox[0][0],
				"south": coll.Extent.Spatial.BBox[0][1],
				"east":  coll.Extent.Spatial.BBox[0][2],
				"north": coll.Extent.Spatial.BBox[0][3],
			}
		}

		// 提取 CRS
		var crs string
		if len(coll.CRS) > 0 {
			crs = coll.CRS[0]
		}

		// 提取 links
		var itemsLink string
		for _, link := range coll.Links {
			if link.Rel == "items" {
				itemsLink = link.Href
				break
			}
		}

		layers = append(layers, models.ExternalServiceLayer{
			ServiceID:    serviceID,
			LayerName:    coll.ID,
			DisplayName:  coll.Title,
			GeometryType: "vector", // OGC API Features 是矢量数据
			CRS:          crs,
			BBox:         bbox,
			Metadata: models.JSONB{
				"description": coll.Description,
				"items_link":  itemsLink,
				"item_type":   coll.ItemType,
			},
			Enabled: true,
		})
	}

	return metadata, layers, nil
}

// WMS Capabilities XML 结构定义（支持 WMS 1.3.0）
type WMSCapabilities struct {
	XMLName    xml.Name      `xml:"WMS_Capabilities"`
	Version    string        `xml:"version,attr"`
	Service    WMSService    `xml:"Service"`
	Capability WMSCapability `xml:"Capability"`
}

type WMSService struct {
	Name               string                    `xml:"Name"`
	Title              string                    `xml:"Title"`
	Abstract           string                    `xml:"Abstract"`
	OnlineResource     WMSOnlineResource         `xml:"OnlineResource"`
	ContactInformation WMSContactInformation     `xml:"ContactInformation"`
}

type WMSOnlineResource struct {
	Href string `xml:"href,attr"`
}

type WMSContactInformation struct {
	Organization string `xml:"ContactPersonPrimary>ContactOrganization"`
}

type WMSCapability struct {
	Layer WMSLayer `xml:"Layer"`
}

type WMSLayer struct {
	Name                   string                  `xml:"Name"`
	Title                  string                  `xml:"Title"`
	Abstract               string                  `xml:"Abstract"`
	CRS                    []string                `xml:"CRS"`
	EXGeographicBoundingBox *WMSGeographicBBox     `xml:"EX_GeographicBoundingBox"`
	BoundingBox            []WMSBoundingBox        `xml:"BoundingBox"`
	Style                  []WMSStyle              `xml:"Style"`
	Queryable              int                     `xml:"queryable,attr"`
	Opaque                 int                     `xml:"opaque,attr"`
	MinScaleDenominator    float64                 `xml:"MinScaleDenominator"`
	MaxScaleDenominator    float64                 `xml:"MaxScaleDenominator"`
	Layers                 []WMSLayer              `xml:"Layer"` // 嵌套子图层
}

type WMSGeographicBBox struct {
	WestLongitude float64 `xml:"westBoundLongitude"`
	EastLongitude float64 `xml:"eastBoundLongitude"`
	SouthLatitude float64 `xml:"southBoundLatitude"`
	NorthLatitude float64 `xml:"northBoundLatitude"`
}

type WMSBoundingBox struct {
	CRS  string  `xml:"CRS,attr"`
	Minx float64 `xml:"minx,attr"`
	Miny float64 `xml:"miny,attr"`
	Maxx float64 `xml:"maxx,attr"`
	Maxy float64 `xml:"maxy,attr"`
}

type WMSStyle struct {
	Name  string `xml:"Name"`
	Title string `xml:"Title"`
}

// WFS Capabilities XML 结构定义（支持 WFS 2.0.0）
type WFSCapabilities struct {
	XMLName               xml.Name                  `xml:"WFS_Capabilities"`
	Version               string                    `xml:"version,attr"`
	ServiceIdentification WFSServiceIdentification  `xml:"ServiceIdentification"`
	ServiceProvider       WFSServiceProvider        `xml:"ServiceProvider"`
	FeatureTypeList       WFSFeatureTypeList        `xml:"FeatureTypeList"`
}

type WFSServiceIdentification struct {
	Title    string `xml:"Title"`
	Abstract string `xml:"Abstract"`
}

type WFSServiceProvider struct {
	ProviderName string `xml:"ProviderName"`
}

type WFSFeatureTypeList struct {
	FeatureTypes []WFSFeatureType `xml:"FeatureType"`
}

type WFSFeatureType struct {
	Name              string               `xml:"Name"`
	Title             string               `xml:"Title"`
	Abstract          string               `xml:"Abstract"`
	Keywords          []string             `xml:"Keywords>Keyword"`
	DefaultCRS        string               `xml:"DefaultCRS"`
	OtherCRS          []string             `xml:"OtherCRS"`
	WGS84BoundingBox  *WFSBoundingBox      `xml:"WGS84BoundingBox"`
	OutputFormats     []string             `xml:"OutputFormats>Format"`
}

type WFSBoundingBox struct {
	LowerCorner string `xml:"LowerCorner"` // "minx miny"
	UpperCorner string `xml:"UpperCorner"` // "maxx maxy"
}

// WMTS Capabilities XML 结构定义（支持 WMTS 1.0.0）
type WMTSCapabilities struct {
	XMLName               xml.Name                   `xml:"Capabilities"`
	Version               string                     `xml:"version,attr"`
	ServiceIdentification WMTSServiceIdentification  `xml:"ServiceIdentification"`
	Contents              WMTSContents               `xml:"Contents"`
}

type WMTSServiceIdentification struct {
	Title    string `xml:"Title"`
	Abstract string `xml:"Abstract"`
}

type WMTSContents struct {
	Layers []WMTSLayer `xml:"Layer"`
}

type WMTSLayer struct {
	Identifier        string                  `xml:"Identifier"`
	Title             string                  `xml:"Title"`
	Abstract          string                  `xml:"Abstract"`
	WGS84BoundingBox  *WFSBoundingBox         `xml:"WGS84BoundingBox"` // 复用 WFS 的结构
	Formats           []string                `xml:"Format"`
	Styles            []WMTSStyle             `xml:"Style"`
	TileMatrixSetLinks []WMTSTileMatrixSetLink `xml:"TileMatrixSetLink"`
}

type WMTSStyle struct {
	Identifier string `xml:"Identifier"`
	Title      string `xml:"Title"`
}

type WMTSTileMatrixSetLink struct {
	TileMatrixSet string `xml:"TileMatrixSet"`
}

// OGC API Features JSON 结构定义
type OGCAPICollections struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Collections []OGCAPICollection `json:"collections"`
}

type OGCAPICollection struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Extent      OGCAPIExtent    `json:"extent"`
	Links       []OGCAPILink    `json:"links"`
	CRS         []string        `json:"crs"`
	ItemType    string          `json:"itemType"`
}

type OGCAPIExtent struct {
	Spatial OGCAPISpatialExtent `json:"spatial"`
}

type OGCAPISpatialExtent struct {
	BBox [][]float64 `json:"bbox"` // [[minx, miny, maxx, maxy]]
}

type OGCAPILink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}


// addAuthHeaders 添加认证头
func (s *ExternalServiceService) addAuthHeaders(req *http.Request, service *models.ExternalService) error {
	switch service.AuthType {
	case "basic":
		if service.AuthConfig == nil {
			return fmt.Errorf("auth config is required for basic auth")
		}
		username, _ := service.AuthConfig["username"].(string)
		password, _ := service.AuthConfig["password"].(string)
		req.SetBasicAuth(username, password)

	case "bearer":
		if service.AuthConfig == nil {
			return fmt.Errorf("auth config is required for bearer auth")
		}
		token, _ := service.AuthConfig["token"].(string)
		req.Header.Set("Authorization", "Bearer "+token)

	case "api_key":
		if service.AuthConfig == nil {
			return fmt.Errorf("auth config is required for api_key auth")
		}
		headerName, _ := service.AuthConfig["header"].(string)
		apiKey, _ := service.AuthConfig["api_key"].(string)
		if headerName == "" {
			headerName = "X-API-Key"
		}
		req.Header.Set(headerName, apiKey)

	case "none", "":
		// 无需认证
	default:
		return fmt.Errorf("unsupported auth type: %s", service.AuthType)
	}

	return nil
}

// GetServiceCatalog 获取服务目录（按类型分类）
func (s *ExternalServiceService) GetServiceCatalog(ctx context.Context, tenantID uint) (map[string][]models.ExternalService, error) {
	services, err := s.repo.GetActiveServices(tenantID)
	if err != nil {
		return nil, err
	}

	catalog := make(map[string][]models.ExternalService)
	for _, service := range services {
		catalog[service.ServiceType] = append(catalog[service.ServiceType], service)
	}

	return catalog, nil
}

// SearchServices 搜索服务
func (s *ExternalServiceService) SearchServices(ctx context.Context, tenantID uint, keyword string, page, pageSize int) ([]models.ExternalService, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	return s.repo.SearchServices(tenantID, keyword, page, pageSize)
}

// ToDTO 转换为 DTO
func (s *ExternalServiceService) ToDTO(service *models.ExternalService) *models.ExternalServiceDTO {
	dto := &models.ExternalServiceDTO{
		ID:             service.ID,
		TenantID:       service.TenantID,
		Name:           service.Name,
		Description:    service.Description,
		ServiceType:    service.ServiceType,
		URL:            service.URL,
		Metadata:       service.Metadata,
		AuthType:       service.AuthType,
		Status:         service.Status,
		HealthCheckURL: string(service.HealthCheck),
		LastCheckedAt:  service.LastChecked,
		CreatedBy:      service.CreatedBy,
		CreatedAt:      service.CreatedAt,
		UpdatedAt:      service.UpdatedAt,
	}

	// 转换图层
	if len(service.Layers) > 0 {
		dto.Layers = make([]models.ExternalServiceLayerDTO, len(service.Layers))
		for i, layer := range service.Layers {
			dto.Layers[i] = models.ExternalServiceLayerDTO{
				ID:           layer.ID,
				ServiceID:    layer.ServiceID,
				LayerName:    layer.LayerName,
				DisplayName:  layer.DisplayName,
				GeometryType: layer.GeometryType,
				CRS:          layer.CRS,
				BBox:         layer.BBox,
				Metadata:     layer.Metadata,
				Enabled:      layer.Enabled,
				CreatedAt:    layer.CreatedAt,
			}
		}
	}

	return dto
}

// isOGCService 判断是否为 OGC 服务
func isOGCService(serviceType string) bool {
	return serviceType == "wms" || serviceType == "wfs" || serviceType == "wmts" || serviceType == "ogc_api"
}

// min 返回两个整数的最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// HealthCheck 执行健康检查
func (s *ExternalServiceService) HealthCheck(ctx context.Context, serviceID, tenantID uint) error {
	service, err := s.repo.GetByTenantAndID(tenantID, serviceID)
	if err != nil {
		return fmt.Errorf("service not found: %w", err)
	}

	checkURL := string(service.HealthCheck)
	if checkURL == "" {
		checkURL = service.URL
		// 如果未设置专门的健康检查 URL，对 OGC 服务添加 GetCapabilities 参数
		checkURL = s.buildOGCHealthCheckURL(checkURL, service.ServiceType)
	}

	// 输出调试日志
	fmt.Printf("[HealthCheck] ServiceID=%d, Type=%s, URL=%s\n", serviceID, service.ServiceType, checkURL)

	req, err := http.NewRequestWithContext(ctx, "GET", checkURL, nil)
	if err != nil {
		return err
	}

	if err := s.addAuthHeaders(req, service); err != nil {
		return err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		_ = s.repo.UpdateHealthCheck(serviceID, "error")
		fmt.Printf("[HealthCheck] Request failed: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	// 输出响应状态
	fmt.Printf("[HealthCheck] Response status: %d\n", resp.StatusCode)

	// 健康检查策略：
	// - 2xx/3xx: 服务正常
	// - 4xx (包括 401/403/418 等): 服务在线，但需要认证或被 WAF 拦截，仍视为"可达"
	// - 5xx: 服务内部错误，视为"不健康"
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		_ = s.repo.UpdateHealthCheck(serviceID, "active")
		if resp.StatusCode >= 400 {
			fmt.Printf("[HealthCheck] Service reachable but returned client error: %d (likely needs auth or blocked by WAF)\n", resp.StatusCode)
		}
		return nil
	}

	// 5xx 服务器错误才视为真正的健康检查失败
	_ = s.repo.UpdateHealthCheck(serviceID, "error")
	return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
}

// buildOGCHealthCheckURL 为 OGC 服务构建健康检查 URL
func (s *ExternalServiceService) buildOGCHealthCheckURL(baseURL, serviceType string) string {
	var params string
	switch serviceType {
	case "wmts":
		params = "?Service=WMTS&Request=GetCapabilities&Version=1.0.0"
	case "wms":
		params = "?Service=WMS&Request=GetCapabilities&Version=1.3.0"
	case "wfs":
		params = "?Service=WFS&Request=GetCapabilities&Version=2.0.0"
	default:
		return baseURL
	}

	// 检查 URL 是否已经包含查询参数
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '?' {
		return baseURL + params[1:] // 去掉 ? 前缀
	} else if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '&' {
		return baseURL + params[1:] // 去掉 ? 前缀
	} else if hasQueryParams(baseURL) {
		return baseURL + "&" + params[1:] // 使用 & 连接
	}
	return baseURL + params
}

// hasQueryParams 检查 URL 是否已包含查询参数
func hasQueryParams(url string) bool {
	for i := 0; i < len(url); i++ {
		if url[i] == '?' {
			return true
		}
	}
	return false
}

// BatchHealthCheck 批量健康检查
func (s *ExternalServiceService) BatchHealthCheck(ctx context.Context, tenantID uint) error {
	services, err := s.repo.GetActiveServices(tenantID)
	if err != nil {
		return err
	}

	for _, service := range services {
		// 异步执行健康检查（实际应该用 goroutine + 并发控制）
		_ = s.HealthCheck(ctx, service.ID, tenantID)
	}

	return nil
}

// ExportServiceConfig 导出服务配置（用于备份）
func (s *ExternalServiceService) ExportServiceConfig(ctx context.Context, tenantID uint) ([]byte, error) {
	services, err := s.repo.GetActiveServices(tenantID)
	if err != nil {
		return nil, err
	}

	// 移除敏感信息
	for i := range services {
		services[i].AuthConfig = nil // 不导出认证配置
	}

	return json.MarshalIndent(services, "", "  ")
}
