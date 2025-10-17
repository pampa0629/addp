package extractors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/addp/meta/internal/scanner"
)

// GeoJSONExtractor GeoJSON文件的元数据提取器
type GeoJSONExtractor struct{}

func (e *GeoJSONExtractor) SupportedTypes() []string {
	return []string{
		"application/geo+json",
		"application/vnd.geo+json",
	}
}

func (e *GeoJSONExtractor) Priority() int {
	return 100 // 高优先级（GeoJSON优先于普通JSON）
}

func (e *GeoJSONExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
	// 1. 读取文件内容
	content, err := io.ReadAll(input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read content: %w", err)
	}

	// 2. 解析JSON
	var geoData map[string]interface{}
	if err := json.Unmarshal(content, &geoData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// 3. 验证是否为有效的GeoJSON
	geoType, ok := geoData["type"].(string)
	if !ok {
		return nil, fmt.Errorf("not a valid GeoJSON: missing 'type' field")
	}

	// 4. 构建基础元数据
	metadata := &scanner.Metadata{
		BasicInfo: scanner.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "GeoJSON",
			Size:         input.Size,
			ContentType:  input.ContentType,
			Encoding:     "UTF-8",
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 5. 提取地理空间元数据
	geoMeta := &scanner.GeoMetadata{
		GeometryType: geoType,
	}

	// 提取坐标系统
	if crs, ok := geoData["crs"].(map[string]interface{}); ok {
		if props, ok := crs["properties"].(map[string]interface{}); ok {
			if name, ok := props["name"].(string); ok {
				geoMeta.CoordinateSystem = name
			}
		}
	} else {
		// 默认为WGS84
		geoMeta.CoordinateSystem = "EPSG:4326"
	}

	// 提取边界框
	if bbox, ok := geoData["bbox"].([]interface{}); ok {
		bboxFloat := make([]float64, 0, len(bbox))
		for _, v := range bbox {
			if f, ok := v.(float64); ok {
				bboxFloat = append(bboxFloat, f)
			}
		}
		geoMeta.BoundingBox = bboxFloat
	}

	// 6. 根据GeoJSON类型提取不同的信息
	switch geoType {
	case "FeatureCollection":
		e.extractFeatureCollection(geoData, metadata, geoMeta)
	case "Feature":
		e.extractFeature(geoData, metadata, geoMeta)
	case "Point", "LineString", "Polygon", "MultiPoint", "MultiLineString", "MultiPolygon":
		e.extractGeometry(geoType, geoData, metadata, geoMeta)
	}

	// 7. 将地理空间元数据添加到自定义属性
	metadata.CustomAttrs["geo_metadata"] = map[string]interface{}{
		"geometry_type":      geoMeta.GeometryType,
		"coordinate_system":  geoMeta.CoordinateSystem,
		"bounding_box":       geoMeta.BoundingBox,
		"feature_count":      geoMeta.FeatureCount,
		"dimensions":         geoMeta.Dimensions,
	}

	return metadata, nil
}

// extractFeatureCollection 提取FeatureCollection的元数据
func (e *GeoJSONExtractor) extractFeatureCollection(
	geoData map[string]interface{},
	metadata *scanner.Metadata,
	geoMeta *scanner.GeoMetadata,
) {
	features, ok := geoData["features"].([]interface{})
	if !ok || len(features) == 0 {
		geoMeta.FeatureCount = 0
		return
	}

	geoMeta.FeatureCount = len(features)

	// 从第一个feature提取属性schema
	firstFeature, ok := features[0].(map[string]interface{})
	if !ok {
		return
	}

	properties, ok := firstFeature["properties"].(map[string]interface{})
	if !ok {
		return
	}

	// 构建schema
	columns := make([]scanner.ColumnInfo, 0, len(properties))
	for key, value := range properties {
		columns = append(columns, scanner.ColumnInfo{
			Name:     key,
			Type:     inferType(value),
			Nullable: true,
			Example:  value,
		})
	}

	// 提取geometry类型
	if geom, ok := firstFeature["geometry"].(map[string]interface{}); ok {
		if geomType, ok := geom["type"].(string); ok {
			geoMeta.GeometryType = geomType
		}
	}

	// 构建SchemaInfo
	metadata.SchemaInfo = &scanner.SchemaMetadata{
		Columns:  columns,
		RowCount: int64(len(features)),
		Extra: map[string]interface{}{
			"geometry_column": "geometry",
		},
	}

	// 提取前5条作为样本数据
	sampleSize := 5
	if len(features) < sampleSize {
		sampleSize = len(features)
	}

	sampleData := make([]map[string]interface{}, 0, sampleSize)
	for i := 0; i < sampleSize; i++ {
		feature, ok := features[i].(map[string]interface{})
		if !ok {
			continue
		}
		props, ok := feature["properties"].(map[string]interface{})
		if !ok {
			continue
		}
		sampleData = append(sampleData, props)
	}
	metadata.SchemaInfo.SampleData = sampleData
}

// extractFeature 提取单个Feature的元数据
func (e *GeoJSONExtractor) extractFeature(
	geoData map[string]interface{},
	metadata *scanner.Metadata,
	geoMeta *scanner.GeoMetadata,
) {
	geoMeta.FeatureCount = 1

	// 提取geometry类型
	if geom, ok := geoData["geometry"].(map[string]interface{}); ok {
		if geomType, ok := geom["type"].(string); ok {
			geoMeta.GeometryType = geomType
		}
	}

	// 提取properties
	if properties, ok := geoData["properties"].(map[string]interface{}); ok {
		metadata.CustomAttrs["properties"] = properties
	}
}

// extractGeometry 提取纯几何对象的元数据
func (e *GeoJSONExtractor) extractGeometry(
	geomType string,
	geoData map[string]interface{},
	metadata *scanner.Metadata,
	geoMeta *scanner.GeoMetadata,
) {
	geoMeta.FeatureCount = 1
	geoMeta.GeometryType = geomType

	// 提取坐标维度
	if coords, ok := geoData["coordinates"].([]interface{}); ok {
		geoMeta.Dimensions = inferDimensions(coords)
	}
}

// inferType 推断数据类型
func inferType(value interface{}) string {
	if value == nil {
		return "null"
	}

	switch value.(type) {
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}

// inferDimensions 推断坐标维度
func inferDimensions(coords []interface{}) int {
	if len(coords) == 0 {
		return 0
	}

	// 递归查找最底层的坐标数组
	first := coords[0]
	switch v := first.(type) {
	case []interface{}:
		return inferDimensions(v)
	case float64:
		return len(coords) // 2D或3D
	default:
		return 0
	}
}
