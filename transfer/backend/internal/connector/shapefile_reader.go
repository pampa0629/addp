package connector

import (
	"context"
	"fmt"
	"io"
	"time"
	"unsafe"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/jonas-p/go-shp"
)

// ShapefileReader Shapefile 数据读取器
// 读取 .shp/.shx/.dbf 文件组合
type ShapefileReader struct {
	filePath  string
	shape     *shp.Reader
	batchSize int
	offset    int64
	schema    *pipeline.Schema
	mode      pipeline.ReaderMode
}

// ShapefileConfig Shapefile 配置
type ShapefileConfig struct {
	FilePath     string `json:"file_path"`      // .shp 文件路径
	Encoding     string `json:"encoding"`       // DBF 编码 (默认 UTF-8)
	GeometryField string `json:"geometry_field"` // 几何字段名 (默认 "geom")
}

// NewShapefileReader 创建 Shapefile Reader
func NewShapefileReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	var shpConfig ShapefileConfig
	if err := mapToStruct(config.Config, &shpConfig); err != nil {
		return nil, fmt.Errorf("invalid shapefile config: %w", err)
	}

	if shpConfig.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	if shpConfig.GeometryField == "" {
		shpConfig.GeometryField = "geom"
	}

	return &ShapefileReader{
		filePath:  shpConfig.FilePath,
		batchSize: batchSize,
		mode:      pipeline.ModeBatch,
	}, nil
}

// Open 打开 Shapefile
func (r *ShapefileReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var shpConfig ShapefileConfig
	if err := mapToStruct(config.Config, &shpConfig); err != nil {
		return err
	}

	// 打开 Shapefile (自动查找 .shx 和 .dbf)
	shape, err := shp.Open(r.filePath)
	if err != nil {
		return fmt.Errorf("failed to open shapefile: %w", err)
	}

	r.shape = shape

	// 推断 schema
	schema, err := r.inferSchema(shpConfig.GeometryField)
	if err != nil {
		shape.Close()
		return fmt.Errorf("failed to infer schema: %w", err)
	}
	r.schema = schema

	return nil
}

// Read 读取一批数据
func (r *ShapefileReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	if r.shape == nil {
		return nil, fmt.Errorf("shapefile not opened")
	}

	var batchRows []map[string]interface{}
	count := 0

	// 读取批次数据
	fields := r.shape.Fields()
	rowNum := 0

	for r.shape.Next() && count < r.batchSize {
		_, shape := r.shape.Shape()

		// 读取属性字段
		row := make(map[string]interface{})

		// 从 DBF 读取属性
		for i, field := range fields {
			// Convert field name from [11]byte to string
			fieldName := string(field.Name[:])
			// Trim null bytes
			fieldName = string([]byte(fieldName)[:len(fieldName)])
			if idx := len(fieldName); idx > 0 {
				for j := 0; j < len(fieldName); j++ {
					if fieldName[j] == 0 {
						fieldName = fieldName[:j]
						break
					}
				}
			}

			// Read attribute value
			attrValue := r.shape.ReadAttribute(rowNum, i)
			row[fieldName] = attrValue
		}

		// 添加几何字段 (WKB 格式)
		wkb, err := shapeToWKB(shape)
		if err != nil {
			return nil, fmt.Errorf("failed to convert shape to WKB: %w", err)
		}
		row["geom"] = wkb

		batchRows = append(batchRows, row)
		count++
		r.offset++
		rowNum++
	}

	// 检查是否读完
	if len(batchRows) == 0 {
		return nil, io.EOF
	}

	return &pipeline.DataBatch{
		Rows:      batchRows,
		Schema:    r.schema,
		Offset:    r.offset,
		Timestamp: time.Now(),
	}, nil
}

// Schema 返回数据 schema
func (r *ShapefileReader) Schema() (*pipeline.Schema, error) {
	if r.schema == nil {
		return nil, fmt.Errorf("schema not initialized, call Open first")
	}
	return r.schema, nil
}

