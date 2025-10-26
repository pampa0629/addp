package connector

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkb"
	"github.com/twpayne/go-geom/encoding/wkbcommon"
	"github.com/twpayne/go-geom/encoding/wkt"
)

// ShapefileWriter Shapefile 数据写入器
// 写入 .shp/.shx/.dbf 文件组合
//
// 注意: go-shp v0.1.1 库存在 Bug,在 SetFields() 中创建 DBF 文件时
// 会生成 "filenamedbf" 而不是 "filename.dbf" (缺少点号)
// 本实现通过 fixDbfFilename() 方法自动修复此问题
type ShapefileWriter struct {
	filePath      string
	geometryField string
	shape         *shp.Writer
	fields        []shp.Field
	buffer        []map[string]interface{}
	batchSize     int
	shapeType     shp.ShapeType
	recordCount   int // Track record count for WriteAttribute
}

// ShapefileWriterConfig Shapefile Writer 配置
type ShapefileWriterConfig struct {
	FilePath      string `json:"file_path"`      // .shp 文件路径
	GeometryField string `json:"geometry_field"` // 几何字段名 (默认 "geom")
	ShapeType     string `json:"shape_type"`     // POINT, POLYLINE, POLYGON (可选，自动推断)
	Encoding      string `json:"encoding"`       // DBF 编码 (默认 UTF-8)
}

// NewShapefileWriter 创建 Shapefile Writer
func NewShapefileWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	var writerConfig ShapefileWriterConfig
	if err := mapToStruct(config.Config, &writerConfig); err != nil {
		return nil, fmt.Errorf("invalid shapefile config: %w", err)
	}

	if writerConfig.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	if writerConfig.GeometryField == "" {
		writerConfig.GeometryField = "geom"
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	// 解析 ShapeType
	var shapeType shp.ShapeType
	switch writerConfig.ShapeType {
	case "POINT":
		shapeType = shp.POINT
	case "POLYLINE", "LINESTRING":
		shapeType = shp.POLYLINE
	case "POLYGON":
		shapeType = shp.POLYGON
	case "MULTIPOINT":
		shapeType = shp.MULTIPOINT
	default:
		shapeType = shp.NULL // 自动推断
	}

	return &ShapefileWriter{
		filePath:      writerConfig.FilePath,
		geometryField: writerConfig.GeometryField,
		buffer:        make([]map[string]interface{}, 0, batchSize),
		batchSize:     batchSize,
		shapeType:     shapeType,
	}, nil
}

