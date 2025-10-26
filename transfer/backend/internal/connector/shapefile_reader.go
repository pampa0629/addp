package connector

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkb"
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
	FilePath      string `json:"file_path"`      // .shp 文件路径
	Encoding      string `json:"encoding"`       // DBF 编码 (默认 UTF-8)
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

	for r.shape.Next() && count < r.batchSize {
		recordIndex := int(r.offset)
		_, shape := r.shape.Shape()

		// 读取属性字段
		row := make(map[string]interface{})

		// 从 DBF 读取属性
		for i, field := range fields {
			// Convert field name from [11]byte to string
			fieldName := trimDBFFieldName(field.Name)

			// Read attribute value
			attrValue := r.shape.ReadAttribute(recordIndex, i)
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
	geometry, err := shapeToGeom(shape)
	if err != nil {
		return nil, err
	}
	return wkb.Marshal(geometry, wkb.NDR)
}

func shapeToGeom(shape shp.Shape) (geom.T, error) {
	switch s := shape.(type) {
	case *shp.Point:
		return geom.NewPointFlat(geom.XY, []float64{s.X, s.Y}), nil
	case *shp.PointZ:
		return geom.NewPointFlat(geom.XY, []float64{s.X, s.Y}), nil
	case *shp.PointM:
		return geom.NewPointFlat(geom.XY, []float64{s.X, s.Y}), nil
	case *shp.MultiPoint:
		return multipointShapeToGeom(s.Points), nil
	case *shp.MultiPointZ:
		return multipointShapeToGeom(s.Points), nil
	case *shp.MultiPointM:
		return multipointShapeToGeom(s.Points), nil
	case *shp.PolyLine:
		return polylineShapeToGeom(s.Parts, s.Points)
	case *shp.PolyLineZ:
		return polylineShapeToGeom(s.Parts, s.Points)
	case *shp.PolyLineM:
		return polylineShapeToGeom(s.Parts, s.Points)
	case *shp.Polygon:
		return polygonShapeToGeom(s.Parts, s.Points)
	case *shp.PolygonZ:
		return polygonShapeToGeom(s.Parts, s.Points)
	case *shp.PolygonM:
		return polygonShapeToGeom(s.Parts, s.Points)
	default:
		return nil, fmt.Errorf("unsupported shape type: %T", shape)
	}
}

func polylineShapeToGeom(parts []int32, points []shp.Point) (geom.T, error) {
	lineParts := splitParts(parts, points)
	if len(lineParts) == 0 {
		return nil, fmt.Errorf("polyline has no points")
	}

	if len(lineParts) == 1 {
		line := geom.NewLineString(geom.XY)
		line.MustSetCoords(pointsToCoords(lineParts[0]))
		return line, nil
	}

	multi := geom.NewMultiLineString(geom.XY)
	coords := make([][]geom.Coord, len(lineParts))
	for i, pts := range lineParts {
		coords[i] = pointsToCoords(pts)
	}
	multi.MustSetCoords(coords)
	return multi, nil
}

func polygonShapeToGeom(parts []int32, points []shp.Point) (geom.T, error) {
	rings := splitParts(parts, points)
	if len(rings) == 0 {
		return nil, fmt.Errorf("polygon has no rings")
	}

	var polygons [][][]geom.Coord
	var current [][]geom.Coord

	for _, pts := range rings {
		closed := ensureRingClosed(pts)
		coords := pointsToCoords(closed)
		area := signedArea(coords)

		if area < 0 || len(current) == 0 {
			// 新的外环（Shapefile 规范：外环顺时针，面积为负）
			if len(current) > 0 {
				polygons = append(polygons, current)
			}
			current = [][]geom.Coord{coordsToCCW(coords)}
		} else {
			// 内环（逆时针）
			if current == nil {
				current = [][]geom.Coord{coordsToCCW(coords)}
			} else {
				current = append(current, coordsToCW(coords))
			}
		}
	}

	if len(current) > 0 {
		polygons = append(polygons, current)
	}

	if len(polygons) == 1 {
		polygon := geom.NewPolygon(geom.XY)
		polygon.MustSetCoords(polygons[0])
		return polygon, nil
	}

	multi := geom.NewMultiPolygon(geom.XY)
	multi.MustSetCoords(polygons)
	return multi, nil
}

func multipointShapeToGeom(points []shp.Point) geom.T {
	if len(points) == 1 {
		return geom.NewPointFlat(geom.XY, []float64{points[0].X, points[0].Y})
	}

	multi := geom.NewMultiPoint(geom.XY)
	coords := make([]geom.Coord, len(points))
	for i, p := range points {
		coords[i] = geom.Coord{p.X, p.Y}
	}
	multi.MustSetCoords(coords)
	return multi
}

func splitParts(parts []int32, points []shp.Point) [][]shp.Point {
	if len(points) == 0 {
		return nil
	}
	result := make([][]shp.Point, 0, len(parts))
	for i, start := range parts {
		var end int
		if i == len(parts)-1 {
			end = len(points)
		} else {
			end = int(parts[i+1])
		}
		part := points[int(start):end]
		if len(part) > 0 {
			result = append(result, part)
		}
	}
	return result
}

func pointsToCoords(points []shp.Point) []geom.Coord {
	coords := make([]geom.Coord, len(points))
	for i, p := range points {
		coords[i] = geom.Coord{p.X, p.Y}
	}
	return coords
}

func ensureRingClosed(points []shp.Point) []shp.Point {
	if len(points) == 0 {
		return points
	}
	first := points[0]
	last := points[len(points)-1]
	if almostEqual(first.X, last.X) && almostEqual(first.Y, last.Y) {
		return points
	}
	return append(points, first)
}

func coordsToCW(coords []geom.Coord) []geom.Coord {
	if signedArea(coords) < 0 {
		return coords
	}
	reversed := make([]geom.Coord, len(coords))
	for i := range coords {
		reversed[i] = coords[len(coords)-1-i]
	}
	return reversed
}

func coordsToCCW(coords []geom.Coord) []geom.Coord {
	if signedArea(coords) > 0 {
		return coords
	}
	reversed := make([]geom.Coord, len(coords))
	for i := range coords {
		reversed[i] = coords[len(coords)-1-i]
	}
	return reversed
}

func signedArea(coords []geom.Coord) float64 {
	if len(coords) < 3 {
		return 0
	}
	area := 0.0
	for i := 0; i < len(coords)-1; i++ {
		x1, y1 := coords[i][0], coords[i][1]
		x2, y2 := coords[i+1][0], coords[i+1][1]
		area += (x1 * y2) - (x2 * y1)
	}
	return area / 2
}

func trimDBFFieldName(name [11]byte) string {
	raw := string(name[:])
	raw = strings.TrimRight(raw, "\x00")
	return strings.TrimSpace(raw)
}
