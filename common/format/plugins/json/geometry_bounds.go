package jsonformat

import "encoding/json"

type geometryBounds struct {
	seen       bool
	minX, minY float64
	maxX, maxY float64
}

func (b *geometryBounds) AddGeometry(geom map[string]interface{}) {
	if geom == nil {
		return
	}
	if coordinates, ok := geom["coordinates"]; ok {
		b.addCoordinates(coordinates)
		return
	}
	if geometries, ok := geom["geometries"].([]interface{}); ok {
		for _, item := range geometries {
			if child, ok := item.(map[string]interface{}); ok {
				b.AddGeometry(child)
			}
		}
	}
}

func (b *geometryBounds) addCoordinates(value interface{}) {
	switch typed := value.(type) {
	case []interface{}:
		if len(typed) >= 2 && isNumberValue(typed[0]) && isNumberValue(typed[1]) {
			x, _ := numberValue(typed[0])
			y, _ := numberValue(typed[1])
			b.addPoint(x, y)
			return
		}
		for _, item := range typed {
			b.addCoordinates(item)
		}
	case []float64:
		if len(typed) >= 2 {
			b.addPoint(typed[0], typed[1])
		}
	}
}

func (b *geometryBounds) addPoint(x, y float64) {
	if !b.seen {
		b.seen = true
		b.minX, b.minY, b.maxX, b.maxY = x, y, x, y
		return
	}
	if x < b.minX {
		b.minX = x
	}
	if y < b.minY {
		b.minY = y
	}
	if x > b.maxX {
		b.maxX = x
	}
	if y > b.maxY {
		b.maxY = y
	}
}

func (b *geometryBounds) BoundingBox() ([4]float64, bool) {
	if !b.seen {
		return [4]float64{}, false
	}
	return [4]float64{b.minX, b.minY, b.maxX, b.maxY}, true
}

func isNumberValue(value interface{}) bool {
	_, ok := numberValue(value)
	return ok
}

func numberValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		v, err := typed.Float64()
		return v, err == nil
	default:
		return 0, false
	}
}

func intValue(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		v, err := typed.Int64()
		if err == nil {
			return int(v)
		}
	}
	return 0
}
