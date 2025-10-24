package pipeline

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/geojson"
	"github.com/twpayne/go-geom/encoding/wkb"
	"github.com/twpayne/go-geom/encoding/wkt"
)

// SpatialFormat 空间数据格式
type SpatialFormat string

const (
	FormatWKB     SpatialFormat = "wkb"      // Well-Known Binary
	FormatWKT     SpatialFormat = "wkt"      // Well-Known Text
	FormatEWKB    SpatialFormat = "ewkb"     // Extended WKB (PostGIS)
	FormatEWKT    SpatialFormat = "ewkt"     // Extended WKT
	FormatGeoJSON SpatialFormat = "geojson"  // GeoJSON
	FormatHexWKB  SpatialFormat = "hexwkb"   // Hex-encoded WKB
)

// SpatialTransformConfig 空间转换配置
type SpatialTransformConfig struct {
	GeometryFields    []string      `json:"geometry_fields"`     // 空间字段名列表
	SourceFormat      SpatialFormat `json:"source_format"`       // 源格式
	TargetFormat      SpatialFormat `json:"target_format"`       // 目标格式
	SourceSRID        int           `json:"source_srid"`         // 源坐标系 (如 4326)
	TargetSRID        int           `json:"target_srid"`         // 目标坐标系 (如 3857)
	SimplifyTolerance float64       `json:"simplify_tolerance"`  // 简化容差
	ValidateGeometry  bool          `json:"validate_geometry"`   // 是否验证几何有效性
}

// SpatialTransform 空间数据转换器
type SpatialTransform struct {
	config SpatialTransformConfig
}

// NewSpatialTransform 创建空间转换器
func NewSpatialTransform(config map[string]interface{}) (Transform, error) {
	var cfg SpatialTransformConfig

	// 手动解析配置
	if fields, ok := config["geometry_fields"].([]interface{}); ok {
		cfg.GeometryFields = make([]string, len(fields))
		for i, f := range fields {
			if s, ok := f.(string); ok {
				cfg.GeometryFields[i] = s
			}
		}
	}

	if sf, ok := config["source_format"].(string); ok {
		cfg.SourceFormat = SpatialFormat(sf)
	} else {
		cfg.SourceFormat = FormatWKB // 默认值
	}

	if tf, ok := config["target_format"].(string); ok {
		cfg.TargetFormat = SpatialFormat(tf)
	} else {
		cfg.TargetFormat = FormatWKB // 默认值
	}

	if ssrid, ok := config["source_srid"].(float64); ok {
		cfg.SourceSRID = int(ssrid)
	}

	if tsrid, ok := config["target_srid"].(float64); ok {
		cfg.TargetSRID = int(tsrid)
	}

	if tol, ok := config["simplify_tolerance"].(float64); ok {
		cfg.SimplifyTolerance = tol
	}

	if val, ok := config["validate_geometry"].(bool); ok {
		cfg.ValidateGeometry = val
	}

	// 验证配置
	if len(cfg.GeometryFields) == 0 {
		return nil, fmt.Errorf("geometry_fields cannot be empty")
	}

	return &SpatialTransform{config: cfg}, nil
}

// Apply 应用空间数据转换
func (t *SpatialTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
	if batch == nil || len(batch.Rows) == 0 {
		return batch, nil
	}

	for rowIdx, row := range batch.Rows {
		for _, geomField := range t.config.GeometryFields {
			value, exists := row[geomField]
			if !exists || value == nil {
				continue
			}

			// 解析几何对象
			geometry, err := t.parseGeometry(value)
			if err != nil {
				return nil, fmt.Errorf("row %d, field %s: failed to parse geometry: %w", rowIdx, geomField, err)
			}

			// 坐标转换（如果需要）
			if t.config.SourceSRID != 0 && t.config.TargetSRID != 0 && t.config.SourceSRID != t.config.TargetSRID {
				geometry, err = t.transformCoordinates(geometry)
				if err != nil {
					return nil, fmt.Errorf("row %d, field %s: failed to transform coordinates: %w", rowIdx, geomField, err)
				}
			}

			// 几何简化（可选）
			if t.config.SimplifyTolerance > 0 {
				geometry = t.simplifyGeometry(geometry, t.config.SimplifyTolerance)
			}

			// 几何验证（可选）
			if t.config.ValidateGeometry {
				if !t.isValidGeometry(geometry) {
					return nil, fmt.Errorf("row %d, field %s: invalid geometry", rowIdx, geomField)
				}
			}

			// 序列化为目标格式
			converted, err := t.serializeGeometry(geometry)
			if err != nil {
				return nil, fmt.Errorf("row %d, field %s: failed to serialize geometry: %w", rowIdx, geomField, err)
			}

			row[geomField] = converted
		}
	}

	return batch, nil
}

