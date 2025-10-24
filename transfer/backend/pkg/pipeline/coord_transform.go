package pipeline

import (
	"fmt"
	"math"

	"github.com/twpayne/go-geom"
)

// CoordTransformer 坐标转换器接口
type CoordTransformer interface {
	Transform(lon, lat float64) (x, y float64, err error)
}

// WGS84ToWebMercator WGS84 (EPSG:4326) 到 Web Mercator (EPSG:3857) 转换器
type WGS84ToWebMercator struct{}

// Transform 执行坐标转换
// Input: lon, lat in WGS84 (degrees)
// Output: x, y in Web Mercator (meters)
func (t *WGS84ToWebMercator) Transform(lon, lat float64) (x, y float64, err error) {
	// 验证输入范围
	if lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("longitude out of range: %f (must be between -180 and 180)", lon)
	}
	if lat < -85.05112878 || lat > 85.05112878 {
		return 0, 0, fmt.Errorf("latitude out of range: %f (must be between -85.05112878 and 85.05112878)", lat)
	}

	// Web Mercator 投影公式
	x = lon * 20037508.34 / 180.0
	y = math.Log(math.Tan((90.0+lat)*math.Pi/360.0)) / (math.Pi / 180.0)
	y = y * 20037508.34 / 180.0

	return x, y, nil
}

// WebMercatorToWGS84 Web Mercator (EPSG:3857) 到 WGS84 (EPSG:4326) 转换器
type WebMercatorToWGS84 struct{}

// Transform 执行坐标转换
// Input: x, y in Web Mercator (meters)
// Output: lon, lat in WGS84 (degrees)
func (t *WebMercatorToWGS84) Transform(x, y float64) (lon, lat float64, err error) {
	// Web Mercator 边界检查
	maxExtent := 20037508.34
	if x < -maxExtent || x > maxExtent {
		return 0, 0, fmt.Errorf("x coordinate out of range: %f", x)
	}
	if y < -maxExtent || y > maxExtent {
		return 0, 0, fmt.Errorf("y coordinate out of range: %f", y)
	}

	// Web Mercator 逆投影公式
	lon = (x / 20037508.34) * 180.0
	lat = (y / 20037508.34) * 180.0
	lat = 180.0 / math.Pi * (2.0*math.Atan(math.Exp(lat*math.Pi/180.0)) - math.Pi/2.0)

	return lon, lat, nil
}

// GetCoordTransformer 根据源 SRID 和目标 SRID 获取转换器
func GetCoordTransformer(sourceSRID, targetSRID int) (CoordTransformer, error) {
	key := fmt.Sprintf("%d->%d", sourceSRID, targetSRID)

	switch key {
	case "4326->3857":
		return &WGS84ToWebMercator{}, nil
	case "3857->4326":
		return &WebMercatorToWGS84{}, nil
	default:
		return nil, fmt.Errorf("unsupported coordinate transformation: EPSG:%d -> EPSG:%d", sourceSRID, targetSRID)
	}
}

// TransformGeometry 转换几何对象的坐标系
func TransformGeometry(g geom.T, transformer CoordTransformer) (geom.T, error) {
	switch geom := g.(type) {
	case *geom.Point:
		return transformPoint(geom, transformer)

	case *geom.LineString:
		return transformLineString(geom, transformer)

	case *geom.Polygon:
		return transformPolygon(geom, transformer)

	case *geom.MultiPoint:
		return transformMultiPoint(geom, transformer)

	case *geom.MultiLineString:
		return transformMultiLineString(geom, transformer)

	case *geom.MultiPolygon:
		return transformMultiPolygon(geom, transformer)

	case *geom.GeometryCollection:
		return transformGeometryCollection(geom, transformer)

	default:
		return nil, fmt.Errorf("unsupported geometry type: %T", g)
	}
}

// transformPoint 转换点坐标
func transformPoint(p *geom.Point, transformer CoordTransformer) (*geom.Point, error) {
	coords := p.Coords()
	x, y, err := transformer.Transform(coords.X(), coords.Y())
	if err != nil {
		return nil, fmt.Errorf("failed to transform point: %w", err)
	}

	return geom.NewPoint(p.Layout()).MustSetCoords(geom.Coord{x, y}), nil
}

// transformLineString 转换线串坐标
func transformLineString(ls *geom.LineString, transformer CoordTransformer) (*geom.LineString, error) {
	numCoords := ls.NumCoords()
	newCoords := make([]geom.Coord, numCoords)

	for i := 0; i < numCoords; i++ {
		coord := ls.Coord(i)
		x, y, err := transformer.Transform(coord.X(), coord.Y())
		if err != nil {
			return nil, fmt.Errorf("failed to transform coordinate %d: %w", i, err)
		}
		newCoords[i] = geom.Coord{x, y}
	}

	return geom.NewLineString(ls.Layout()).MustSetCoords(newCoords), nil
}

