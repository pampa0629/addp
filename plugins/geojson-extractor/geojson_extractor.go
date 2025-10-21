// Package geojsonextractor GeoJSON文件元数据提取器插件
package geojsonextractor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"

	sdk "github.com/addp/meta-extractor-sdk"
)

// init 函数：GeoSpatialMetadata已经在SDK中内置注册
func init() {
	// GeoSpatialMetadata已经在SDK的init()中注册
}

// GeoJSONExtractor GeoJSON文件的元数据提取器
type GeoJSONExtractor struct{}

// SupportedTypes 返回支持的MIME类型
func (e *GeoJSONExtractor) SupportedTypes() []string {
	return []string{
		"application/geo+json",
		"application/vnd.geo+json",
		"application/json", // GeoJSON也是JSON
	}
}

// Priority 返回优先级
func (e *GeoJSONExtractor) Priority() int {
	return 70
}

// Extract 提取GeoJSON文件元数据
func (e *GeoJSONExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
	// 1. 读取GeoJSON内容
	content, err := io.ReadAll(input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read GeoJSON: %w", err)
	}

	// 2. 解析GeoJSON
	var geoJSON map[string]interface{}
	if err := json.Unmarshal(content, &geoJSON); err != nil {
		return nil, fmt.Errorf("failed to parse GeoJSON: %w", err)
	}

	// 3. 验证是否为有效的GeoJSON
	geoType, ok := geoJSON["type"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid GeoJSON: missing type field")
	}

	// 4. 创建基础元数据
	metadata := sdk.NewMetadata(
		filepath.Base(input.ObjectKey),
		"GeoJSON File",
		input.Size,
	)

	metadata.BasicInfo.ContentType = input.ContentType
	metadata.BasicInfo.LastModified = input.LastModified
	metadata.BasicInfo.ETag = input.ETag

	// 5. 提取地理空间元数据
	geoMeta := e.extractGeoSpatialInfo(geoJSON, geoType)

	// 6. 添加类型化元数据
	metadata.AddTypedMetadata("geo_metadata", geoMeta)

	// 7. 添加其他属性
	metadata.CustomAttrs["geojson_type"] = geoType
	metadata.CustomAttrs["file_size"] = input.Size
	metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)

	if plain := buildGeoJSONPlainText(geoJSON, geoType); plain != "" {
		trimmed := truncateRunes(plain, 20000)
		metadata.CustomAttrs["plain_text"] = trimmed
		metadata.CustomAttrs["plain_text_preview"] = truncateRunes(trimmed, 400)
	}

	return metadata, nil
}

// formatFileSize 格式化文件大小为人类可读格式
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// extractGeoSpatialInfo 提取地理空间信息
func (e *GeoJSONExtractor) extractGeoSpatialInfo(geoJSON map[string]interface{}, geoType string) *sdk.GeoSpatialMetadata {
	meta := &sdk.GeoSpatialMetadata{
		CoordinateSystem: "EPSG:4326", // GeoJSON默认使用WGS84
		Dimensions:       2,           // 默认2D
		SpatialIndex:     false,
		Attributes:       []string{},
	}

	switch geoType {
	case "FeatureCollection":
		e.extractFeatureCollectionInfo(geoJSON, meta)
	case "Feature":
		e.extractFeatureInfo(geoJSON, meta)
	case "GeometryCollection":
		e.extractGeometryCollectionInfo(geoJSON, meta)
	default:
		// 单个几何对象
		meta.GeometryType = geoType
		meta.FeatureCount = 1
		if geom, ok := geoJSON["coordinates"]; ok {
			meta.BoundingBox = calculateBoundingBox(geom)
		}
	}

	return meta
}

// extractFeatureCollectionInfo 提取FeatureCollection信息
func (e *GeoJSONExtractor) extractFeatureCollectionInfo(geoJSON map[string]interface{}, meta *sdk.GeoSpatialMetadata) {
	features, ok := geoJSON["features"].([]interface{})
	if !ok {
		return
	}

	meta.FeatureCount = len(features)

	// 分析第一个feature获取几何类型和属性
	if len(features) > 0 {
		if firstFeature, ok := features[0].(map[string]interface{}); ok {
			// 获取几何类型
			if geometry, ok := firstFeature["geometry"].(map[string]interface{}); ok {
				if geomType, ok := geometry["type"].(string); ok {
					meta.GeometryType = geomType
				}
			}

			// 获取属性列表
			if properties, ok := firstFeature["properties"].(map[string]interface{}); ok {
				for key := range properties {
					meta.Attributes = append(meta.Attributes, key)
				}
			}
		}
	}

	// 计算边界框
	meta.BoundingBox = e.calculateFeatureCollectionBBox(features)
}