// parseGeometry 解析几何对象
func (t *SpatialTransform) parseGeometry(value interface{}) (geom.T, error) {
	switch t.config.SourceFormat {
	case FormatWKB, FormatEWKB:
		// 处理 []byte 或 string
		var data []byte
		switch v := value.(type) {
		case []byte:
			data = v
		case string:
			// 可能是 hex encoded
			decoded, err := hex.DecodeString(v)
			if err != nil {
				// 不是 hex，可能是原始 bytes
				data = []byte(v)
			} else {
				data = decoded
			}
		default:
			return nil, fmt.Errorf("WKB must be []byte or string, got %T", value)
		}
		return wkb.Unmarshal(data)

	case FormatHexWKB:
		// Hex-encoded WKB
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("HexWKB must be string, got %T", value)
		}
		data, err := hex.DecodeString(str)
		if err != nil {
			return nil, fmt.Errorf("invalid hex WKB: %w", err)
		}
		return wkb.Unmarshal(data)

	case FormatWKT, FormatEWKT:
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("WKT must be string, got %T", value)
		}
		return wkt.Unmarshal(str)

	case FormatGeoJSON:
		// GeoJSON 可以是 string 或 map
		var jsonBytes []byte
		switch v := value.(type) {
		case string:
			jsonBytes = []byte(v)
		case map[string]interface{}:
			var err error
			jsonBytes, err = json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal GeoJSON map: %w", err)
			}
		default:
			return nil, fmt.Errorf("GeoJSON must be string or map, got %T", value)
		}

		// 尝试作为 Feature 解析
		var feature geojson.Feature
		if err := json.Unmarshal(jsonBytes, &feature); err == nil {
			if feature.Geometry != nil {
				return feature.Geometry, nil
			}
		}

		// 尝试作为 Geometry 直接解析
		var geometry geom.T
		if err := geojson.Unmarshal(jsonBytes, &geometry); err != nil {
			return nil, fmt.Errorf("invalid GeoJSON: %w", err)
		}
		return geometry, nil

	default:
		return nil, fmt.Errorf("unsupported source format: %s", t.config.SourceFormat)
	}
}

// serializeGeometry 序列化几何对象
func (t *SpatialTransform) serializeGeometry(geometry geom.T) (interface{}, error) {
	switch t.config.TargetFormat {
	case FormatWKB, FormatEWKB:
		data, err := wkb.Marshal(geometry, wkb.NDR)
		if err != nil {
			return nil, err
		}
		return data, nil

	case FormatHexWKB:
		data, err := wkb.Marshal(geometry, wkb.NDR)
		if err != nil {
			return nil, err
		}
		return hex.EncodeToString(data), nil

	case FormatWKT, FormatEWKT:
		data, err := wkt.Marshal(geometry)
		if err != nil {
			return nil, err
		}
		return data, nil

	case FormatGeoJSON:
		data, err := geojson.Marshal(geometry)
		if err != nil {
			return nil, err
		}

		// 返回 map 而不是 string
		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}
		return result, nil

	default:
		return nil, fmt.Errorf("unsupported target format: %s", t.config.TargetFormat)
	}
}

