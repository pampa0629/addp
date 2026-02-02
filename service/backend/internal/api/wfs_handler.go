package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/logger"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/ogc/common"
	"github.com/addp/service/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// WFSHandler WFS 服务处理器
type WFSHandler struct {
	repo   *repository.InternalServiceRepository
	db     *gorm.DB
	sqlDB  *sql.DB // 用于 sql 查询
}

// NewWFSHandler 创建新的 WFS 处理器
func NewWFSHandler(repo *repository.InternalServiceRepository, db *gorm.DB, sqlDB *sql.DB) *WFSHandler {
	return &WFSHandler{
		repo:  repo,
		db:    db,
		sqlDB: sqlDB,
	}
}

// HandleWFSRequest 处理 WFS 请求的主处理器
// GET /ogc/wfs/:serviceName
func (h *WFSHandler) HandleWFSRequest(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 解析请求参数
	request := c.Query("request")
	service := c.Query("service")

	// 验证服务类型
	if service != "WFS" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service type. Expected 'WFS'"})
		return
	}

	// 验证服务是否存在且启用 WFS
	svc, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	if !svc.EnabledWFS {
		c.JSON(http.StatusForbidden, gin.H{"error": "WFS is not enabled for this service"})
		return
	}

	// 根据 request 参数分发到不同的处理器
	switch strings.ToUpper(request) {
	case "GETCAPABILITIES":
		h.GetCapabilities(c, serviceName, svc)

	case "DESCRIBEFEATURETYPE":
		h.DescribeFeatureType(c, serviceName, svc)

	case "GETFEATURE":
		h.GetFeature(c, serviceName, svc)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown or missing request parameter"})
	}
}

// GetCapabilities 处理 GetCapabilities 请求
func (h *WFSHandler) GetCapabilities(c *gin.Context, serviceName string, svc *models.InternalService) {
	// 生成 Capabilities XML
	baseURL := "http://" + c.Request.Host + "/ogc/wfs/" + serviceName

	logger.L().Debug("WFS GetCapabilities requested", "service", serviceName)

	// 生成完整的 WFS 2.0.0 Capabilities 文档
	capabilities := h.generateCapabilitiesXML(baseURL, svc)

	c.Header("Content-Type", "application/xml; charset=utf-8")
	c.String(http.StatusOK, capabilities)
}