// SeekTo 跳转到指定偏移量
func (r *ShapefileReader) SeekTo(offset int64) error {
	// Shapefile 不支持随机访问，需要重新打开并跳过
	r.offset = offset
	// TODO: 实现跳过逻辑
	return nil
}

// Close 关闭连接
func (r *ShapefileReader) Close() error {
	if r.shape != nil {
		return r.shape.Close()
	}
	return nil
}

// Mode 返回读取模式
func (r *ShapefileReader) Mode() pipeline.ReaderMode {
	return r.mode
}

// inferSchema 推断数据 schema
func (r *ShapefileReader) inferSchema(geomFieldName string) (*pipeline.Schema, error) {
	if r.shape == nil {
		return nil, fmt.Errorf("shapefile not opened")
	}

	fields := make([]pipeline.Field, 0)

	// 添加 DBF 字段
	for _, field := range r.shape.Fields() {
		// Convert field name from [11]byte to string and trim null bytes
		fieldName := string(field.Name[:])
		for j := 0; j < len(fieldName); j++ {
			if fieldName[j] == 0 {
				fieldName = fieldName[:j]
				break
			}
		}

		fields = append(fields, pipeline.Field{
			Name:     fieldName,
			Type:     mapDBFType(field.Fieldtype),
			Nullable: true,
		})
	}

	// 添加几何字段
	spatialType := mapShapeType(r.shape.GeometryType)
	fields = append(fields, pipeline.Field{
		Name:        geomFieldName,
		Type:        "geometry",
		SpatialType: spatialType,
		Nullable:    false,
	})

	return &pipeline.Schema{
		Fields: fields,
		Metadata: map[string]interface{}{
			"source_type":   "shapefile",
			"geometry_type": r.shape.GeometryType,
			"bbox":          r.shape.BBox(),
		},
	}, nil
}

// mapDBFType 映射 DBF 类型到统一类型
func mapDBFType(dbfType byte) string {
	switch dbfType {
	case 'C': // Character
		return "string"
	case 'N': // Numeric
		return "float"
	case 'F': // Float
		return "float"
	case 'L': // Logical
		return "bool"
	case 'D': // Date
		return "datetime"
	case 'M': // Memo
		return "string"
	default:
		return "string"
	}
}

