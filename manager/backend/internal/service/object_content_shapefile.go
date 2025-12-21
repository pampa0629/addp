package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/models"
	"github.com/jonas-p/go-shp"
)

const (
	defaultShapefilePreviewFeatures = 200
)

type shapefileContentHandler struct {
	baseContentHandler
	maxFeatures int
}

func (h *shapefileContentHandler) Handle(ctx context.Context, req *ObjectContentRequest, fetcher ObjectContentProvider) (*models.ObjectPreviewContent, bool, error) {
	// Shapefile 预览依赖多文件协同处理，普通 Handle 无法工作，这里给出提示
	return &models.ObjectPreviewContent{
		Kind: "shapefile",
		Text: "Shapefile 预览需要完整的 .shp/.shx/.dbf 文件组合，当前处理器未启用流式模式。",
	}, false, nil
}

func (h *shapefileContentHandler) HandleCompositeStream(ctx context.Context, req *ObjectContentRequest, baseStreamer ObjectStreamProvider, siblingProvider ObjectSiblingStreamProvider) (*models.ObjectPreviewContent, bool, error) {
	tmpDir, err := os.MkdirTemp("", "shapefile-preview-*")
	if err != nil {
		return nil, false, fmt.Errorf("创建 shapefile 临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	rawExt := filepath.Ext(req.Path)
	baseName := strings.TrimSuffix(filepath.Base(req.Path), rawExt)
	if baseName == "" {
		baseName = strings.TrimSuffix(req.Path, rawExt)
	}
	if baseName == "" {
		baseName = strings.TrimSuffix(filepath.Base(req.Path), req.Extension)
	}
	if baseName == "" {
		baseName = "shapefile"
	}

	shpPath := filepath.Join(tmpDir, baseName+".shp")
	if _, err := downloadObjectToFile(baseStreamer, shpPath); err != nil {
		return nil, false, fmt.Errorf("下载 shapefile 主文件失败: %w", err)
	}

	requiredExts := []string{".shx", ".dbf"}
	missing := make([]string, 0, len(requiredExts))
	for _, ext := range requiredExts {
		target := filepath.Join(tmpDir, baseName+ext)
		if err := downloadSiblingToFile(req.Path, ext, siblingProvider, target); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				missing = append(missing, ext)
				continue
			}
			return nil, false, fmt.Errorf("下载 shapefile 依赖文件失败 (%s): %w", ext, err)
		}
	}

	if len(missing) > 0 {
		return &models.ObjectPreviewContent{
			Kind: "shapefile",
			Text: fmt.Sprintf("Shapefile 缺少必要的配套文件: %s。请确认 .shp/.shx/.dbf 均已上传。", strings.Join(missing, ", ")),
			Metadata: map[string]interface{}{
				"missing_parts": missing,
			},
		}, false, nil
	}

	prjText, _ := downloadSiblingText(req.Path, ".prj", siblingProvider)
	cpgText, _ := downloadSiblingText(req.Path, ".cpg", siblingProvider)

	reader, err := shp.Open(shpPath)
	if err != nil {
		return nil, false, fmt.Errorf("打开 shapefile 失败: %w", err)
	}
	defer reader.Close()

	fields := reader.Fields()
	fieldsMeta := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.String())
		fieldsMeta = append(fieldsMeta, map[string]interface{}{
			"name":      name,
			"type":      decodeDBFFieldType(field.Fieldtype),
			"raw_type":  string(field.Fieldtype),
			"size":      int(field.Size),
			"precision": int(field.Precision),
		})
	}

	totalFeatures := reader.AttributeCount()
	bbox := reader.BBox()
	maxPreview := h.maxFeatures
	if maxPreview <= 0 {
		maxPreview = defaultShapefilePreviewFeatures
	}

	features := make([]map[string]interface{}, 0, maxPreview)
	for reader.Next() {
		if len(features) >= maxPreview {
			break
		}
		recordIndex, shape := reader.Shape()
		geometry, err := convertShapeToGeoJSON(shape)
		if err != nil {
			logger.L().Warn("Shapefile 预览: 几何转换失败", "path", req.Path, "error", err)
			continue
		}

		properties := make(map[string]interface{}, len(fields))
		for i, field := range fields {
			fieldName := strings.TrimSpace(field.String())
			rawValue := strings.TrimSpace(reader.ReadAttribute(recordIndex, i))
			if rawValue == "" {
				properties[fieldName] = nil
				continue
			}
			properties[fieldName] = parseDBFAttribute(field.Fieldtype, rawValue)
		}

		features = append(features, map[string]interface{}{
			"type":       "Feature",
			"geometry":   geometry,
			"properties": properties,
		})
	}

	if err := reader.Err(); err != nil {
		return nil, false, fmt.Errorf("读取 shapefile 数据失败: %w", err)
	}

	truncated := totalFeatures > len(features)
	metadata := map[string]interface{}{
		"geometry_type":         mapShapeTypeToString(reader.GeometryType),
		"feature_count":         totalFeatures,
		"preview_feature_count": len(features),
		"fields":                fieldsMeta,
		"bbox":                  []float64{bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY},
		"required_parts":        []string{".shp", ".shx", ".dbf"},
		"optional_parts":        []string{".prj", ".cpg"},
	}
	if prjText != "" {
		metadata["projection_wkt"] = prjText
	}
	if cpgText != "" {
		metadata["code_page"] = cpgText
	}

	geojson := map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
	}

	content := &models.ObjectPreviewContent{
		Kind:      "shapefile",
		GeoJSON:   geojson,
		Metadata:  metadata,
		Truncated: truncated,
	}

	return content, truncated, nil
}

