package shapefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// ParseSchema 解析 Shapefile Schema
// 注意：Shapefile 需要 .shp, .shx, .dbf 三个文件，input 应该是 .shp 文件路径
func (p *Parser) ParseSchema(ctx context.Context, input io.Reader) (*format.Schema, error) {
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
	schema := reader.GetSchema()

	// 转换为标准 Schema
	return p.convertToSchema(reader, schema)
}

// ReadRecords 读取 Shapefile 数据记录
func (p *Parser) ReadRecords(ctx context.Context, input io.Reader, offset, limit int64) ([]map[string]interface{}, error) {
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

// CountRecords 统计总记录数
func (p *Parser) CountRecords(ctx context.Context, input io.Reader) (int64, error) {
	tempDir, cleanup, err := p.saveToTempFiles(input)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	shpPath := filepath.Join(tempDir, "data.shp")
	reader, err := Open(shpPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	// 使用 Reader 的内部方法获取形状数量
	count := int64(0)
	for reader.Next() {
		count++
	}

	return count, nil
}

// ExtractMetadata 提取 Shapefile 元数据
func (p *Parser) ExtractMetadata(ctx context.Context, input io.Reader) (map[string]interface{}, error) {
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

	// 获取边界框
	bbox := reader.BBox()

	// 获取几何类型 - 从嵌入的 Reader 中获取
	shapeType := "Unknown"
	if reader.Reader != nil {
		// jonas-p/go-shp 没有直接的 ShapeType() 方法
		// 我们通过读取第一个 shape 来推断类型
		if reader.Next() {
			_, shape := reader.Shape()
			if shape != nil {
				shapeType = fmt.Sprintf("%T", shape)
			}
		}
		// 重新打开以继续读取
		reader.Close()
		reader, _ = Open(shpPath)
		defer reader.Close()
	}

	// 获取字段信息
	schema := reader.GetSchema()
	fieldNames := make([]string, 0, len(schema))
	fieldTypes := make(map[string]string)
	for _, field := range schema {
		fieldNames = append(fieldNames, field.Name)
		fieldTypes[field.Name] = field.Type
	}

	// 统计记录数
	recordCount := int64(0)
	for reader.Next() {
		recordCount++
	}

	// 构建元数据
	metadata := map[string]interface{}{
		"geometry_type": shapeType,
		"record_count":  recordCount,
		"field_count":   len(schema),
		"fields":        fieldNames,
		"field_types":   fieldTypes,
		"bounding_box": map[string]interface{}{
			"min_x": bbox.MinX,
			"min_y": bbox.MinY,
			"max_x": bbox.MaxX,
			"max_y": bbox.MaxY,
		},
	}

	// 添加空间参考系统信息（如果有 .prj 文件）
	prjPath := strings.TrimSuffix(shpPath, ".shp") + ".prj"
	if prjData, err := os.ReadFile(prjPath); err == nil {
		metadata["spatial_ref_sys"] = string(prjData)
	}

	return metadata, nil
}

// convertToSchema 将 Shapefile schema 转换为标准 Schema
func (p *Parser) convertToSchema(reader *Reader, shpSchema []FieldInfo) (*format.Schema, error) {
	geometryField := p.getGeometryFieldName()

	// 构建字段列表（几何字段 + 属性字段）
	fields := make([]format.Field, 0, len(shpSchema)+1)

	// 添加几何字段
	fields = append(fields, format.Field{
		Name:     geometryField,
		Type:     format.FieldTypeGeometry,
		Nullable: false,
	})

	// 添加属性字段
	for _, field := range shpSchema {
		fields = append(fields, format.Field{
			Name:     field.Name,
			Type:     mapShapefileTypeToFieldType(field.Type),
			Nullable: true,
		})
	}

	schema := &format.Schema{
		Fields:        fields,
		GeometryField: &geometryField,
	}

	// 统计记录数
	recordCount := int64(0)
	for reader.Next() {
		recordCount++
	}
	schema.RecordCount = &recordCount

	// 添加几何类型
	shapeType := "Unknown"
	schema.GeometryType = &shapeType

	// 添加空间参考系统（如果在 options 中指定）
	if p.options.SpatialRefSys != "" {
		schema.SpatialRefSys = &p.options.SpatialRefSys
	}

	return schema, nil
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

func init() {
	// 注册到全局 format registry
	_ = format.Register(NewParser(nil))
}
