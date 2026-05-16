package jsonformat

import (
	"strings"
)

func featureFromObjectRecord(raw map[string]interface{}) *Feature {
	if isGeoJSONFeatureObject(raw) {
		return featureFromGeoJSONFeature(raw)
	}
	props := normalizeProperties(raw)
	geometryKey, geometry := detectGeometryProperty(props)
	if geometryKey != "" {
		delete(props, geometryKey)
	}
	return &Feature{
		Geometry:      geometry,
		GeometryField: geometryKey,
		Properties:    props,
	}
}

func isGeoJSONFeatureObject(raw map[string]interface{}) bool {
	typeName, _ := raw["type"].(string)
	if !strings.EqualFold(strings.TrimSpace(typeName), "Feature") {
		return false
	}
	_, hasProperties := raw["properties"].(map[string]interface{})
	_, hasGeometry := raw["geometry"].(map[string]interface{})
	return hasProperties || hasGeometry
}

func featureFromGeoJSONFeature(raw map[string]interface{}) *Feature {
	props := map[string]interface{}{}
	if rawProps, ok := raw["properties"].(map[string]interface{}); ok {
		props = normalizeProperties(rawProps)
	}
	return &Feature{
		ID:            normalizeValue(raw["id"]),
		Geometry:      normalizeGeometry(interfaceMap(raw["geometry"])),
		GeometryField: defaultGeometryField,
		Properties:    props,
	}
}

type rawCRS struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

func (c rawCRS) Name() string {
	if c.Properties == nil {
		return ""
	}
	if name, ok := c.Properties["name"].(string); ok {
		return name
	}
	return ""
}

func normalizeProperties(props map[string]interface{}) map[string]interface{} {
	if props == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(props))
	for k, v := range props {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeGeometry(geom map[string]interface{}) map[string]interface{} {
	if geom == nil {
		return nil
	}
	out := make(map[string]interface{}, len(geom))
	for k, v := range geom {
		out[k] = normalizeValue(v)
	}
	return out
}

func interfaceMap(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	default:
		return nil
	}
}

func detectGeometryProperty(props map[string]interface{}) (string, map[string]interface{}) {
	for key, value := range props {
		if geom := geometryValue(value); geom != nil {
			return key, geom
		}
	}
	return "", nil
}

func geometryValue(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return normalizeGeoJSONGeometry(typed)
	case string:
		if geom, err := decodeWKBGeometry(typed); err == nil {
			return geom
		}
	}
	return nil
}

func normalizeGeoJSONGeometry(value map[string]interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	typeName, _ := value["type"].(string)
	if !isGeoJSONGeometryType(typeName) {
		return nil
	}
	if _, ok := value["coordinates"]; !ok && !strings.EqualFold(typeName, "GeometryCollection") {
		return nil
	}
	return normalizeGeometry(value)
}

func geometrySRID(geom map[string]interface{}) int {
	if geom == nil {
		return 0
	}
	return intValue(geom["srid"])
}

func isGeoJSONGeometryType(typeName string) bool {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "point", "linestring", "polygon", "multipoint", "multilinestring", "multipolygon", "geometrycollection":
		return true
	default:
		return false
	}
}
