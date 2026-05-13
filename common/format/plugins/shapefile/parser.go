package shapefile

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkt"
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
	opts := p.resolveOptions(options)
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

	opts := p.resolveOptions(options)
	if opts.SpatialRefSys == "" {
		if prjBytes, readErr := os.ReadFile(basePath + ".prj"); readErr == nil {
			opts.SpatialRefSys = strings.TrimSpace(string(prjBytes))
		}
	}
	if opts.Encoding == "" || NormalizeDBFEncoding(opts.Encoding) == "utf-8" {
		if cpgEncoding := readCPGEncoding(basePath); cpgEncoding != "" {
			opts.Encoding = cpgEncoding
		}
	}
	return p.describeTableInfoFromHeaders(basePath, components.Components(), opts)
}

// SampleTable 读取 Shapefile 表格样本。
func (p *Parser) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	tempDir, cleanup, err := p.saveToTempFiles(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return p.sampleTableFromPath(ctx, filepath.Join(tempDir, "data.shp"), offset, limit, p.resolveOptions(options))
}

func (p *Parser) SampleTableComponents(ctx context.Context, components resource.ComponentReader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	opts := p.resolveOptions(options)
	if rows, ok, err := p.sampleTableComponentsIndexed(ctx, components, offset, limit, opts); ok {
		if err == nil {
			return rows, nil
		}
		if !isIndexedSampleFallbackError(err) {
			return nil, err
		}
	}

	_, basePath, cleanup, err := materializeComponents(ctx, components)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if opts.Encoding == "" || NormalizeDBFEncoding(opts.Encoding) == "utf-8" {
		if cpgEncoding := readCPGEncoding(basePath); cpgEncoding != "" {
			opts.Encoding = cpgEncoding
		}
	}
	return p.sampleTableFromPath(ctx, basePath+".shp", offset, limit, opts)
}

