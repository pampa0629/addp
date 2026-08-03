package datatype

import "strings"

// GeometryType is ADDP's standard OGC geometry topology type.
type GeometryType string

const (
	GeometryTypeUnknown            GeometryType = ""
	GeometryTypeGeometry           GeometryType = "Geometry"
	GeometryTypePoint              GeometryType = "Point"
	GeometryTypeMultiPoint         GeometryType = "MultiPoint"
	GeometryTypeLineString         GeometryType = "LineString"
	GeometryTypeMultiLineString    GeometryType = "MultiLineString"
	GeometryTypePolygon            GeometryType = "Polygon"
	GeometryTypeMultiPolygon       GeometryType = "MultiPolygon"
	GeometryTypeGeometryCollection GeometryType = "GeometryCollection"
)

var knownGeometryTypes = map[GeometryType]struct{}{
	GeometryTypeGeometry:           {},
	GeometryTypePoint:              {},
	GeometryTypeMultiPoint:         {},
	GeometryTypeLineString:         {},
	GeometryTypeMultiLineString:    {},
	GeometryTypePolygon:            {},
	GeometryTypeMultiPolygon:       {},
	GeometryTypeGeometryCollection: {},
}

// ParseGeometryType normalizes a native geometry type name into ADDP's standard geometry type.
func ParseGeometryType(value string) GeometryType {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return GeometryTypeUnknown
	}
	lower := strings.ToLower(normalized)
	lower = strings.TrimPrefix(lower, "st_")
	lower = strings.TrimPrefix(lower, "st")
	lower = strings.ReplaceAll(lower, "_", "")
	lower = strings.ReplaceAll(lower, "-", "")
	lower = strings.ReplaceAll(lower, " ", "")
	for _, suffix := range []string{"zm", "z", "m"} {
		if strings.HasSuffix(lower, suffix) {
			lower = strings.TrimSuffix(lower, suffix)
			break
		}
	}
	switch lower {
	case "geometry":
		return GeometryTypeGeometry
	case "point":
		return GeometryTypePoint
	case "multipoint":
		return GeometryTypeMultiPoint
	case "linestring":
		return GeometryTypeLineString
	case "multilinestring":
		return GeometryTypeMultiLineString
	case "polygon":
		return GeometryTypePolygon
	case "multipolygon":
		return GeometryTypeMultiPolygon
	case "geometrycollection", "geomcollection":
		return GeometryTypeGeometryCollection
	default:
		return GeometryTypeUnknown
	}
}

// IsKnownGeometryType reports whether geometryType is one of ADDP's standard geometry types.
func IsKnownGeometryType(geometryType GeometryType) bool {
	_, ok := knownGeometryTypes[geometryType]
	return ok
}

// StandardGeometryType returns the canonical string for a native geometry type.
func StandardGeometryType(value string) string {
	geometryType := ParseGeometryType(value)
	if geometryType == GeometryTypeUnknown {
		return ""
	}
	return string(geometryType)
}
