package wfs

import (
	"fmt"
	"strings"

	"github.com/addp/service/internal/models"
)

// DescribeFeatureTypeHandler WFS DescribeFeatureType 请求处理器
type DescribeFeatureTypeHandler struct{}

// NewDescribeFeatureTypeHandler 创建新的 DescribeFeatureType 处理器
func NewDescribeFeatureTypeHandler() *DescribeFeatureTypeHandler {
	return &DescribeFeatureTypeHandler{}
}

// DescribeFeatureTypeRequest DescribeFeatureType 请求参数
type DescribeFeatureTypeRequest struct {
	Service    string
	Version    string
	Request    string
	TypeName   string // 图层名称
	OutputFormat string // 输出格式：application/xml (XSD)
}

// PropertyDescription 属性描述
type PropertyDescription struct {
	Name     string
	Type     string
	Nillable bool
}

// GenerateXSD 生成 XSD Schema（WFS DescribeFeatureType 响应）
func GenerateXSD(layer *models.InternalServiceLayer, targetNamespace string) (string, error) {
	var xsd strings.Builder

	// 写入 XML 和 XSD 声明
	xsd.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	xsd.WriteString(fmt.Sprintf(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:%s="%s" targetNamespace="%s" elementFormDefault="qualified">`,
		"topp", targetNamespace, targetNamespace))

	// 导入 GML 3.2
	xsd.WriteString(`<xs:import namespace="http://www.opengis.net/gml/3.2.1" schemaLocation="http://schemas.opengis.net/gml/3.2.1/gml.xsd"/>`)

	// 定义要素类型
	xsd.WriteString(fmt.Sprintf(`<xs:element name="%s" type="%s:FeatureType"/>`, layer.LayerName, "topp"))

	xsd.WriteString(fmt.Sprintf(`<xs:complexType name="FeatureType">
		<xs:complexContent>
			<xs:extension base="gml:AbstractFeatureType">
				<xs:sequence>
					<xs:element name="geometry" type="gml:GeometryPropertyType"/>`,
		"topp"))

	// 添加属性（简化版，实际应该从数据库元数据获取）
	// TODO: 从 Meta 模块获取实际的列信息和类型
	for _, col := range layer.FilterColumns {
		// 推断类型（简化处理）
		xsType := "xs:string" // 默认为字符串
		if strings.Contains(col, "id") {
			xsType = "xs:integer"
		} else if strings.Contains(col, "population") || strings.Contains(col, "count") {
			xsType = "xs:integer"
		} else if strings.Contains(col, "area") || strings.Contains(col, "population") {
			xsType = "xs:double"
		} else if strings.Contains(col, "date") || strings.Contains(col, "time") {
			xsType = "xs:dateTime"
		}

		xsd.WriteString(fmt.Sprintf(`
					<xs:element name="%s" type="%s" minOccurs="0"/>`, col, xsType))
	}

	xsd.WriteString(`
				</xs:sequence>
			</xs:extension>
		</xs:complexContent>
	</xs:complexType>
</xs:schema>`)

	return xsd.String(), nil
}

// GeometryTypeToXSD 将 PostGIS 几何类型转换为 GML 几何类型
func GeometryTypeToXSD(geomType string) string {
	switch geomType {
	case "ST_Point", "POINT":
		return "gml:PointPropertyType"
	case "ST_LineString", "LINESTRING":
		return "gml:LineStringPropertyType"
	case "ST_Polygon", "POLYGON":
		return "gml:PolygonPropertyType"
	case "ST_MultiPoint", "MULTIPOINT":
		return "gml:MultiPointPropertyType"
	case "ST_MultiLineString", "MULTILINESTRING":
		return "gml:MultiLineStringPropertyType"
	case "ST_MultiPolygon", "MULTIPOLYGON":
		return "gml:MultiPolygonPropertyType"
	case "ST_GeometryCollection", "GEOMETRYCOLLECTION":
		return "gml:GeometryPropertyType"
	default:
		return "gml:GeometryPropertyType"
	}
}