func downloadObjectToFile(opener func() (io.ReadCloser, error), target string) (int64, error) {
	if opener == nil {
		return 0, fmt.Errorf("对象读取器未就绪")
	}
	reader, err := opener()
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	file, err := os.Create(target)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	written, err := io.Copy(file, reader)
	if err != nil {
		return 0, err
	}
	return written, nil
}

func downloadSiblingToFile(basePath, ext string, provider ObjectSiblingStreamProvider, target string) error {
	if provider == nil {
		return fs.ErrNotExist
	}

	candidates := candidateSiblingPaths(basePath, ext)
	for _, key := range candidates {
		reader, err := provider(key)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}

		file, err := os.Create(target)
		if err != nil {
			reader.Close()
			return err
		}

		_, copyErr := io.Copy(file, reader)
		closeErr := reader.Close()
		fileErr := file.Close()

		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if fileErr != nil {
			return fileErr
		}
		return nil
	}
	return fs.ErrNotExist
}

func downloadSiblingText(basePath, ext string, provider ObjectSiblingStreamProvider) (string, error) {
	if provider == nil {
		return "", fs.ErrNotExist
	}
	candidates := candidateSiblingPaths(basePath, ext)
	for _, key := range candidates {
		reader, err := provider(key)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", err
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return strings.TrimSpace(string(data)), nil
	}
	return "", fs.ErrNotExist
}

func candidateSiblingPaths(basePath, ext string) []string {
	cleanExt := ensureLeadingDot(ext)
	base := strings.TrimSuffix(basePath, filepath.Ext(basePath))

	candidates := []string{
		base + cleanExt,
		base + strings.ToLower(cleanExt),
		base + strings.ToUpper(cleanExt),
	}

	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, exists := seen[c]; exists {
			continue
		}
		seen[c] = struct{}{}
		unique = append(unique, c)
	}
	return unique
}

func ensureLeadingDot(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		return "." + ext
	}
	return ext
}

func decodeDBFFieldType(t byte) string {
	switch t {
	case 'C':
		return "character"
	case 'N':
		return "numeric"
	case 'F':
		return "float"
	case 'D':
		return "date"
	case 'L':
		return "logical"
	case 'M':
		return "memo"
	case 'B':
		return "binary"
	default:
		return strings.ToUpper(string(t))
	}
}

func parseDBFAttribute(t byte, raw string) interface{} {
	switch t {
	case 'N', 'F':
		if strings.Contains(raw, ".") {
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				return f
			}
		}
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
	case 'L':
		switch strings.ToUpper(raw) {
		case "T", "Y":
			return true
		case "F", "N":
			return false
		}
	case 'D':
		if len(raw) == 8 {
			if ts, err := time.Parse("20060102", raw); err == nil {
				return ts.Format(time.RFC3339)
			}
		}
	}
	return raw
}

func convertShapeToGeoJSON(shape shp.Shape) (map[string]interface{}, error) {
	switch g := shape.(type) {
	case *shp.Point:
		return geoJSONPoint(g.X, g.Y), nil
	case *shp.PointM:
		return geoJSONPoint(g.X, g.Y), nil
	case *shp.PointZ:
		return geoJSONPoint(g.X, g.Y, g.Z), nil
	case *shp.MultiPoint:
		return geoJSONMultiPoint(pointsToCoordinates(g.Points)), nil
	case *shp.MultiPointM:
		return geoJSONMultiPoint(pointsToCoordinates(g.Points)), nil
	case *shp.MultiPointZ:
		return geoJSONMultiPoint(pointsToCoordinates(g.Points)), nil
	case *shp.PolyLine:
		return geoJSONFromLineParts(g.Parts, g.Points), nil
	case *shp.PolyLineM:
		alias := shp.PolyLine{
			Box:       g.Box,
			NumParts:  g.NumParts,
			NumPoints: g.NumPoints,
			Parts:     g.Parts,
			Points:    g.Points,
		}
		return geoJSONFromLineParts(alias.Parts, alias.Points), nil
	case *shp.PolyLineZ:
		alias := shp.PolyLine{
			Box:       g.Box,
			NumParts:  g.NumParts,
			NumPoints: g.NumPoints,
			Parts:     g.Parts,
			Points:    g.Points,
		}
		return geoJSONFromLineParts(alias.Parts, alias.Points), nil
	case *shp.Polygon:
		return geoJSONFromPolygonParts(g.Parts, g.Points), nil
	case *shp.PolygonM:
		alias := shp.Polygon{
			Box:       g.Box,
			NumParts:  g.NumParts,
			NumPoints: g.NumPoints,
			Parts:     g.Parts,
			Points:    g.Points,
		}
		return geoJSONFromPolygonParts(alias.Parts, alias.Points), nil
	case *shp.PolygonZ:
		alias := shp.Polygon{
			Box:       g.Box,
			NumParts:  g.NumParts,
			NumPoints: g.NumPoints,
			Parts:     g.Parts,
			Points:    g.Points,
		}
		return geoJSONFromPolygonParts(alias.Parts, alias.Points), nil
	case *shp.MultiPatch:
		return nil, fmt.Errorf("MultiPatch 几何暂不支持预览")
	case *shp.Null:
		return map[string]interface{}{"type": "GeometryCollection", "geometries": []interface{}{}}, nil
	default:
		return nil, fmt.Errorf("暂不支持的 Shapefile 几何类型: %T", shape)
	}
}

