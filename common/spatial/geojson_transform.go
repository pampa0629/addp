package spatial

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

func normalizeGeoJSONPayload(payload interface{}) (interface{}, error) {
	switch v := payload.(type) {
	case nil:
		return nil, fmt.Errorf("geojson payload is nil")
	case string:
		var result interface{}
		if err := json.Unmarshal([]byte(v), &result); err != nil {
			return nil, err
		}
		return result, nil
	case []byte:
		var result interface{}
		if err := json.Unmarshal(v, &result); err != nil {
			return nil, err
		}
		return result, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var result interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}
		return result, nil
	}
}

func transformGeoJSONNode(node interface{}, transformGeometry func(map[string]interface{}) (map[string]interface{}, error)) (interface{}, error) {
	switch typed := node.(type) {
	case map[string]interface{}:
		nodeType, _ := typed["type"].(string)
		switch strings.ToLower(nodeType) {
		case "featurecollection":
			cloned := cloneMap(typed)
			rawFeatures, _ := typed["features"].([]interface{})
			features := make([]interface{}, 0, len(rawFeatures))
			for _, item := range rawFeatures {
				transformed, err := transformGeoJSONNode(item, transformGeometry)
				if err != nil {
					return nil, err
				}
				features = append(features, transformed)
			}
			cloned["features"] = features
			return cloned, nil
		case "feature":
			cloned := cloneMap(typed)
			if rawGeometry, exists := typed["geometry"]; exists && rawGeometry != nil {
				transformed, err := transformGeoJSONNode(rawGeometry, transformGeometry)
				if err != nil {
					return nil, err
				}
				cloned["geometry"] = transformed
			}
			return cloned, nil
		case "point", "multipoint", "linestring", "multilinestring", "polygon", "multipolygon", "geometrycollection":
			return transformGeometry(typed)
		default:
			return cloneMap(typed), nil
		}
	case []interface{}:
		items := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			transformed, err := transformGeoJSONNode(item, transformGeometry)
			if err != nil {
				return nil, err
			}
			items = append(items, transformed)
		}
		return items, nil
	default:
		return node, nil
	}
}

func extractGeoJSONBoundingBox(payload interface{}) []float64 {
	minX := math.Inf(1)
	minY := math.Inf(1)
	maxX := math.Inf(-1)
	maxY := math.Inf(-1)
	found := false

	var visitGeometry func(interface{})
	visitGeometry = func(node interface{}) {
		switch typed := node.(type) {
		case map[string]interface{}:
			nodeType, _ := typed["type"].(string)
			switch strings.ToLower(nodeType) {
			case "featurecollection":
				if features, ok := typed["features"].([]interface{}); ok {
					for _, item := range features {
						visitGeometry(item)
					}
				}
			case "feature":
				visitGeometry(typed["geometry"])
			case "geometrycollection":
				if geoms, ok := typed["geometries"].([]interface{}); ok {
					for _, item := range geoms {
						visitGeometry(item)
					}
				}
			case "point", "multipoint", "linestring", "multilinestring", "polygon", "multipolygon":
				visitCoordinates(typed["coordinates"], func(x, y float64) {
					found = true
					if x < minX {
						minX = x
					}
					if y < minY {
						minY = y
					}
					if x > maxX {
						maxX = x
					}
					if y > maxY {
						maxY = y
					}
				})
			}
		case []interface{}:
			for _, item := range typed {
				visitGeometry(item)
			}
		}
	}

	visitGeometry(payload)
	if !found {
		return nil
	}
	return []float64{minX, minY, maxX, maxY}
}

func visitCoordinates(node interface{}, visit func(x, y float64)) {
	values, ok := node.([]interface{})
	if !ok {
		return
	}

	if len(values) >= 2 {
		x, xOK := toFloat64(values[0])
		y, yOK := toFloat64(values[1])
		if xOK && yOK {
			visit(x, y)
			return
		}
	}

	for _, item := range values {
		visitCoordinates(item, visit)
	}
}

func toFloat64(v interface{}) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		f, err := value.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