// transformCoordinates 坐标投影转换
// 当前实现支持简单的坐标系转换 (WGS84 ⟷ Web Mercator)
// 未来可以集成 PROJ 库以支持更多坐标系
func (t *SpatialTransform) transformCoordinates(geometry geom.T) (geom.T, error) {
	// 如果没有设置源和目标 SRID，或者两者相同，则直接返回
	if t.config.SourceSRID == 0 || t.config.TargetSRID == 0 || t.config.SourceSRID == t.config.TargetSRID {
		return geometry, nil
	}

	// 获取坐标转换器
	transformer, err := GetCoordTransformer(t.config.SourceSRID, t.config.TargetSRID)
	if err != nil {
		// 如果不支持该坐标转换，返回错误提示
		return nil, fmt.Errorf("coordinate transformation not supported: %w (hint: currently only WGS84(4326) ⟷ Web Mercator(3857) are supported. For other coordinate systems, use PostGIS ST_Transform or integrate PROJ library)", err)
	}

	// 执行坐标转换
	return TransformGeometry(geometry, transformer)
}

// simplifyGeometry 几何简化（Douglas-Peucker 算法）
func (t *SpatialTransform) simplifyGeometry(geometry geom.T, tolerance float64) geom.T {
	// TODO: 实现几何简化算法
	// 可选方案：
	// 1. 使用 github.com/paulmach/orb 的简化算法
	// 2. 自行实现 Douglas-Peucker 算法

	// 暂时返回原始几何对象
	return geometry
}

// isValidGeometry 验证几何有效性
func (t *SpatialTransform) isValidGeometry(geometry geom.T) bool {
	// 基本验证：检查是否为 nil
	if geometry == nil {
		return false
	}

	// TODO: 实现更严格的几何验证
	// 1. 检查坐标是否有效
	// 2. 检查拓扑是否正确（如 Polygon 不自交）
	// 3. 使用 GEOS 库进行完整验证

	return true
}

// Name 返回转换器名称
func (t *SpatialTransform) Name() string {
	return "SpatialTransform"
}

// 注册空间转换器到全局注册表
func init() {
	RegisterTransform("spatial", NewSpatialTransform, TransformCapability{
		Name:        "spatial",
		Description: "Transform spatial/geometry data between different formats (WKB, WKT, GeoJSON) and coordinate systems (SRID transformation)",
		SupportedTypes: []string{"geometry", "geography", "blob", "binary", "bytea"},
		ConfigSchema: map[string]interface{}{
			"type": "object",
			"required": []string{"geometry_fields"},
			"properties": map[string]interface{}{
				"geometry_fields": map[string]interface{}{
					"type":        "array",
					"description": "List of geometry field names to transform",
					"items": map[string]string{
						"type": "string",
					},
					"minItems": 1,
				},
				"source_format": map[string]interface{}{
					"type":        "string",
					"description": "Source geometry format",
					"enum":        []string{"wkb", "wkt", "ewkb", "ewkt", "geojson", "hexwkb"},
					"default":     "wkb",
				},
				"target_format": map[string]interface{}{
					"type":        "string",
					"description": "Target geometry format",
					"enum":        []string{"wkb", "wkt", "ewkb", "ewkt", "geojson", "hexwkb"},
					"default":     "wkb",
				},
				"source_srid": map[string]interface{}{
					"type":        "integer",
					"description": "Source Spatial Reference System ID (e.g., 4326 for WGS84)",
					"minimum":     0,
				},
				"target_srid": map[string]interface{}{
					"type":        "integer",
					"description": "Target Spatial Reference System ID (e.g., 3857 for Web Mercator)",
					"minimum":     0,
				},
				"simplify_tolerance": map[string]interface{}{
					"type":        "number",
					"description": "Tolerance for geometry simplification (Douglas-Peucker algorithm)",
					"minimum":     0,
				},
				"validate_geometry": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to validate geometry validity",
					"default":     false,
				},
			},
		},
		Version: "1.0.0",
		Author:  "ADDP Transfer Module",
	})
}
