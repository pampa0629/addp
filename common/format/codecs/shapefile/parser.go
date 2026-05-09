package shapefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	commonSpatial "github.com/addp/common/spatial"
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

// ParseTableInfo 从 Shapefile 文件中提取 TableInfo
func (p *Parser) ParseTableInfo(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	opts := p.options
	if options != nil {
		opts = options
	}
	tempDir, cleanup, err := p.saveToTempFiles(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return p.parseTableInfoFromPath(filepath.Join(tempDir, "data.shp"), opts)
}

func (p *Parser) DescribeTableComponents(ctx context.Context, components resource.ComponentReader, options *format.ParseOptions) (*format.TableInfo, error) {
	_, basePath, cleanup, err := materializeComponents(ctx, components)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	opts := p.options
	if options != nil {
		opts = options
	}
	if opts.SpatialRefSys == "" {
		if prjBytes, readErr := os.ReadFile(basePath + ".prj"); readErr == nil {
			opts.SpatialRefSys = strings.TrimSpace(string(prjBytes))
		}
	}
	return p.parseTableInfoFromPath(basePath+".shp", opts)
}

// ReadPreview 读取 Shapefile 数据预览
func (p *Parser) ReadPreview(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	tempDir, cleanup, err := p.saveToTempFiles(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return p.readPreviewFromPath(ctx, filepath.Join(tempDir, "data.shp"), offset, limit)
}

func (p *Parser) SampleTableComponents(ctx context.Context, components resource.ComponentReader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	_, basePath, cleanup, err := materializeComponents(ctx, components)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return p.readPreviewFromPath(ctx, basePath+".shp", offset, limit)
}

func (p *Parser) parseTableInfoFromPath(shpPath string, opts *format.ParseOptions) (*format.TableInfo, error) {
	reader, err := Open(shpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	shpSchema := reader.GetSchema()
	geometryField := p.getGeometryFieldName()
	fields := make([]format.FieldInfo, 0, len(shpSchema)+1)
	fields = append(fields, format.FieldInfo{
		Name:         geometryField,
		Type:         format.FieldTypeGeometry,
		OriginalType: "Geometry",
		Nullable:     false,
		IsPrimaryKey: false,
		Comment:      "Shapefile geometry field",
	})
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

	recordCount := int64(0)
	for reader.Next() {
		recordCount++
	}

	reader2, err := Open(shpPath)
	if err != nil {
		return &format.TableInfo{
			Name:       "shapefile_data",
			RowCount:   &recordCount,
			Fields:     fields,
			PrimaryKey: []string{},
			Extensions: []format.ExtensionInfo{},
		}, nil
	}
	defer reader2.Close()

	geomType := determineShapefileGeometryType(reader2.GeometryType)
	if geomType == "Geometry" && reader2.Next() {
		_, shape := reader2.Shape()
		geomType = determineShapeGeometryType(shape)
	}

	spatialInfo := &format.SpatialInfo{
		GeometryColumn: geometryField,
		GeometryType:   geomType,
		SRID:           0,
		Dimension:      2,
	}
	if opts != nil && opts.SpatialRefSys != "" {
		if srid := commonSpatial.ParseSRID(opts.SpatialRefSys); srid > 0 {
			spatialInfo.SRID = srid
		}
	}

	shapefileInfo := &format.ShapefileInfo{
		Encoding:   "",
		ShapeType:  geomType,
		HasPRJ:     fileExists(strings.TrimSuffix(shpPath, filepath.Ext(shpPath)) + ".prj"),
		HasCPG:     fileExists(strings.TrimSuffix(shpPath, filepath.Ext(shpPath)) + ".cpg"),
		DBFVersion: 0,
	}
	if opts != nil {
		shapefileInfo.Encoding = opts.Encoding
	}

	return &format.TableInfo{
		Name:       "shapefile_data",
		RowCount:   &recordCount,
		Fields:     fields,
		PrimaryKey: []string{},
		Extensions: []format.ExtensionInfo{spatialInfo, shapefileInfo},
	}, nil
}

func (p *Parser) readPreviewFromPath(ctx context.Context, shpPath string, offset, limit int64) ([]map[string]interface{}, error) {
	reader, err := Open(shpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	fields := reader.Fields()
	geometryField := p.getGeometryFieldName()
	currentRecord := int64(0)
	for currentRecord < offset && reader.Next() {
		currentRecord++
	}

	maxRows := limit
	if limit < 0 {
		maxRows = 1<<31 - 1
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
		record := make(map[string]interface{}, len(fields)+1)
		for i, field := range fields {
			fieldName := TrimDBFFieldName(field.Name)
			rawValue := strings.TrimSpace(reader.ReadAttribute(recordIndex, i))
			if rawValue == "" {
				record[fieldName] = nil
			} else {
				record[fieldName] = ParseDBFAttribute(field.Fieldtype, rawValue)
			}
		}
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

func materializeComponents(ctx context.Context, components resource.ComponentReader) (tempDir string, basePath string, cleanup func(), err error) {
	tempDir, err = os.MkdirTemp("", "shapefile-components-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() {
		os.RemoveAll(tempDir)
	}

	var mainLocalPath string
	for _, component := range components.Components() {
		localPath := filepath.Join(tempDir, filepath.Base(component.Path))
		if err := materializeComponent(ctx, components, component, localPath); err != nil {
			if component.Required {
				cleanup()
				return "", "", nil, fmt.Errorf("failed to read required component %s: %w", component.Path, err)
			}
			continue
		}
		if component.ComponentRole == "main" {
			mainLocalPath = localPath
		}
	}
	if mainLocalPath == "" {
		cleanup()
		return "", "", nil, fmt.Errorf("main component missing")
	}
	return tempDir, strings.TrimSuffix(mainLocalPath, filepath.Ext(mainLocalPath)), cleanup, nil
}

func materializeComponent(ctx context.Context, components resource.ComponentReader, component resource.ComponentRef, destPath string) error {
	src, err := components.OpenComponent(ctx, component)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

func determineShapeGeometryType(shape shp.Shape) string {
	switch shape.(type) {
	case *shp.Point, *shp.PointZ, *shp.PointM:
		return "Point"
	case *shp.PolyLine, *shp.PolyLineZ, *shp.PolyLineM:
		return "LineString"
	case *shp.Polygon, *shp.PolygonZ, *shp.PolygonM:
		return "Polygon"
	case *shp.MultiPoint, *shp.MultiPointZ, *shp.MultiPointM:
		return "MultiPoint"
	default:
		return "Geometry"
	}
}

func init() {
	parser := NewParser(nil)
	_ = format.RegisterTableProvider(newTableProvider(parser))
}
