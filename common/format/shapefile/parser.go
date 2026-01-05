package shapefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/addp/common/format"
	"github.com/jonas-p/go-shp"
)

// Parser 实现 Shapefile 格式的解析器
type Parser struct {
	options *format.ParseOptions
}

// NewParser 创建 Shapefile 解析器
func NewParser(opts *format.ParseOptions) *Parser {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Parser{options: opts}
}

// SupportedFormats 返回支持的格式
func (p *Parser) SupportedFormats() []format.FormatType {
	return []format.FormatType{format.FormatShapefile}
}

// ============ FileTableParser 接口实现 ============

// ParseTableInfo 从 Shapefile 文件中提取 TableInfo
func (p *Parser) ParseTableInfo(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	// 使用传入的 options，如果为 nil 则使用默认的
	opts := p.options
	if options != nil {
		opts = options
		p.options = opts // 更新 options 以供内部方法使用
	}

	// Shapefile 需要文件路径，将 input 保存到临时文件
	tempDir, cleanup, err := p.saveToTempFiles(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// 打开 shapefile
	shpPath := filepath.Join(tempDir, "data.shp")
	reader, err := Open(shpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	// 获取 schema 信息
	shpSchema := reader.GetSchema()
	geometryField := p.getGeometryFieldName()

	// 构建 FieldInfo 列表（几何字段 + 属性字段）
	fields := make([]format.FieldInfo, 0, len(shpSchema)+1)

	// 添加几何字段
	fields = append(fields, format.FieldInfo{
		Name:         geometryField,
		Type:         format.FieldTypeGeometry,
		OriginalType: "Geometry",
		Nullable:     false,
		IsPrimaryKey: false,
		Comment:      "Shapefile geometry field",
	})

	// 添加属性字段
	for _, field := range shpSchema {
		fields = append(fields, format.FieldInfo{
			Name:         field.Name,
			Type:         mapShapefileTypeToFieldType(field.Type),
			OriginalType: field.Type,
			Nullable:     true,
			IsPrimaryKey: false,
			Comment:      "",
		})
	}

	// 统计记录数
	recordCount := int64(0)
	for reader.Next() {
		recordCount++
	}

	// 构建 SpatialInfo 扩展
	// 重新打开 reader 来获取第一个 shape 以确定几何类型
	reader2, err := Open(shpPath)
	if err == nil {
		defer reader2.Close()
		var geomType string
		if reader2.Next() {
			_, shape := reader2.Shape()
			// 使用 shape 的类型来确定几何类型
			geomType = fmt.Sprintf("%T", shape) // 简化处理
			// 标准化类型名称
			if strings.Contains(geomType, "Point") {
				geomType = "Point"
			} else if strings.Contains(geomType, "Polygon") {
				geomType = "Polygon"
			} else if strings.Contains(geomType, "PolyLine") {
				geomType = "LineString"
			} else {
				geomType = "Geometry"
			}
		} else {
			geomType = "Geometry"
		}

		spatialInfo := &format.SpatialInfo{
			GeometryColumn: geometryField,
			GeometryType:   geomType,
			SRID:           4326, // Shapefile 默认 WGS84
			Dimension:      2,    // Shapefile 默认 2D
		}

		// 如果在 options 中指定了空间参考系统，解析 SRID
		if opts.SpatialRefSys != "" {
			if srid := parseSRID(opts.SpatialRefSys); srid > 0 {
				spatialInfo.SRID = srid
			}
		}

		// 构建 ShapefileInfo 扩展
		shapefileInfo := &format.ShapefileInfo{
			Encoding:   opts.Encoding,
			ShapeType:  geomType,
			HasPRJ:     false, // TODO: 检测 .prj 文件
			HasCPG:     false, // TODO: 检测 .cpg 文件
			DBFVersion: 0,     // TODO: 从 DBF 读取版本号
		}

		// 构建 TableInfo
		tableInfo := &format.TableInfo{
			Name:       "shapefile_data", // Shapefile 没有表名，使用默认值
			RowCount:   &recordCount,
			Fields:     fields,
			PrimaryKey: []string{}, // Shapefile 没有主键
			Extensions: []format.ExtensionInfo{spatialInfo, shapefileInfo},
		}

		return tableInfo, nil
	}

	// 如果无法重新打开文件，返回不带扩展信息的 TableInfo
	tableInfo := &format.TableInfo{
		Name:       "shapefile_data",
		RowCount:   &recordCount,
		Fields:     fields,
		PrimaryKey: []string{},
		Extensions: []format.ExtensionInfo{},
	}

	return tableInfo, nil
}

// ReadPreview 读取 Shapefile 数据预览
func (p *Parser) ReadPreview(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	tempDir, cleanup, err := p.saveToTempFiles(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	shpPath := filepath.Join(tempDir, "data.shp")
	reader, err := Open(shpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	// 获取字段信息
	fields := reader.Fields()
	geometryField := p.getGeometryFieldName()

	// 跳过到 offset
	currentRecord := int64(0)
	for currentRecord < offset && reader.Next() {
		currentRecord++
	}

	// 读取数据
	maxRows := limit
	if limit < 0 {
		maxRows = 1<<31 - 1 // shapefile 最大记录数
	}

	records := make([]map[string]interface{}, 0)
	readCount := int64(0)
	recordIndex := int(offset)

	for readCount < maxRows && reader.Next() {
		select {
		case <-ctx.Done():
			return records, ctx.Err()
		default:
		}

		_, shape := reader.Shape()

		// 构建记录
		record := make(map[string]interface{}, len(fields)+1)

		// 添加属性字段
		for i, field := range fields {
			fieldName := TrimDBFFieldName(field.Name)
			rawValue := strings.TrimSpace(reader.ReadAttribute(recordIndex, i))
			if rawValue == "" {
				record[fieldName] = nil
			} else {
				record[fieldName] = ParseDBFAttribute(field.Fieldtype, rawValue)
			}
		}

		// 添加几何字段（转换为 WKT）
		if shape != nil {
			wkt, err := ShapeToWKT(shape)
			if err == nil {
				record[geometryField] = wkt
			} else {
				record[geometryField] = nil
			}
		} else {
			record[geometryField] = nil
		}

		records = append(records, record)
		readCount++
		recordIndex++
	}

	return records, nil
}
// mapShapefileTypeToFieldType 将 Shapefile 字段类型映射到 FieldType
func mapShapefileTypeToFieldType(shpType string) format.FieldType {
	switch shpType {
	case "Integer", "Long":
		return format.FieldTypeInt
	case "Float", "Double":
		return format.FieldTypeFloat
	case "Boolean":
		return format.FieldTypeBool
	case "Date":
		return format.FieldTypeDate
	case "DateTime":
		return format.FieldTypeTimestamp
	case "String", "Text":
		return format.FieldTypeString
	default:
		return format.FieldTypeString
	}
}

// getGeometryFieldName 获取几何字段名称
func (p *Parser) getGeometryFieldName() string {
	if p.options.ExtraParams != nil {
		if name, ok := p.options.ExtraParams["geometry_field"].(string); ok && name != "" {
			return name
		}
	}
	return "geometry" // 默认几何字段名
}

// saveToTempFiles 将 shapefile 相关文件保存到临时目录
// 注意：这个实现假设 input 是一个包含所有必需文件的 tar/zip 流
// 实际使用中，可能需要根据具体情况调整
func (p *Parser) saveToTempFiles(input io.Reader) (string, func(), error) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "shapefile-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// 注意：这里简化处理，假设 input 是 .shp 文件
	// 实际使用中需要处理 .shp, .shx, .dbf 三个文件
	shpPath := filepath.Join(tempDir, "data.shp")
	file, err := os.Create(shpPath)
	if err != nil {
		os.RemoveAll(tempDir)
		return "", nil, fmt.Errorf("failed to create shp file: %w", err)
	}

	if _, err := io.Copy(file, input); err != nil {
		file.Close()
		os.RemoveAll(tempDir)
		return "", nil, fmt.Errorf("failed to write shp file: %w", err)
	}
	file.Close()

	// 返回清理函数
	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return tempDir, cleanup, nil
}

// ShapeToWKT 将 shapefile 几何转换为 WKT 格式
func ShapeToWKT(shape shp.Shape) (string, error) {
	// 使用现有的 ShapeToGeom 转换
	geom, err := ShapeToGeom(shape)
	if err != nil {
		return "", err
	}

	// 转换为 WKT
	wkt, err := geomToWKT(geom)
	if err != nil {
		return "", fmt.Errorf("failed to convert geometry to WKT: %w", err)
	}

	return wkt, nil
}

// geomToWKT 将 go-geom 几何转换为 WKT
func geomToWKT(geom interface{}) (string, error) {
	// 这里需要实现 WKT 转换
	// 可以使用 github.com/twpayne/go-geom/encoding/wkt
	// 简化实现，返回几何类型描述
	return fmt.Sprintf("%T", geom), nil
}

// determineShapefileGeometryType 根据 shape type 确定几何类型
func determineShapefileGeometryType(shapeType shp.ShapeType) string {
	switch shapeType {
	case shp.POINT, shp.POINTZ, shp.POINTM:
		return "Point"
	case shp.POLYLINE, shp.POLYLINEZ, shp.POLYLINEM:
		return "LineString"
	case shp.POLYGON, shp.POLYGONZ, shp.POLYGONM:
		return "Polygon"
	case shp.MULTIPOINT, shp.MULTIPOINTZ, shp.MULTIPOINTM:
		return "MultiPoint"
	default:
		return "Geometry"
	}
}

// parseSRID 从空间参考系统字符串中解析 SRID
// 例如: "EPSG:4326" -> 4326
func parseSRID(srsStr string) int {
	if srsStr == "" {
		return 0
	}

	// 处理 "EPSG:xxxx" 格式
	if strings.HasPrefix(strings.ToUpper(srsStr), "EPSG:") {
		sridStr := strings.TrimPrefix(strings.ToUpper(srsStr), "EPSG:")
		if srid, err := strconv.Atoi(sridStr); err == nil {
			return srid
		}
	}

	return 0
}

func init() {
	parser := NewParser(nil)
	// 注册为 FileTableParser
	_ = format.RegisterFileTableParser(parser)
}