// generateCapabilitiesXML 生成 WFS 2.0.0 Capabilities XML 文档
func (h *WFSHandler) generateCapabilitiesXML(baseURL string, svc *models.InternalService) string {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<wfs:WFS_Capabilities version="2.0.0"
  xmlns:wfs="http://www.opengis.net/wfs/2.0"
  xmlns:ows="http://www.opengis.net/ows/1.1"
  xmlns:gml="http://www.opengis.net/gml/3.2"
  xmlns:fes="http://www.opengis.net/fes/2.0"
  xmlns:xlink="http://www.w3.org/1999/xlink"
  xmlns:xs="http://www.w3.org/2001/XMLSchema"
  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
  xsi:schemaLocation="http://www.opengis.net/wfs/2.0 http://schemas.opengis.net/wfs/2.0/wfs.xsd">

  <!-- Service Identification -->
  <ows:ServiceIdentification>
    <ows:Title>` + escapeXML(svc.Title) + `</ows:Title>
    <ows:Abstract>` + escapeXML(svc.Abstract) + `</ows:Abstract>
    <ows:ServiceType>WFS</ows:ServiceType>
    <ows:ServiceTypeVersion>2.0.0</ows:ServiceTypeVersion>
  </ows:ServiceIdentification>

  <!-- Service Provider -->
  <ows:ServiceProvider>
    <ows:ProviderName>ADDP Platform</ows:ProviderName>
    <ows:ServiceContact>
      <ows:IndividualName>Administrator</ows:IndividualName>
    </ows:ServiceContact>
  </ows:ServiceProvider>

  <!-- Operations Metadata -->
  <ows:OperationsMetadata>
    <!-- GetCapabilities Operation -->
    <ows:Operation name="GetCapabilities">
      <ows:DCP>
        <ows:HTTP>
          <ows:Get xlink:href="` + baseURL + `?"/>
          <ows:Post xlink:href="` + baseURL + `"/>
        </ows:HTTP>
      </ows:DCP>
      <ows:Parameter name="AcceptVersions">
        <ows:AllowedValues>
          <ows:Value>2.0.0</ows:Value>
        </ows:AllowedValues>
      </ows:Parameter>
      <ows:Parameter name="AcceptFormats">
        <ows:AllowedValues>
          <ows:Value>text/xml</ows:Value>
        </ows:AllowedValues>
      </ows:Parameter>
    </ows:Operation>

    <!-- DescribeFeatureType Operation -->
    <ows:Operation name="DescribeFeatureType">
      <ows:DCP>
        <ows:HTTP>
          <ows:Get xlink:href="` + baseURL + `?"/>
          <ows:Post xlink:href="` + baseURL + `"/>
        </ows:HTTP>
      </ows:DCP>
      <ows:Parameter name="outputFormat">
        <ows:AllowedValues>
          <ows:Value>application/gml+xml; version=3.2</ows:Value>
          <ows:Value>text/xml; subtype=gml/3.2</ows:Value>
        </ows:AllowedValues>
      </ows:Parameter>
    </ows:Operation>

    <!-- GetFeature Operation -->
    <ows:Operation name="GetFeature">
      <ows:DCP>
        <ows:HTTP>
          <ows:Get xlink:href="` + baseURL + `?"/>
          <ows:Post xlink:href="` + baseURL + `"/>
        </ows:HTTP>
      </ows:DCP>
      <ows:Parameter name="resultType">
        <ows:AllowedValues>
          <ows:Value>results</ows:Value>
          <ows:Value>hits</ows:Value>
        </ows:AllowedValues>
      </ows:Parameter>
      <ows:Parameter name="outputFormat">
        <ows:AllowedValues>
          <ows:Value>application/gml+xml; version=3.2</ows:Value>
          <ows:Value>application/json</ows:Value>
          <ows:Value>text/xml; subtype=gml/3.2</ows:Value>
        </ows:AllowedValues>
      </ows:Parameter>
    </ows:Operation>

    <!-- Constraint: Implements Basic WFS -->
    <ows:Constraint name="ImplementsBasicWFS">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsTransactionalWFS">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsLockingWFS">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
  </ows:OperationsMetadata>

  <!-- Feature Type List -->
  <FeatureTypeList>
`

	// 添加每个图层的 FeatureType 定义
	for _, layer := range svc.Layers {
		if !layer.Enabled {
			continue
		}

		// 只有空间图层支持 WFS
		if layer.GeometryColumn == "" {
			continue
		}

		xml += `    <FeatureType>
      <Name>` + escapeXML(layer.LayerName) + `</Name>
      <Title>` + escapeXML(layer.Title) + `</Title>
      <Abstract>` + escapeXML(layer.Abstract) + `</Abstract>
      <ows:Keywords>
        <ows:Keyword>features</ows:Keyword>
        <ows:Keyword>` + escapeXML(layer.LayerName) + `</ows:Keyword>
      </ows:Keywords>
      <DefaultCRS>urn:ogc:def:crs:EPSG::` + strconv.Itoa(svc.DefaultSRID) + `</DefaultCRS>
      <OtherCRS>urn:ogc:def:crs:EPSG::4326</OtherCRS>
      <OtherCRS>urn:ogc:def:crs:EPSG::3857</OtherCRS>
`

		// 添加空间范围（WGS84 BBox）
		if layer.Extent4326 != nil {
			// Extent4326 是 JSONB 类型（map[string]interface{}），需要序列化后再解析为数组
			if bboxBytes, err := json.Marshal(layer.Extent4326); err == nil {
				var bbox []float64
				if err := json.Unmarshal(bboxBytes, &bbox); err == nil && len(bbox) == 4 {
					xml += `      <ows:WGS84BoundingBox>
        <ows:LowerCorner>` + strconv.FormatFloat(bbox[0], 'f', 6, 64) + ` ` + strconv.FormatFloat(bbox[1], 'f', 6, 64) + `</ows:LowerCorner>
        <ows:UpperCorner>` + strconv.FormatFloat(bbox[2], 'f', 6, 64) + ` ` + strconv.FormatFloat(bbox[3], 'f', 6, 64) + `</ows:UpperCorner>
      </ows:WGS84BoundingBox>
`
				}
			}
		}

		xml += `      <OutputFormats>
        <Format>application/gml+xml; version=3.2</Format>
        <Format>application/json</Format>
        <Format>text/xml; subtype=gml/3.2</Format>
      </OutputFormats>
    </FeatureType>
`
	}

	xml += `  </FeatureTypeList>

  <!-- Filter Capabilities -->
  <fes:Filter_Capabilities>
    <fes:Conformance>
      <fes:Constraint name="ImplementsQuery">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsAdHocQuery">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsFunctions">
        <ows:NoValues/>
        <ows:DefaultValue>FALSE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsMinStandardFilter">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsStandardFilter">
        <ows:NoValues/>
        <ows:DefaultValue>FALSE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsMinSpatialFilter">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsSpatialFilter">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsMinTemporalFilter">
        <ows:NoValues/>
        <ows:DefaultValue>FALSE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsTemporalFilter">
        <ows:NoValues/>
        <ows:DefaultValue>FALSE</ows:DefaultValue>
      </fes:Constraint>
    </fes:Conformance>
    <fes:Spatial_Capabilities>
      <fes:GeometryOperands>
        <fes:GeometryOperand name="gml:Point"/>
        <fes:GeometryOperand name="gml:LineString"/>
        <fes:GeometryOperand name="gml:Polygon"/>
        <fes:GeometryOperand name="gml:MultiPoint"/>
        <fes:GeometryOperand name="gml:MultiLineString"/>
        <fes:GeometryOperand name="gml:MultiPolygon"/>
      </fes:GeometryOperands>
      <fes:SpatialOperators>
        <fes:SpatialOperator name="BBOX"/>
        <fes:SpatialOperator name="Intersects"/>
        <fes:SpatialOperator name="Within"/>
        <fes:SpatialOperator name="Contains"/>
      </fes:SpatialOperators>
    </fes:Spatial_Capabilities>
  </fes:Filter_Capabilities>

</wfs:WFS_Capabilities>`

	return xml
}

// escapeXML 转义 XML 特殊字符
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// DescribeFeatureType 处理 DescribeFeatureType 请求
func (h *WFSHandler) DescribeFeatureType(c *gin.Context, serviceName string, svc *models.InternalService) {
	typeName := c.Query("typeName")
	if typeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required parameter: typeName"})
		return
	}

	// 查询图层
	layer, err := h.repo.GetLayerByName(svc.ID, typeName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "FeatureType not found"})
		return
	}

	if !layer.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "FeatureType is disabled"})
		return
	}

	// 只有空间图层支持 WFS
	if layer.GeometryColumn == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "FeatureType does not contain spatial data"})
		return
	}

	// 从数据库获取字段信息
	columns, err := h.getTableColumns(layer.SchemaName, layer.DBTableName)
	if err != nil {
		logger.L().Error("Failed to get table columns", "error", err, "schema", layer.SchemaName, "table", layer.DBTableName)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve feature type schema"})
		return
	}

	// 生成 XML Schema
	baseURL := "http://" + c.Request.Host + "/ogc/wfs/" + serviceName
	schema := h.generateFeatureTypeSchema(baseURL, typeName, layer, columns)

	c.Header("Content-Type", "application/xml; charset=utf-8")
	c.String(http.StatusOK, schema)

	logger.L().Debug("WFS DescribeFeatureType requested", "service", serviceName, "typeName", typeName)
}

// ColumnInfo 字段信息
type ColumnInfo struct {
	Name         string
	DataType     string
	IsNullable   bool
	CharMaxLen   *int
	NumPrecision *int
	NumScale     *int
}

// getTableColumns 从数据库获取表的字段信息
func (h *WFSHandler) getTableColumns(schemaName, tableName string) ([]ColumnInfo, error) {
	query := `
		SELECT
			column_name,
			data_type,
			is_nullable = 'YES' as is_nullable,
			character_maximum_length,
			numeric_precision,
			numeric_scale
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	rows, err := h.sqlDB.Query(query, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		err := rows.Scan(&col.Name, &col.DataType, &col.IsNullable, &col.CharMaxLen, &col.NumPrecision, &col.NumScale)
		if err != nil {
			return nil, err
		}
		columns = append(columns, col)
	}

	return columns, rows.Err()
}