func geoJSONPoint(coords ...float64) map[string]interface{} {
	switch len(coords) {
	case 0:
		return map[string]interface{}{"type": "Point", "coordinates": []float64{0, 0}}
	case 2:
		return map[string]interface{}{"type": "Point", "coordinates": []float64{coords[0], coords[1]}}
	default:
		return map[string]interface{}{"type": "Point", "coordinates": coords}
	}
}

func geoJSONMultiPoint(points [][]float64) map[string]interface{} {
	return map[string]interface{}{
		"type":        "MultiPoint",
		"coordinates": points,
	}
}

func geoJSONFromLineParts(parts []int32, points []shp.Point) map[string]interface{} {
	segments := splitParts(points, parts)
	if len(segments) == 0 {
		return map[string]interface{}{"type": "LineString", "coordinates": [][]float64{}}
	}
	if len(segments) == 1 {
		return map[string]interface{}{
			"type":        "LineString",
			"coordinates": pointsToCoordinates(segments[0]),
		}
	}
	multi := make([][][]float64, 0, len(segments))
	for _, segment := range segments {
		multi = append(multi, pointsToCoordinates(segment))
	}
	return map[string]interface{}{
		"type":        "MultiLineString",
		"coordinates": multi,
	}
}

func geoJSONFromPolygonParts(parts []int32, points []shp.Point) map[string]interface{} {
	rings := splitParts(points, parts)
	if len(rings) == 0 {
		return map[string]interface{}{"type": "Polygon", "coordinates": [][][]float64{}}
	}
	if len(rings) == 1 {
		return map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{pointsToCoordinates(rings[0])},
		}
	}
	multi := make([][][][]float64, 0, len(rings))
	for _, ring := range rings {
		multi = append(multi, [][][]float64{pointsToCoordinates(ring)})
	}
	return map[string]interface{}{
		"type":        "MultiPolygon",
		"coordinates": multi,
	}
}

func splitParts(points []shp.Point, parts []int32) [][]shp.Point {
	if len(parts) == 0 {
		return [][]shp.Point{points}
	}

	segments := make([][]shp.Point, 0, len(parts))
	for idx, start := range parts {
		begin := int(start)
		if begin < 0 || begin >= len(points) {
			continue
		}
		var end int
		if idx == len(parts)-1 {
			end = len(points)
		} else {
			next := int(parts[idx+1])
			if next <= begin {
				continue
			}
			end = next
		}
		if end > len(points) {
			end = len(points)
		}
		segment := make([]shp.Point, end-begin)
		copy(segment, points[begin:end])
		segments = append(segments, segment)
	}
	if len(segments) == 0 {
		return [][]shp.Point{points}
	}
	return segments
}

func pointsToCoordinates(points []shp.Point) [][]float64 {
	coords := make([][]float64, 0, len(points))
	for _, p := range points {
		coords = append(coords, []float64{p.X, p.Y})
	}
	return coords
}

func mapShapeTypeToString(shapeType shp.ShapeType) string {
	switch shapeType {
	case shp.NULL:
		return "Null"
	case shp.POINT:
		return "Point"
	case shp.POLYLINE:
		return "Polyline"
	case shp.POLYGON:
		return "Polygon"
	case shp.MULTIPOINT:
		return "MultiPoint"
	case shp.POINTZ:
		return "PointZ"
	case shp.POLYLINEZ:
		return "PolylineZ"
	case shp.POLYGONZ:
		return "PolygonZ"
	case shp.MULTIPOINTZ:
		return "MultiPointZ"
	case shp.POINTM:
		return "PointM"
	case shp.POLYLINEM:
		return "PolylineM"
	case shp.POLYGONM:
		return "PolygonM"
	case shp.MULTIPOINTM:
		return "MultiPointM"
	case shp.MULTIPATCH:
		return "MultiPatch"
	default:
		return fmt.Sprintf("Unknown(%d)", shapeType)
	}
}
