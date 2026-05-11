package shapefile

import (
	"fmt"

	"github.com/jonas-p/go-shp"
)

// ShapeToGeoJSON 将 shapefile 几何转换为 GeoJSON 格式
func ShapeToGeoJSON(shape shp.Shape) (map[string]interface{}, error) {
	switch g := shape.(type) {
	case *shp.Point:
		return geoJSONPoint(g.X, g.Y), nil
	case *shp.PointM:
		return geoJSONPoint(g.X, g.Y), nil
	case *shp.PointZ:
		return geoJSONPoint(g.X, g.Y, g.Z), nil
	case *shp.MultiPoint:
		return geoJSONMultiPoint(pointsToGeoJSONCoordinates(g.Points)), nil
	case *shp.MultiPointM:
		return geoJSONMultiPoint(pointsToGeoJSONCoordinates(g.Points)), nil
	case *shp.MultiPointZ:
		return geoJSONMultiPoint(pointsToGeoJSONCoordinates(g.Points)), nil
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
		return nil, fmt.Errorf("MultiPatch geometry is not supported for GeoJSON conversion")
	case *shp.Null:
		return map[string]interface{}{"type": "GeometryCollection", "geometries": []interface{}{}}, nil
	default:
		return nil, fmt.Errorf("unsupported Shapefile geometry type: %T", shape)
	}
}

// geoJSONPoint 构建 GeoJSON Point 几何
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

// geoJSONMultiPoint 构建 GeoJSON MultiPoint 几何
func geoJSONMultiPoint(points [][]float64) map[string]interface{} {
	return map[string]interface{}{
		"type":        "MultiPoint",
		"coordinates": points,
	}
}

// geoJSONFromLineParts 从 shapefile polyline parts 构建 GeoJSON LineString 或 MultiLineString
func geoJSONFromLineParts(parts []int32, points []shp.Point) map[string]interface{} {
	segments := splitGeoJSONParts(points, parts)
	if len(segments) == 0 {
		return map[string]interface{}{"type": "LineString", "coordinates": [][]float64{}}
	}
	if len(segments) == 1 {
		return map[string]interface{}{
			"type":        "LineString",
			"coordinates": pointsToGeoJSONCoordinates(segments[0]),
		}
	}
	multi := make([][][]float64, 0, len(segments))
	for _, segment := range segments {
		multi = append(multi, pointsToGeoJSONCoordinates(segment))
	}
	return map[string]interface{}{
		"type":        "MultiLineString",
		"coordinates": multi,
	}
}

// geoJSONFromPolygonParts 从 shapefile polygon parts 构建 GeoJSON Polygon 或 MultiPolygon
func geoJSONFromPolygonParts(parts []int32, points []shp.Point) map[string]interface{} {
	rings := splitGeoJSONParts(points, parts)
	if len(rings) == 0 {
		return map[string]interface{}{"type": "Polygon", "coordinates": [][][]float64{}}
	}
	if len(rings) == 1 {
		return map[string]interface{}{
			"type":        "Polygon",
			"coordinates": [][][]float64{pointsToGeoJSONCoordinates(rings[0])},
		}
	}
	multi := make([][][][]float64, 0, len(rings))
	for _, ring := range rings {
		multi = append(multi, [][][]float64{pointsToGeoJSONCoordinates(ring)})
	}
	return map[string]interface{}{
		"type":        "MultiPolygon",
		"coordinates": multi,
	}
}

// splitGeoJSONParts 将 shapefile points 按 parts 索引拆分为多个部分
func splitGeoJSONParts(points []shp.Point, parts []int32) [][]shp.Point {
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

// pointsToGeoJSONCoordinates 将 shapefile points 转换为 GeoJSON 坐标数组
func pointsToGeoJSONCoordinates(points []shp.Point) [][]float64 {
	coords := make([][]float64, 0, len(points))
	for _, p := range points {
		coords = append(coords, []float64{p.X, p.Y})
	}
	return coords
}