// transformPolygon 转换多边形坐标
func transformPolygon(poly *geom.Polygon, transformer CoordTransformer) (*geom.Polygon, error) {
	numRings := poly.NumLinearRings()
	newRings := make([][]geom.Coord, numRings)

	for i := 0; i < numRings; i++ {
		ring := poly.LinearRing(i)
		numCoords := ring.NumCoords()
		newCoords := make([]geom.Coord, numCoords)

		for j := 0; j < numCoords; j++ {
			coord := ring.Coord(j)
			x, y, err := transformer.Transform(coord.X(), coord.Y())
			if err != nil {
				return nil, fmt.Errorf("failed to transform ring %d, coordinate %d: %w", i, j, err)
			}
			newCoords[j] = geom.Coord{x, y}
		}

		newRings[i] = newCoords
	}

	return geom.NewPolygon(poly.Layout()).MustSetCoords(newRings), nil
}

// transformMultiPoint 转换多点坐标
func transformMultiPoint(mp *geom.MultiPoint, transformer CoordTransformer) (*geom.MultiPoint, error) {
	numPoints := mp.NumPoints()
	newCoords := make([]geom.Coord, numPoints)

	for i := 0; i < numPoints; i++ {
		point := mp.Point(i)
		coords := point.Coords()
		x, y, err := transformer.Transform(coords.X(), coords.Y())
		if err != nil {
			return nil, fmt.Errorf("failed to transform point %d: %w", i, err)
		}
		newCoords[i] = geom.Coord{x, y}
	}

	return geom.NewMultiPoint(mp.Layout()).MustSetCoords(newCoords), nil
}

// transformMultiLineString 转换多线串坐标
func transformMultiLineString(mls *geom.MultiLineString, transformer CoordTransformer) (*geom.MultiLineString, error) {
	numLineStrings := mls.NumLineStrings()
	newCoords := make([][]geom.Coord, numLineStrings)

	for i := 0; i < numLineStrings; i++ {
		ls := mls.LineString(i)
		numCoords := ls.NumCoords()
		lineCoords := make([]geom.Coord, numCoords)

		for j := 0; j < numCoords; j++ {
			coord := ls.Coord(j)
			x, y, err := transformer.Transform(coord.X(), coord.Y())
			if err != nil {
				return nil, fmt.Errorf("failed to transform linestring %d, coordinate %d: %w", i, j, err)
			}
			lineCoords[j] = geom.Coord{x, y}
		}

		newCoords[i] = lineCoords
	}

	return geom.NewMultiLineString(mls.Layout()).MustSetCoords(newCoords), nil
}

// transformMultiPolygon 转换多多边形坐标
func transformMultiPolygon(mpoly *geom.MultiPolygon, transformer CoordTransformer) (*geom.MultiPolygon, error) {
	numPolygons := mpoly.NumPolygons()
	newCoords := make([][][]geom.Coord, numPolygons)

	for i := 0; i < numPolygons; i++ {
		poly := mpoly.Polygon(i)
		numRings := poly.NumLinearRings()
		polyRings := make([][]geom.Coord, numRings)

		for j := 0; j < numRings; j++ {
			ring := poly.LinearRing(j)
			numCoords := ring.NumCoords()
			ringCoords := make([]geom.Coord, numCoords)

			for k := 0; k < numCoords; k++ {
				coord := ring.Coord(k)
				x, y, err := transformer.Transform(coord.X(), coord.Y())
				if err != nil {
					return nil, fmt.Errorf("failed to transform polygon %d, ring %d, coordinate %d: %w", i, j, k, err)
				}
				ringCoords[k] = geom.Coord{x, y}
			}

			polyRings[j] = ringCoords
		}

		newCoords[i] = polyRings
	}

	return geom.NewMultiPolygon(mpoly.Layout()).MustSetCoords(newCoords), nil
}

// transformGeometryCollection 转换几何集合
func transformGeometryCollection(gc *geom.GeometryCollection, transformer CoordTransformer) (*geom.GeometryCollection, error) {
	numGeoms := gc.NumGeoms()
	newGeoms := make([]geom.T, numGeoms)

	for i := 0; i < numGeoms; i++ {
		g := gc.Geom(i)
		transformedGeom, err := TransformGeometry(g, transformer)
		if err != nil {
			return nil, fmt.Errorf("failed to transform geometry %d in collection: %w", i, err)
		}
		newGeoms[i] = transformedGeom
	}

	return geom.NewGeometryCollection().MustPush(newGeoms...), nil
}