func (p *Parser) parseTableInfoFromPath(shpPath string, opts *format.ParseOptions) (*format.TableInfo, error) {
	encodingName := ""
	if opts != nil {
		encodingName = opts.Encoding
	}
	reader, err := OpenWithEncoding(shpPath, encodingName)
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

func (p *Parser) describeTableInfoFromHeaders(basePath string, components []resource.ComponentRef, opts *format.ParseOptions) (*format.TableInfo, error) {
	shpHeader, err := readSHPHeader(basePath + ".shp")
	if err != nil {
		return nil, err
	}
	dbfHeader, err := readDBFHeader(basePath+".dbf", opts.Encoding)
	if err != nil {
		return nil, err
	}

	geometryField := p.getGeometryFieldName()
	fields := make([]format.FieldInfo, 0, len(dbfHeader.Fields)+1)
	geomType := determineShapefileGeometryType(shpHeader.ShapeType)
	fields = append(fields, format.FieldInfo{
		Name:         geometryField,
		Type:         format.FieldTypeGeometry,
		OriginalType: geomType,
		Nullable:     false,
		IsPrimaryKey: false,
		Comment:      "Shapefile geometry field",
	})
	mapper := format.GetTypeMapper("shapefile")
	for _, field := range dbfHeader.Fields {
		fieldType := format.FieldTypeUnknown
		if mapper != nil {
			fieldType = mapper.ToCommon(field.RawType)
		}
		fields = append(fields, format.FieldInfo{
			Name:         field.Name,
			Type:         fieldType,
			OriginalType: field.RawType,
			Nullable:     true,
			Size:         field.Size,
			Precision:    field.Precision,
		})
	}

	rowCount := int64(dbfHeader.RecordCount)
	spatialInfo := &format.SpatialInfo{
		GeometryColumn: geometryField,
		GeometryType:   geomType,
		SRID:           0,
		BoundingBox:    &shpHeader.BBox,
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
		HasPRJ:     fileExists(basePath + ".prj"),
		HasCPG:     fileExists(basePath + ".cpg"),
		DBFVersion: dbfHeader.Version,
	}
	if opts != nil {
		shapefileInfo.Encoding = NormalizeDBFEncoding(opts.Encoding)
	}

	return &format.TableInfo{
		Name:       "shapefile_data",
		RowCount:   &rowCount,
		Fields:     fields,
		PrimaryKey: []string{},
		Extensions: []format.ExtensionInfo{
			spatialInfo,
			shapefileInfo,
			&Info{
				BaseName:            filepath.Base(basePath),
				ComponentExtensions: componentExtensions(components),
				HasPRJ:              shapefileInfo.HasPRJ,
				HasCPG:              shapefileInfo.HasCPG,
				ShapeType:           geomType,
				DBFVersion:          dbfHeader.Version,
				Encoding:            shapefileInfo.Encoding,
			},
		},
	}, nil
}

type shpHeaderInfo struct {
	ShapeType shp.ShapeType
	BBox      [4]float64
}

func readSHPHeader(path string) (*shpHeaderInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(32, io.SeekStart); err != nil {
		return nil, err
	}
	var shapeType shp.ShapeType
	if err := binary.Read(file, binary.LittleEndian, &shapeType); err != nil {
		return nil, err
	}
	var bbox [4]float64
	for i := range bbox {
		if err := binary.Read(file, binary.LittleEndian, &bbox[i]); err != nil {
			return nil, err
		}
	}
	return &shpHeaderInfo{ShapeType: shapeType, BBox: bbox}, nil
}

type dbfHeaderInfo struct {
	Version      byte
	RecordCount  int32
	HeaderLength int
	RecordLength int
	Fields       []FieldInfo
}

func readDBFHeader(path string, encodingName string) (*dbfHeaderInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var header [32]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return nil, err
	}
	recordCount := int32(binary.LittleEndian.Uint32(header[4:8]))
	headerLength := int(binary.LittleEndian.Uint16(header[8:10]))
	if headerLength < 33 {
		return nil, fmt.Errorf("invalid DBF header length: %d", headerLength)
	}
	fieldCount := (headerLength - 33) / 32
	fields := make([]FieldInfo, 0, fieldCount)
	for i := 0; i < fieldCount; i++ {
		var desc [32]byte
		if _, err := io.ReadFull(file, desc[:]); err != nil {
			return nil, err
		}
		var name [11]byte
		copy(name[:], desc[0:11])
		fieldType := desc[11]
		fields = append(fields, FieldInfo{
			Name:      decodeDBFName(name, encodingName),
			Type:      DecodeDBFFieldType(fieldType),
			RawType:   string(fieldType),
			Size:      int(desc[16]),
			Precision: int(desc[17]),
		})
	}
	return &dbfHeaderInfo{
		Version:      header[0],
		RecordCount:  recordCount,
		HeaderLength: headerLength,
		RecordLength: int(binary.LittleEndian.Uint16(header[10:12])),
		Fields:       fields,
	}, nil
}

func componentExtensions(components []resource.ComponentRef) []string {
	seen := map[string]bool{}
	extensions := make([]string, 0, len(components))
	for _, component := range components {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(component.Path)), ".")
		if ext == "" || seen[ext] {
			continue
		}
		seen[ext] = true
		extensions = append(extensions, ext)
	}
	sort.Strings(extensions)
	return extensions
}

func (p *Parser) sampleTableFromPath(ctx context.Context, shpPath string, offset, limit int64, opts *format.ParseOptions) ([]map[string]interface{}, error) {
	opts = p.resolveOptions(opts)
	encodingName := ""
	if opts != nil {
		encodingName = opts.Encoding
	}
	if encodingName == "" || NormalizeDBFEncoding(encodingName) == "utf-8" {
		if cpgEncoding := readCPGEncoding(strings.TrimSuffix(shpPath, filepath.Ext(shpPath))); cpgEncoding != "" {
			encodingName = cpgEncoding
		}
	}
	reader, err := OpenWithEncoding(shpPath, encodingName)
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
			fieldName := reader.TrimDBFFieldName(field.Name)
			rawValue := strings.TrimSpace(reader.ReadAttributeDecoded(recordIndex, i))
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

func (p *Parser) resolveOptions(options *format.ParseOptions) *format.ParseOptions {
	if options != nil {
		copied := *options
		return &copied
	}
	if p != nil && p.options != nil {
		copied := *p.options
		return &copied
	}
	return format.DefaultParseOptions()
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
func geomToWKT(geometry geom.T) (string, error) {
	return wkt.Marshal(geometry)
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
	_ = format.RegisterFormatPlugin(newTableProvider(parser))
}