// generateFeatureTypeSchema 生成 FeatureType 的 XML Schema
func (h *WFSHandler) generateFeatureTypeSchema(baseURL, typeName string, layer *models.InternalServiceLayer, columns []ColumnInfo) string {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<xsd:schema xmlns:xsd="http://www.w3.org/2001/XMLSchema"
            xmlns:gml="http://www.opengis.net/gml/3.2"
            xmlns:wfs="http://www.opengis.net/wfs/2.0"
            xmlns:` + typeName + `="` + baseURL + `"
            targetNamespace="` + baseURL + `"
            elementFormDefault="qualified"
            version="1.0">

  <xsd:import namespace="http://www.opengis.net/gml/3.2"
              schemaLocation="http://schemas.opengis.net/gml/3.2.1/gml.xsd"/>

  <!-- Feature Type Definition -->
  <xsd:element name="` + typeName + `" type="` + typeName + `:` + typeName + `Type" substitutionGroup="gml:AbstractFeature"/>

  <xsd:complexType name="` + typeName + `Type">
    <xsd:complexContent>
      <xsd:extension base="gml:AbstractFeatureType">
        <xsd:sequence>
`

	// 添加每个字段
	for _, col := range columns {
		// 跳过几何列（单独处理）
		if col.Name == layer.GeometryColumn {
			continue
		}

		xsdType := h.postgresTypeToXSD(col.DataType)
		minOccurs := "1"
		if col.IsNullable {
			minOccurs = "0"
		}

		xml += `          <xsd:element name="` + escapeXML(col.Name) + `" type="` + xsdType + `" minOccurs="` + minOccurs + `" maxOccurs="1"`

		// 对于字符串类型，添加长度限制
		if col.CharMaxLen != nil && *col.CharMaxLen > 0 {
			xml += `>
            <xsd:simpleType>
              <xsd:restriction base="` + xsdType + `">
                <xsd:maxLength value="` + strconv.Itoa(*col.CharMaxLen) + `"/>
              </xsd:restriction>
            </xsd:simpleType>
          </xsd:element>
`
		} else {
			xml += `/>`
		}
		xml += "\n"
	}

	// 添加几何字段
	if layer.GeometryColumn != "" {
		geometryType := "gml:GeometryPropertyType"
		// 如果有具体的几何类型，使用更具体的类型
		if len(layer.GeometryTypes) > 0 {
			switch layer.GeometryTypes[0] {
			case "Point", "MultiPoint":
				geometryType = "gml:PointPropertyType"
			case "LineString", "MultiLineString":
				geometryType = "gml:CurvePropertyType"
			case "Polygon", "MultiPolygon":
				geometryType = "gml:SurfacePropertyType"
			}
		}

		xml += `          <xsd:element name="` + escapeXML(layer.GeometryColumn) + `" type="` + geometryType + `" minOccurs="0" maxOccurs="1"/>
`
	}

	xml += `        </xsd:sequence>
      </xsd:extension>
    </xsd:complexContent>
  </xsd:complexType>

</xsd:schema>`

	return xml
}

// postgresTypeToXSD 将 PostgreSQL 数据类型映射到 XSD 类型
func (h *WFSHandler) postgresTypeToXSD(pgType string) string {
	typeMap := map[string]string{
		// 整数类型
		"smallint":          "xsd:short",
		"integer":           "xsd:int",
		"bigint":            "xsd:long",
		"smallserial":       "xsd:short",
		"serial":            "xsd:int",
		"bigserial":         "xsd:long",

		// 浮点类型
		"real":              "xsd:float",
		"double precision":  "xsd:double",
		"numeric":           "xsd:decimal",
		"decimal":           "xsd:decimal",

		// 字符串类型
		"character varying": "xsd:string",
		"varchar":           "xsd:string",
		"character":         "xsd:string",
		"char":              "xsd:string",
		"text":              "xsd:string",

		// 布尔类型
		"boolean":           "xsd:boolean",

		// 日期时间类型
		"timestamp":                        "xsd:dateTime",
		"timestamp without time zone":     "xsd:dateTime",
		"timestamp with time zone":        "xsd:dateTime",
		"date":                             "xsd:date",
		"time":                             "xsd:time",
		"time without time zone":          "xsd:time",
		"time with time zone":             "xsd:time",

		// UUID
		"uuid":              "xsd:string",

		// JSON
		"json":              "xsd:string",
		"jsonb":             "xsd:string",

		// 数组（简化为字符串）
		"ARRAY":             "xsd:string",
	}

	if xsdType, ok := typeMap[pgType]; ok {
		return xsdType
	}

	// 处理数组类型（PostgreSQL 中以 ARRAY 结尾）
	if len(pgType) > 2 && pgType[len(pgType)-2:] == "[]" {
		return "xsd:string"
	}

	// 默认返回字符串类型
	return "xsd:string"
}

// GetFeature 处理 GetFeature 请求
func (h *WFSHandler) GetFeature(c *gin.Context, serviceName string, svc *models.InternalService) {
	typeName := c.Query("typeName")
	if typeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required parameter: typeName"})
		return
	}

	// 查询图层
	layer, err := h.repo.GetLayerByName(svc.ID, typeName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "FeatureType not found"})
		return
	}

	if !layer.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "FeatureType is disabled"})
		return
	}

	// 只有空间图层支持 WFS
	if layer.GeometryColumn == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "FeatureType does not contain spatial data"})
		return
	}

	// 解析查询参数
	outputFormat := c.DefaultQuery("outputFormat", "application/gml+xml; version=3.2")
	maxFeatures := 100 // 默认值
	if mf := c.Query("maxFeatures"); mf != "" {
		if parsed, err := strconv.Atoi(mf); err == nil && parsed > 0 {
			maxFeatures = parsed
		}
	}
	// WFS 2.0 使用 count 参数
	if count := c.Query("count"); count != "" {
		if parsed, err := strconv.Atoi(count); err == nil && parsed > 0 {
			maxFeatures = parsed
		}
	}

	// 应用图层或服务的最大要素数限制
	maxFeaturesLimit := svc.MaxFeatures
	if layer.MaxFeatures != nil && *layer.MaxFeatures > 0 {
		maxFeaturesLimit = *layer.MaxFeatures
	}
	if maxFeatures > maxFeaturesLimit {
		maxFeatures = maxFeaturesLimit
	}

	startIndex := 0
	if si := c.Query("startIndex"); si != "" {
		if parsed, err := strconv.Atoi(si); err == nil && parsed >= 0 {
			startIndex = parsed
		}
	}

	// 解析 bbox 参数（格式：minx,miny,maxx,maxy[,crs]）
	var bbox *common.BBox
	if bboxStr := c.Query("bbox"); bboxStr != "" {
		bbox, err = common.ParseBBox(bboxStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid bbox parameter", "details": err.Error()})
			return
		}
	}

	// 解析 filter 参数（CQL）
	filter := c.Query("filter")

	// 解析 sortBy 参数（格式：field1 A,field2 D）
	sortBy := c.Query("sortBy")

	// 解析坐标系参数
	srid := svc.DefaultSRID
	if crsParam := c.Query("srsName"); crsParam != "" {
		// 支持格式：EPSG:4326 或 urn:ogc:def:crs:EPSG::4326
		if parsed, err := strconv.Atoi(crsParam); err == nil {
			srid = parsed
		} else if len(crsParam) > 5 && crsParam[:5] == "EPSG:" {
			if parsed, err := strconv.Atoi(crsParam[5:]); err == nil {
				srid = parsed
			}
		} else if len(crsParam) > 28 && crsParam[:28] == "urn:ogc:def:crs:EPSG::" {
			if parsed, err := strconv.Atoi(crsParam[28:]); err == nil {
				srid = parsed
			}
		}
	}

	// 构建查询参数（复用 OGC API 的查询引擎）
	queryParams := &common.QueryParams{
		LayerID: layer.ID,
		SRID:    srid,
		BBOX:    bbox,
		Filter:  filter,
		Limit:   maxFeatures,
		Offset:  startIndex,
	}

	// 构建查询 SQL
	sql, args, err := common.BuildFeatureQuery(layer, queryParams)
	if err != nil {
		logger.L().Error("Failed to build feature query", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid query parameters", "details": err.Error()})
		return
	}

	// 如果有 sortBy 参数，追加到 SQL
	if sortBy != "" {
		// 简单验证防止 SQL 注入
		if !strings.Contains(strings.ToUpper(sortBy), "DROP") &&
			!strings.Contains(strings.ToUpper(sortBy), "DELETE") {
			sql = strings.TrimSuffix(sql, " LIMIT $1 OFFSET $2") + " ORDER BY " + sortBy + " LIMIT $1 OFFSET $2"
		}
	}

	// 执行查询
	rows, err := h.db.Raw(sql, args...).Rows()
	if err != nil {
		logger.L().Error("Failed to execute feature query", "error", err, "sql", sql)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query features"})
		return
	}
	defer rows.Close()

	// 获取列名
	columns, err := rows.Columns()
	if err != nil {
		logger.L().Error("Failed to get columns", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process query results"})
		return
	}

	// 转换为 GeoJSON Features
	features, err := common.RowsToFeatures(rows, columns)
	if err != nil {
		logger.L().Error("Failed to convert rows to features", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process features"})
		return
	}

	// 处理 geometry 字段中的 __rawJSON 标记
	processedFeatures := make([]map[string]interface{}, 0, len(features))
	for _, feature := range features {
		if geom, ok := feature["geometry"].(map[string]interface{}); ok {
			if rawJSON, ok := geom["__rawJSON"].(string); ok {
				var geometryObj interface{}
				if err := json.Unmarshal([]byte(rawJSON), &geometryObj); err == nil {
					feature["geometry"] = geometryObj
				}
			}
		}
		processedFeatures = append(processedFeatures, feature)
	}

	// 根据 outputFormat 返回不同格式
	switch {
	case strings.Contains(outputFormat, "json"):
		// 返回 GeoJSON 格式
		h.respondGeoJSON(c, serviceName, typeName, processedFeatures, maxFeatures, startIndex)

	case strings.Contains(outputFormat, "gml"):
		// 返回 GML 格式（简化实现，实际应该生成完整的 GML XML）
		h.respondGML(c, serviceName, typeName, processedFeatures, srid)

	default:
		// 默认返回 GeoJSON
		h.respondGeoJSON(c, serviceName, typeName, processedFeatures, maxFeatures, startIndex)
	}

	logger.L().Debug("WFS GetFeature returned",
		"service", serviceName,
		"typeName", typeName,
		"count", len(processedFeatures),
		"outputFormat", outputFormat)
}

// respondGeoJSON 返回 GeoJSON 格式的 FeatureCollection
func (h *WFSHandler) respondGeoJSON(c *gin.Context, serviceName, typeName string, features []map[string]interface{}, limit, offset int) {
	response := map[string]interface{}{
		"type":           "FeatureCollection",
		"features":       features,
		"numberReturned": len(features),
		"timeStamp":      nil, // TODO: 添加时间戳
	}

	c.JSON(http.StatusOK, response)
}

// respondGML 返回 GML 格式的 FeatureCollection（简化实现）
func (h *WFSHandler) respondGML(c *gin.Context, serviceName, typeName string, features []map[string]interface{}, srid int) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<wfs:FeatureCollection
  xmlns:wfs="http://www.opengis.net/wfs/2.0"
  xmlns:gml="http://www.opengis.net/gml/3.2"
  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
  xmlns:` + typeName + `="http://` + c.Request.Host + `/ogc/wfs/` + serviceName + `"
  numberReturned="` + strconv.Itoa(len(features)) + `"
  timeStamp="` + "2026-01-01T00:00:00Z" + `"
  xsi:schemaLocation="http://www.opengis.net/wfs/2.0 http://schemas.opengis.net/wfs/2.0/wfs.xsd
                      http://www.opengis.net/gml/3.2 http://schemas.opengis.net/gml/3.2.1/gml.xsd">

  <wfs:boundedBy>
    <gml:Envelope srsName="urn:ogc:def:crs:EPSG::` + strconv.Itoa(srid) + `">
      <gml:lowerCorner>0 0</gml:lowerCorner>
      <gml:upperCorner>180 90</gml:upperCorner>
    </gml:Envelope>
  </wfs:boundedBy>

`

	// 添加每个要素（简化实现，实际应该根据几何类型生成不同的 GML）
	for i, feature := range features {
		gmlID := typeName + "." + strconv.Itoa(i+1)
		if idVal, ok := feature["id"]; ok {
			gmlID = typeName + "." + strconv.Itoa(int(idVal.(float64)))
		}

		xml += `  <wfs:member>
    <` + typeName + `:` + typeName + ` gml:id="` + gmlID + `">
`

		// 添加属性
		for key, val := range feature {
			if key == "type" || key == "id" || key == "geometry" {
				continue
			}
			if val != nil {
				xml += `      <` + typeName + `:` + escapeXML(key) + `>` + escapeXML(jsonValueToString(val)) + `</` + typeName + `:` + escapeXML(key) + `>
`
			}
		}

		// 添加几何（简化实现，仅支持 Point）
		if geom, ok := feature["geometry"].(map[string]interface{}); ok {
			if geomType, ok := geom["type"].(string); ok && geomType == "Point" {
				if coords, ok := geom["coordinates"].([]interface{}); ok && len(coords) >= 2 {
					x := coords[0].(float64)
					y := coords[1].(float64)
					xml += `      <` + typeName + `:geom>
        <gml:Point srsName="urn:ogc:def:crs:EPSG::` + strconv.Itoa(srid) + `">
          <gml:pos>` + strconv.FormatFloat(x, 'f', 6, 64) + ` ` + strconv.FormatFloat(y, 'f', 6, 64) + `</gml:pos>
        </gml:Point>
      </` + typeName + `:geom>
`
				}
			}
		}

		xml += `    </` + typeName + `:` + typeName + `>
  </wfs:member>
`
	}

	xml += `</wfs:FeatureCollection>`

	c.Header("Content-Type", "application/gml+xml; version=3.2")
	c.String(http.StatusOK, xml)
}

// jsonValueToString 将 JSON 值转换为字符串
func jsonValueToString(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	default:
		return ""
	}
}

// PostGetFeature 处理 POST 方式的 GetFeature 请求
// POST /ogc/wfs/:serviceName
func (h *WFSHandler) PostGetFeature(c *gin.Context) {
	// TODO: 实现 POST GetFeature
	c.JSON(http.StatusOK, gin.H{"message": "POST GetFeature not yet implemented"})
}
