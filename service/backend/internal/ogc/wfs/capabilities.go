package wfs

import (
	"encoding/xml"
	"fmt"

	"github.com/addp/service/internal/models"
)

// WFSCapabilities WFS GetCapabilities 响应
type WFSCapabilities struct {
	XMLName xml.Name `xml:"WFS_Capabilities"`
	Version string   `xml:"version,attr"`
	Xmlns   map[string]string
	// ... 其他字段将在下面定义
	ServiceIdentification *ServiceIdentification `xml:"ServiceIdentification"`
	ServiceProvider       *ServiceProvider       `xml:"ServiceProvider"`
	OperationsMetadata    *OperationsMetadata    `xml:"OperationsMetadata"`
	FeatureTypeList       *FeatureTypeList       `xml:"FeatureTypeList"`
}

// ServiceIdentification 服务标识
type ServiceIdentification struct {
	Title       string   `xml:"Title"`
	Abstract    string   `xml:"Abstract,omitempty"`
	Keywords    *Keywords `xml:"Keywords,omitempty"`
	ServiceType string   `xml:"ServiceType"`
	ServiceTypeVersion string `xml:"ServiceTypeVersion"`
}

// Keywords 关键词
type Keywords struct {
	Keyword []string `xml:"Keyword"`
}

// ServiceProvider 服务提供者
type ServiceProvider struct {
	ProviderName string       `xml:"ProviderName,omitempty"`
	ProviderSite *ProviderSite `xml:"ProviderSite,omitempty"`
	ServiceContact *ServiceContact `xml:"ServiceContact,omitempty"`
}

// ProviderSite 提供者网站
type ProviderSite struct {
	Href string `xml:"href,attr"`
}

// ServiceContact 服务联系信息
type ServiceContact struct {
	IndividualName string `xml:"IndividualName,omitempty"`
	ElectronicMailAddress string `xml:"ElectronicMailAddress,omitempty"`
}

// OperationsMetadata 操作元数据
type OperationsMetadata struct {
	Operation []*Operation `xml:"Operation"`
}

// Operation 操作定义
type Operation struct {
	Name string       `xml:"name,attr"`
	DCP  *DCP         `xml:"DCP"`
}

// DCP 分布式计算平台
type DCP struct {
	HTTP *HTTP `xml:"HTTP"`
}

// HTTP HTTP 端点
type HTTP struct {
	Get  *HTTPMethod `xml:"Get,omitempty"`
	Post *HTTPMethod `xml:"Post,omitempty"`
}

// HTTPMethod HTTP 方法
type HTTPMethod struct {
	Href string `xml:"href,attr"`
}

// FeatureTypeList 要素类型列表
type FeatureTypeList struct {
	FeatureType []*FeatureType `xml:"FeatureType"`
}

// FeatureType 要素类型（对应我们的图层）
type FeatureType struct {
	Name           string           `xml:"Name"`
	Title          string           `xml:"Title"`
	Abstract       string           `xml:"Abstract,omitempty"`
	Keywords       *Keywords        `xml:"Keywords,omitempty"`
	DefaultCRS     string           `xml:"DefaultCRS"`
	OtherCRS       []string         `xml:"OtherCRS"`
	WGS84BoundingBox *WGS84BoundingBox `xml:"WGS84BoundingBox"`
}

// WGS84BoundingBox WGS84 范围
type WGS84BoundingBox struct {
	LowerCorner string `xml:"LowerCorner"`
	UpperCorner string `xml:"UpperCorner"`
}

// GenerateCapabilities 生成 WFS GetCapabilities XML
func GenerateCapabilities(service *models.InternalService, baseURL string) (string, error) {
	// 支持的 SRID 列表
	otherCRS := []string{
		"urn:ogc:def:crs:EPSG::3857",
		"urn:ogc:def:crs:EPSG::4490",
		"urn:ogc:def:crs:EPSG::2000",
		"urn:ogc:def:crs:EPSG::4214",
	}

	// 构建要素类型列表
	featureTypes := make([]*FeatureType, 0)
	for _, layer := range service.Layers {
		if !layer.Enabled {
			continue
		}

		// 处理 Extent4326
		var extent4326 *WGS84BoundingBox
		if layer.Extent4326 != nil {
			// 假设 Extent4326 是 JSONB，包含 [minLng, minLat, maxLng, maxLat]
			// 这里需要解析 JSONB 数据
			// TODO: 正确解析 JSONB 数据
			extent4326 = &WGS84BoundingBox{
				LowerCorner: "0 0",       // 应从 extent4326[0] extent4326[1]
				UpperCorner: "180 90",    // 应从 extent4326[2] extent4326[3]
			}
		}

		// 处理关键词
		var keywords *Keywords
		if len(layer.Keywords) > 0 {
			keywords = &Keywords{Keyword: layer.Keywords}
		}

		ft := &FeatureType{
			Name:             layer.LayerName,
			Title:            layer.Title,
			Abstract:         layer.Abstract,
			Keywords:         keywords,
			DefaultCRS:       fmt.Sprintf("urn:ogc:def:crs:EPSG::%d", service.DefaultSRID),
			OtherCRS:         otherCRS,
			WGS84BoundingBox: extent4326,
		}

		featureTypes = append(featureTypes, ft)
	}

	// 处理服务关键词
	var serviceKeywords *Keywords
	if len(service.Keywords) > 0 {
		serviceKeywords = &Keywords{Keyword: service.Keywords}
	}

	// 构建 Capabilities 对象
	caps := &WFSCapabilities{
		Version: "2.0.0",
		ServiceIdentification: &ServiceIdentification{
			Title:              service.Title,
			Abstract:           service.Abstract,
			Keywords:           serviceKeywords,
			ServiceType:        "WFS",
			ServiceTypeVersion: "2.0.0",
		},
		ServiceProvider: &ServiceProvider{
			ProviderName: service.ProviderName,
			ProviderSite: &ProviderSite{Href: service.ProviderSite},
			ServiceContact: &ServiceContact{
				IndividualName:        service.ContactPerson,
				ElectronicMailAddress: service.ContactEmail,
			},
		},
		OperationsMetadata: &OperationsMetadata{
			Operation: []*Operation{
				{
					Name: "GetCapabilities",
					DCP: &DCP{
						HTTP: &HTTP{
							Get: &HTTPMethod{Href: baseURL + "?service=WFS&version=2.0.0&request=GetCapabilities"},
						},
					},
				},
				{
					Name: "DescribeFeatureType",
					DCP: &DCP{
						HTTP: &HTTP{
							Get: &HTTPMethod{Href: baseURL + "?service=WFS&version=2.0.0&request=DescribeFeatureType"},
						},
					},
				},
				{
					Name: "GetFeature",
					DCP: &DCP{
						HTTP: &HTTP{
							Get:  &HTTPMethod{Href: baseURL + "?service=WFS&version=2.0.0&request=GetFeature"},
							Post: &HTTPMethod{Href: baseURL},
						},
					},
				},
			},
		},
		FeatureTypeList: &FeatureTypeList{
			FeatureType: featureTypes,
		},
	}

	// 序列化为 XML
	xmlData, err := xml.MarshalIndent(caps, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal capabilities: %w", err)
	}

	// 添加 XML 声明
	result := xml.Header + string(xmlData)
	return result, nil
}

// ParseBBox 从 Extent4326 JSONB 解析边界框
// 假设格式为 [minLng, minLat, maxLng, maxLat]
func ParseBBox(extentJSON interface{}) (string, string, error) {
	// TODO: 实现从 JSONB 解析边界框的逻辑
	// 现在返回默认值
	return "0 0", "180 90", nil
}