// extractFeatureInfo 提取Feature信息
func (e *GeoJSONExtractor) extractFeatureInfo(geoJSON map[string]interface{}, meta *sdk.GeoSpatialMetadata) {
	meta.FeatureCount = 1

	// 获取几何类型
	if geometry, ok := geoJSON["geometry"].(map[string]interface{}); ok {
		if geomType, ok := geometry["type"].(string); ok {
			meta.GeometryType = geomType
		}
		if coords, ok := geometry["coordinates"]; ok {
			meta.BoundingBox = calculateBoundingBox(coords)
		}
	}

	// 获取属性列表
	if properties, ok := geoJSON["properties"].(map[string]interface{}); ok {
		for key := range properties {
			meta.Attributes = append(meta.Attributes, key)
		}
	}
}

// extractGeometryCollectionInfo 提取GeometryCollection信息
func (e *GeoJSONExtractor) extractGeometryCollectionInfo(geoJSON map[string]interface{}, meta *sdk.GeoSpatialMetadata) {
	geometries, ok := geoJSON["geometries"].([]interface{})
	if !ok {
		return
	}

	meta.FeatureCount = len(geometries)
	meta.GeometryType = "Mixed"

	// 获取第一个几何类型
	if len(geometries) > 0 {
		if firstGeom, ok := geometries[0].(map[string]interface{}); ok {
			if geomType, ok := firstGeom["type"].(string); ok {
				meta.GeometryType = geomType
			}
		}
	}
}

// calculateFeatureCollectionBBox 计算FeatureCollection的边界框
func (e *GeoJSONExtractor) calculateFeatureCollectionBBox(features []interface{}) []float64 {
	if len(features) == 0 {
		return []float64{}
	}

	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64

	for _, feature := range features {
		if f, ok := feature.(map[string]interface{}); ok {
			if geometry, ok := f["geometry"].(map[string]interface{}); ok {
				if coords, ok := geometry["coordinates"]; ok {
					bbox := calculateBoundingBox(coords)
					if len(bbox) >= 4 {
						if bbox[0] < minX {
							minX = bbox[0]
						}
						if bbox[1] < minY {
							minY = bbox[1]
						}
						if bbox[2] > maxX {
							maxX = bbox[2]
						}
						if bbox[3] > maxY {
							maxY = bbox[3]
						}
					}
				}
			}
		}
	}

	if minX == math.MaxFloat64 {
		return []float64{}
	}

	return []float64{minX, minY, maxX, maxY}
}

// calculateBoundingBox 计算坐标的边界框
func calculateBoundingBox(coords interface{}) []float64 {
	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64

	var processCoords func(interface{})
	processCoords = func(c interface{}) {
		switch v := c.(type) {
		case []interface{}:
			if len(v) == 2 {
				// 这是一个坐标点 [lon, lat]
				if lon, ok := v[0].(float64); ok {
					if lat, ok := v[1].(float64); ok {
						if lon < minX {
							minX = lon
						}
						if lon > maxX {
							maxX = lon
						}
						if lat < minY {
							minY = lat
						}
						if lat > maxY {
							maxY = lat
						}
						return
					}
				}
			}
			// 递归处理嵌套数组
			for _, item := range v {
				processCoords(item)
			}
		}
	}

	processCoords(coords)

	if minX == math.MaxFloat64 {
		return []float64{}
	}

	return []float64{minX, minY, maxX, maxY}
}

func buildGeoJSONPlainText(geoJSON map[string]interface{}, geoType string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Type: %s\n", geoType))

	if bbox, ok := geoJSON["bbox"]; ok {
		builder.WriteString(fmt.Sprintf("BoundingBox: %v\n", bbox))
	}

	switch geoType {
	case "FeatureCollection":
		features, _ := geoJSON["features"].([]interface{})
		builder.WriteString(fmt.Sprintf("Features: %d\n", len(features)))
		limit := 5
		if len(features) < limit {
			limit = len(features)
		}
		for i := 0; i < limit; i++ {
			feature, ok := features[i].(map[string]interface{})
			if !ok {
				continue
			}
			builder.WriteString(fmt.Sprintf("Feature %d:\n", i+1))
			if geometry, ok := feature["geometry"].(map[string]interface{}); ok {
				if t, ok := geometry["type"].(string); ok {
					builder.WriteString(fmt.Sprintf("  Geometry: %s\n", t))
				}
			}
			if properties, ok := feature["properties"].(map[string]interface{}); ok {
				builder.WriteString("  Properties:\n")
				count := 0
				for key, value := range properties {
					builder.WriteString(fmt.Sprintf("    %s=%v\n", key, value))
					count++
					if count >= 8 {
						break
					}
				}
			}
		}
	case "Feature":
		if properties, ok := geoJSON["properties"].(map[string]interface{}); ok {
			builder.WriteString("Properties:\n")
			count := 0
			for key, value := range properties {
				builder.WriteString(fmt.Sprintf("  %s=%v\n", key, value))
				count++
				if count >= 12 {
					break
				}
			}
		}
	default:
		if coordinates, ok := geoJSON["coordinates"]; ok {
			builder.WriteString(fmt.Sprintf("Coordinates: %v\n", coordinates))
		}
	}

	return strings.TrimSpace(builder.String())
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// GetExtractor 返回提取器实例（供ADDP加载使用）
func GetExtractor() sdk.MetadataExtractor {
	return &GeoJSONExtractor{}
}