// mapShapeType 映射 Shapefile 几何类型到标准类型
func mapShapeType(shapeType shp.ShapeType) string {
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

// shapeToWKB 将 Shapefile 几何转为 WKB
func shapeToWKB(shape shp.Shape) ([]byte, error) {
	// TODO: 实现完整的 Shapefile → WKB 转换
	// 当前简化实现，返回空 WKB
	// 完整实现需要根据不同的 Shape 类型构建 WKB

	switch s := shape.(type) {
	case *shp.Point:
		return pointToWKB(s), nil
	case *shp.PolyLine:
		return polylineToWKB(s), nil
	case *shp.Polygon:
		return polygonToWKB(s), nil
	case *shp.MultiPoint:
		return multipointToWKB(s), nil
	default:
		return nil, fmt.Errorf("unsupported shape type: %T", shape)
	}
}

// pointToWKB 转换 Point 为 WKB
func pointToWKB(p *shp.Point) []byte {
	// WKB Point 格式:
	// - 1 byte: byte order (01 = little endian)
	// - 4 bytes: type (01 00 00 00 = Point)
	// - 8 bytes: X (double)
	// - 8 bytes: Y (double)

	wkb := make([]byte, 21)
	wkb[0] = 0x01 // Little endian

	// Type: Point (1)
	wkb[1] = 0x01
	wkb[2] = 0x00
	wkb[3] = 0x00
	wkb[4] = 0x00

	// X coordinate
	writeFloat64LE(wkb[5:13], p.X)

	// Y coordinate
	writeFloat64LE(wkb[13:21], p.Y)

	return wkb
}

// polylineToWKB 转换 PolyLine 为 WKB LineString
func polylineToWKB(pl *shp.PolyLine) []byte {
	if pl.NumParts == 0 || pl.NumPoints == 0 {
		return nil
	}

	// 简化：只取第一部分
	numPoints := int32(pl.NumPoints)

	// WKB LineString: 1 + 4 + 4 + (8*2*numPoints)
	wkbSize := 1 + 4 + 4 + (16 * numPoints)
	wkb := make([]byte, wkbSize)

	wkb[0] = 0x01 // Little endian

	// Type: LineString (2)
	wkb[1] = 0x02
	wkb[2] = 0x00
	wkb[3] = 0x00
	wkb[4] = 0x00

	// NumPoints
	writeUint32LE(wkb[5:9], uint32(numPoints))

	// Points
	offset := 9
	for i := 0; i < int(numPoints); i++ {
		writeFloat64LE(wkb[offset:offset+8], pl.Points[i].X)
		writeFloat64LE(wkb[offset+8:offset+16], pl.Points[i].Y)
		offset += 16
	}

	return wkb
}

// polygonToWKB 转换 Polygon 为 WKB
func polygonToWKB(pg *shp.Polygon) []byte {
	if pg.NumParts == 0 || pg.NumPoints == 0 {
		return nil
	}

	// 简化实现：只取第一个环
	numPoints := int32(pg.NumPoints)

	// WKB Polygon: 1 + 4 + 4 + 4 + (8*2*numPoints)
	wkbSize := 1 + 4 + 4 + 4 + (16 * numPoints)
	wkb := make([]byte, wkbSize)

	wkb[0] = 0x01 // Little endian

	// Type: Polygon (3)
	wkb[1] = 0x03
	wkb[2] = 0x00
	wkb[3] = 0x00
	wkb[4] = 0x00

	// NumRings (1)
	writeUint32LE(wkb[5:9], 1)

	// NumPoints in ring
	writeUint32LE(wkb[9:13], uint32(numPoints))

	// Points
	offset := 13
	for i := 0; i < int(numPoints); i++ {
		writeFloat64LE(wkb[offset:offset+8], pg.Points[i].X)
		writeFloat64LE(wkb[offset+8:offset+16], pg.Points[i].Y)
		offset += 16
	}

	return wkb
}

// multipointToWKB 转换 MultiPoint 为 WKB
func multipointToWKB(mp *shp.MultiPoint) []byte {
	numPoints := int32(mp.NumPoints)

	// WKB MultiPoint: 1 + 4 + 4 + (numPoints * 21)
	wkbSize := 1 + 4 + 4 + (int(numPoints) * 21)
	wkb := make([]byte, wkbSize)

	wkb[0] = 0x01 // Little endian

	// Type: MultiPoint (4)
	wkb[1] = 0x04
	wkb[2] = 0x00
	wkb[3] = 0x00
	wkb[4] = 0x00

	// NumPoints
	writeUint32LE(wkb[5:9], uint32(numPoints))

	// Points (each as WKB Point)
	offset := 9
	for i := 0; i < int(numPoints); i++ {
		// Byte order
		wkb[offset] = 0x01
		// Type: Point
		writeUint32LE(wkb[offset+1:offset+5], 1)
		// X, Y
		writeFloat64LE(wkb[offset+5:offset+13], mp.Points[i].X)
		writeFloat64LE(wkb[offset+13:offset+21], mp.Points[i].Y)
		offset += 21
	}

	return wkb
}

// 辅助函数：写入 little-endian float64
func writeFloat64LE(buf []byte, val float64) {
	bits := *(*uint64)(unsafe.Pointer(&val))
	buf[0] = byte(bits)
	buf[1] = byte(bits >> 8)
	buf[2] = byte(bits >> 16)
	buf[3] = byte(bits >> 24)
	buf[4] = byte(bits >> 32)
	buf[5] = byte(bits >> 40)
	buf[6] = byte(bits >> 48)
	buf[7] = byte(bits >> 56)
}

// 辅助函数：写入 little-endian uint32
func writeUint32LE(buf []byte, val uint32) {
	buf[0] = byte(val)
	buf[1] = byte(val >> 8)
	buf[2] = byte(val >> 16)
	buf[3] = byte(val >> 24)
}