// Open 打开 Shapefile 写入
func (w *ShapefileWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	// 确保目录存在
	dir := filepath.Dir(w.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 暂不创建 Writer，等待第一批数据确定 schema
	return nil
}

// Write 写入数据批次
func (w *ShapefileWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if batch.IsEmpty() {
		return nil
	}

	// 第一次写入时初始化 Writer
	if w.shape == nil {
		if err := w.initializeWriter(batch); err != nil {
			return err
		}
	}

	// 添加到缓冲区
	w.buffer = append(w.buffer, batch.Rows...)

	// 缓冲区满时执行批量写入
	if len(w.buffer) >= w.batchSize {
		return w.flushBuffer()
	}

	return nil
}

// Flush 刷新缓冲区
func (w *ShapefileWriter) Flush(ctx context.Context) error {
	if len(w.buffer) > 0 {
		return w.flushBuffer()
	}
	return nil
}

// Close 关闭连接
func (w *ShapefileWriter) Close() error {
	// 刷新剩余数据
	if len(w.buffer) > 0 {
		if err := w.flushBuffer(); err != nil {
			return err
		}
	}

	if w.shape != nil {
		w.shape.Close() // Close() doesn't return an error
	}
	return nil
}

// initializeWriter 初始化 Shapefile Writer
func (w *ShapefileWriter) initializeWriter(batch *pipeline.DataBatch) error {
	// 从第一行数据推断 schema
	firstRow := batch.Rows[0]

	// 如果 geometry 字段大小写不一致，尝试匹配实际 key
	if matchedKey, ok := findGeometryKey(firstRow, w.geometryField); ok {
		w.geometryField = matchedKey
	} else {
		if detected, ok := detectGeometryField(firstRow); ok {
			fmt.Printf("[ShapefileWriter] geometry field '%s' not found, fallback to '%s'\n", w.geometryField, detected)
			w.geometryField = detected
		} else {
			fmt.Printf("[ShapefileWriter] geometry field '%s' not found; available fields: %v\n", w.geometryField, mapKeys(firstRow))
			return fmt.Errorf("geometry field '%s' not found", w.geometryField)
		}
	}

	// 构建 DBF 字段定义
	w.fields = make([]shp.Field, 0)
	for key, value := range firstRow {
		if strings.EqualFold(key, w.geometryField) {
			continue // 跳过几何字段
		}

		field := shp.Field{
			Name: stringToFieldName(truncateFieldName(key)),
		}

		// 根据值类型推断 DBF 类型
		switch value.(type) {
		case string:
			field.Fieldtype = 'C' // Character
			field.Size = 254
		case int, int32, int64:
			field.Fieldtype = 'N' // Numeric
			field.Size = 18
			field.Precision = 0
		case float32, float64:
			field.Fieldtype = 'N' // Numeric
			field.Size = 18
			field.Precision = 8
		case bool:
			field.Fieldtype = 'L' // Logical
			field.Size = 1
		default:
			field.Fieldtype = 'C' // 默认为字符串
			field.Size = 254
		}

		w.fields = append(w.fields, field)
	}

	// 推断 ShapeType（如果未指定）
	if w.shapeType == shp.NULL {
		geomValue, exists := findGeometryValue(firstRow, w.geometryField)
		if !exists {
			fmt.Printf("[ShapefileWriter] geometry field '%s' missing in first row; available fields: %v\n", w.geometryField, mapKeys(firstRow))
			return fmt.Errorf("geometry field '%s' not found", w.geometryField)
		}

		shapeType, err := inferShapeType(geomValue)
		if err != nil {
			return err
		}
		w.shapeType = shapeType
	}

	// 创建 Shapefile Writer
	shape, err := shp.Create(w.filePath, w.shapeType)
	if err != nil {
		return fmt.Errorf("failed to create shapefile: %w", err)
	}

	// 设置 DBF 字段
	if err := shape.SetFields(w.fields); err != nil {
		return fmt.Errorf("failed to set DBF fields: %w", err)
	}

	// WORKAROUND: go-shp v0.1.1 has a bug where it creates "filenamedbf" instead of "filename.dbf"
	// We need to rename the file if it was created incorrectly
	w.fixDbfFilename()

	w.shape = shape
	return nil
}

// fixDbfFilename fixes the DBF filename bug in go-shp library
// go-shp v0.1.1 creates "filenamedbf" instead of "filename.dbf" in SetFields()
func (w *ShapefileWriter) fixDbfFilename() {
	basePath := w.filePath
	if strings.HasSuffix(strings.ToLower(basePath), ".shp") {
		basePath = basePath[:len(basePath)-4]
	}

	wrongDbfPath := basePath + "dbf"    // Bug: missing dot
	correctDbfPath := basePath + ".dbf" // Correct filename

	// Check if the wrong file exists
	if _, err := os.Stat(wrongDbfPath); err == nil {
		// Wrong file exists, rename it to correct name
		if err := os.Rename(wrongDbfPath, correctDbfPath); err != nil {
			fmt.Printf("[ShapefileWriter] Warning: failed to rename DBF file: %v\n", err)
		} else {
			fmt.Printf("[ShapefileWriter] Fixed DBF filename: %s -> %s\n", wrongDbfPath, correctDbfPath)
		}
	}
}

// flushBuffer 刷新缓冲区到 Shapefile
func (w *ShapefileWriter) flushBuffer() error {
	if w.shape == nil {
		return fmt.Errorf("shapefile writer not initialized")
	}

	for _, row := range w.buffer {
		// 提取几何数据
		geomValue, exists := findGeometryValue(row, w.geometryField)
		if !exists {
			continue // 跳过缺失几何的行
		}

		// 转换为 Shapefile 几何
		shape, err := toShapefileGeometry(geomValue, w.shapeType)
		if err != nil {
			return fmt.Errorf("failed to convert geometry: %w", err)
		}

		// 写入几何
		w.shape.Write(shape)

		// 写入属性 (需要逐个字段写入)
		rowNum := w.recordCount
		for i, field := range w.fields {
			// Convert field name from [11]byte to string
			fieldName := string(field.Name[:])
			for j := 0; j < len(fieldName); j++ {
				if fieldName[j] == 0 {
					fieldName = fieldName[:j]
					break
				}
			}

			var val interface{}
			if v, ok := row[fieldName]; ok {
				val = v
			}
			w.shape.WriteAttribute(rowNum, i, val)
		}
		w.recordCount++
	}

	// 清空缓冲区
	w.buffer = w.buffer[:0]

	return nil
}

// truncateFieldName 截断字段名到 10 个字符（DBF 限制）
func truncateFieldName(name string) string {
	if len(name) > 10 {
		return name[:10]
	}
	return name
}

// stringToFieldName converts a string to [11]byte field name
func stringToFieldName(name string) [11]byte {
	var fieldName [11]byte
	copy(fieldName[:], name)
	return fieldName
}

// findGeometryKey 在行数据中寻找匹配的几何字段（忽略大小写）
func findGeometryKey(row map[string]interface{}, desired string) (string, bool) {
	if _, ok := row[desired]; ok {
		return desired, true
	}

	lowerDesired := strings.ToLower(desired)
	for key := range row {
		if strings.ToLower(key) == lowerDesired {
			return key, true
		}
	}

	return "", false
}

// findGeometryValue 返回几何字段值（忽略大小写）
func findGeometryValue(row map[string]interface{}, field string) (interface{}, bool) {
	if value, ok := row[field]; ok {
		return value, true
	}

	lowerField := strings.ToLower(field)
	for key, value := range row {
		if strings.ToLower(key) == lowerField {
			return value, true
		}
	}
	return nil, false
}

// detectGeometryField 尝试自动识别几何字段
func detectGeometryField(row map[string]interface{}) (string, bool) {
	for key, value := range row {
		switch v := value.(type) {
		case []byte:
			if len(v) > 0 {
				return key, true
			}
		case string:
			trimmed := strings.TrimSpace(strings.ToUpper(v))
			if strings.HasPrefix(trimmed, "POINT") ||
				strings.HasPrefix(trimmed, "LINESTRING") ||
				strings.HasPrefix(trimmed, "POLYGON") ||
				strings.HasPrefix(trimmed, "MULTIPOLYGON") ||
				strings.HasPrefix(trimmed, "MULTILINESTRING") ||
				strings.HasPrefix(trimmed, "MULTIPOINT") {
				return key, true
			}
		case map[string]interface{}:
			// GeoJSON 格式
			if geomType, ok := v["type"].(string); ok {
				switch strings.ToUpper(geomType) {
				case "POINT", "LINESTRING", "POLYGON", "MULTIPOLYGON", "MULTILINESTRING", "MULTIPOINT", "GEOMETRYCOLLECTION":
					return key, true
				}
			}
		}
	}
	return "", false
}

// mapKeys 返回 map 的键列表（用于调试日志）
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

// inferShapeType 从几何数据推断 ShapeType
func inferShapeType(geomValue interface{}) (shp.ShapeType, error) {
	// 尝试解析为 WKB
	var wkbData []byte

	switch v := geomValue.(type) {
	case []byte:
		wkbData = v
	case string:
		// 假设是 WKT，先转为 WKB
		geometry, err := wkt.Unmarshal(v)
		if err != nil {
			return shp.NULL, fmt.Errorf("invalid WKT: %w", err)
		}
		wkbData, err = wkb.Marshal(geometry, wkb.NDR)
		if err != nil {
			return shp.NULL, err
		}
	default:
		return shp.NULL, fmt.Errorf("unsupported geometry type: %T", geomValue)
	}

	// 解析 WKB 获取几何类型
	geometry, err := wkb.Unmarshal(wkbData)
	if err != nil {
		return shp.NULL, fmt.Errorf("failed to parse WKB: %w", err)
	}

	// 根据几何类型返回 ShapeType
	switch geometry.(type) {
	case *geom.Point:
		return shp.POINT, nil
	case *geom.LineString:
		return shp.POLYLINE, nil
	case *geom.Polygon:
		return shp.POLYGON, nil
	case *geom.MultiPoint:
		return shp.MULTIPOINT, nil
	case *geom.MultiLineString:
		return shp.POLYLINE, nil
	case *geom.MultiPolygon:
		return shp.POLYGON, nil
	default:
		return shp.NULL, fmt.Errorf("unsupported geometry type: %T", geometry)
	}
}

// toShapefileGeometry 转换为 Shapefile 几何
func toShapefileGeometry(geomValue interface{}, targetType shp.ShapeType) (shp.Shape, error) {
	// 解析几何对象
	var geometry geom.T

	switch v := geomValue.(type) {
	case []byte:
		var err error
		geometry, err = parseBinaryGeometry(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse WKB/EWKB: %w", err)
		}
	case string:
		data := v
		if len(data) > 2 && (strings.HasPrefix(data, "0x") || strings.HasPrefix(data, "0X")) {
			data = data[2:]
		}

		if decoded, err := hex.DecodeString(data); err == nil && len(decoded) > 0 {
			geometry, err = parseBinaryGeometry(decoded)
			if err != nil {
				return nil, fmt.Errorf("failed to parse hex WKB/EWKB: %w", err)
			}
			break
		}

		var err error
		geometry, err = wkt.Unmarshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse WKT: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported geometry value type: %T", geomValue)
	}

	// 转换为 Shapefile 几何
	switch g := geometry.(type) {
	case *geom.Point:
		return &shp.Point{
			X: g.X(),
			Y: g.Y(),
		}, nil

	case *geom.LineString:
		return lineStringToShapefile(g)

	case *geom.MultiLineString:
		return multiLineStringToShapefile(g)

	case *geom.Polygon:
		return polygonToShapefile(g)

	case *geom.MultiPolygon:
		return multiPolygonToShapefile(g)

	case *geom.MultiPoint:
		numPoints := g.NumPoints()
		points := make([]shp.Point, numPoints)
		for i := 0; i < numPoints; i++ {
			p := g.Point(i)
			points[i] = shp.Point{X: p.X(), Y: p.Y()}
		}
		return &shp.MultiPoint{
			Box:       calculateBox(points),
			NumPoints: int32(numPoints),
			Points:    points,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported geometry type: %T", geometry)
	}
}

func parseBinaryGeometry(data []byte) (geom.T, error) {
	geometry, err := wkb.Unmarshal(data)
	if err == nil {
		return geometry, nil
	}

	if shouldFallbackToEWKB(err) {
		return ewkb.Unmarshal(data)
	}
	return nil, err
}

func shouldFallbackToEWKB(err error) bool {
	var unknownType wkbcommon.ErrUnknownType
	if errors.As(err, &unknownType) {
		return true
	}

	var unsupportedType wkbcommon.ErrUnsupportedType
	if errors.As(err, &unsupportedType) {
		return true
	}

	return false
}

// calculateBox 计算边界框
func calculateBox(points []shp.Point) shp.Box {
	if len(points) == 0 {
		return shp.Box{}
	}

	minX, minY := points[0].X, points[0].Y
	maxX, maxY := points[0].X, points[0].Y

	for _, p := range points {
		if p.X < minX {
			minX = p.X
		}
		if p.X > maxX {
			maxX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}

	return shp.Box{
		MinX: minX,
		MinY: minY,
		MaxX: maxX,
		MaxY: maxY,
	}
}

func lineStringToShapefile(line *geom.LineString) (*shp.PolyLine, error) {
	if line.NumCoords() < 2 {
		return nil, fmt.Errorf("linestring requires at least two points")
	}

	points := coordsToShpPoints(line.Coords())
	return &shp.PolyLine{
		Box:       calculateBox(points),
		NumParts:  1,
		NumPoints: int32(len(points)),
		Parts:     []int32{0},
		Points:    points,
	}, nil
}

func multiLineStringToShapefile(multiline *geom.MultiLineString) (*shp.PolyLine, error) {
	if multiline.NumLineStrings() == 0 {
		return nil, fmt.Errorf("multilinestring has no parts")
	}

	var allPoints []shp.Point
	parts := make([]int32, multiline.NumLineStrings())
	offset := 0

	for i := 0; i < multiline.NumLineStrings(); i++ {
		line := multiline.LineString(i)
		if line.NumCoords() < 2 {
			return nil, fmt.Errorf("linestring requires at least two points")
		}
		partPoints := coordsToShpPoints(line.Coords())
		parts[i] = int32(offset)
		allPoints = append(allPoints, partPoints...)
		offset += len(partPoints)
	}

	return &shp.PolyLine{
		Box:       calculateBox(allPoints),
		NumParts:  int32(len(parts)),
		NumPoints: int32(len(allPoints)),
		Parts:     parts,
		Points:    allPoints,
	}, nil
}

func polygonToShapefile(polygon *geom.Polygon) (*shp.Polygon, error) {
	if polygon.NumLinearRings() == 0 {
		return nil, fmt.Errorf("polygon has no rings")
	}

	return buildShapefilePolygon([]*geom.Polygon{polygon})
}

func multiPolygonToShapefile(multi *geom.MultiPolygon) (*shp.Polygon, error) {
	if multi.NumPolygons() == 0 {
		return nil, fmt.Errorf("multipolygon has no polygons")
	}

	polygons := make([]*geom.Polygon, multi.NumPolygons())
	for i := 0; i < multi.NumPolygons(); i++ {
		polygons[i] = multi.Polygon(i)
	}
	return buildShapefilePolygon(polygons)
}

func buildShapefilePolygon(polygons []*geom.Polygon) (*shp.Polygon, error) {
	var allPoints []shp.Point
	var parts []int32
	offset := 0

	for _, polygon := range polygons {
		if polygon.NumLinearRings() == 0 {
			continue
		}

		for ringIdx := 0; ringIdx < polygon.NumLinearRings(); ringIdx++ {
			ring := polygon.LinearRing(ringIdx)
			coords := ring.Coords()
			closed := closeCoordsIfNeeded(coords)

			if ringIdx == 0 {
				closed = ensureClockwise(closed)
			} else {
				closed = ensureCounterClockwise(closed)
			}

			partPoints := coordsToShpPoints(closed)
			parts = append(parts, int32(offset))
			allPoints = append(allPoints, partPoints...)
			offset += len(partPoints)
		}
	}

	if len(allPoints) == 0 {
		return nil, fmt.Errorf("polygon contains no points")
	}

	return &shp.Polygon{
		Box:       calculateBox(allPoints),
		NumParts:  int32(len(parts)),
		NumPoints: int32(len(allPoints)),
		Parts:     parts,
		Points:    allPoints,
	}, nil
}

func coordsToShpPoints(coords []geom.Coord) []shp.Point {
	points := make([]shp.Point, len(coords))
	for i, c := range coords {
		points[i] = shp.Point{X: c.X(), Y: c.Y()}
	}
	return points
}

func closeCoordsIfNeeded(coords []geom.Coord) []geom.Coord {
	if len(coords) == 0 {
		return coords
	}
	first := coords[0]
	last := coords[len(coords)-1]
	if almostEqual(first.X(), last.X()) && almostEqual(first.Y(), last.Y()) {
		return coords
	}
	return append(coords, geom.Coord{first.X(), first.Y()})
}

func ensureClockwise(coords []geom.Coord) []geom.Coord {
	if ringArea(coords) <= 0 {
		return coords
	}
	reversed := make([]geom.Coord, len(coords))
	for i := range coords {
		reversed[i] = coords[len(coords)-1-i]
	}
	return reversed
}

func ensureCounterClockwise(coords []geom.Coord) []geom.Coord {
	if ringArea(coords) >= 0 {
		return coords
	}
	reversed := make([]geom.Coord, len(coords))
	for i := range coords {
		reversed[i] = coords[len(coords)-1-i]
	}
	return reversed
}

func ringArea(coords []geom.Coord) float64 {
	if len(coords) < 3 {
		return 0
	}
	sum := 0.0
	for i := 0; i < len(coords)-1; i++ {
		x1, y1 := coords[i].X(), coords[i].Y()
		x2, y2 := coords[i+1].X(), coords[i+1].Y()
		sum += (x1 * y2) - (x2 * y1)
	}
	return sum / 2
}
