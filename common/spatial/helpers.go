package spatial

import "github.com/twpayne/go-geom"

// GeometryTypeName returns the canonical OGC geometry type name.
func GeometryTypeName(geometry geom.T) string {
	switch geometry.(type) {
	case *geom.Point:
		return "Point"
	case *geom.LineString:
		return "LineString"
	case *geom.LinearRing:
		return "LinearRing"
	case *geom.Polygon:
		return "Polygon"
	case *geom.MultiPoint:
		return "MultiPoint"
	case *geom.MultiLineString:
		return "MultiLineString"
	case *geom.MultiPolygon:
		return "MultiPolygon"
	case *geom.GeometryCollection:
		return "GeometryCollection"
	default:
		return "Unknown"
	}
}

func cloneEndss(src [][]int) [][]int {
	if src == nil {
		return nil
	}
	dst := make([][]int, len(src))
	for i := range src {
		if src[i] == nil {
			continue
		}
		dst[i] = append([]int(nil), src[i]...)
	}
	return dst
}
