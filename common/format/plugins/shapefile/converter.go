package shapefile

import (
	"fmt"

	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
)

func shapeToGeom(shape shp.Shape) (geom.T, error) {
	switch s := shape.(type) {
	case *shp.Point:
		return geom.NewPointFlat(geom.XY, []float64{s.X, s.Y}), nil
	case *shp.PointZ:
		return geom.NewPointFlat(geom.XY, []float64{s.X, s.Y}), nil
	case *shp.PointM:
		return geom.NewPointFlat(geom.XY, []float64{s.X, s.Y}), nil
	case *shp.MultiPoint:
		return multipointShapeToGeom(s.Points), nil
	case *shp.MultiPointZ:
		return multipointShapeToGeom(s.Points), nil
	case *shp.MultiPointM:
		return multipointShapeToGeom(s.Points), nil
	case *shp.PolyLine:
		return polylineShapeToGeom(s.Parts, s.Points)
	case *shp.PolyLineZ:
		return polylineShapeToGeom(s.Parts, s.Points)
	case *shp.PolyLineM:
		return polylineShapeToGeom(s.Parts, s.Points)
	case *shp.Polygon:
		return polygonShapeToGeom(s.Parts, s.Points)
	case *shp.PolygonZ:
		return polygonShapeToGeom(s.Parts, s.Points)
	case *shp.PolygonM:
		return polygonShapeToGeom(s.Parts, s.Points)
	default:
		return nil, fmt.Errorf("unsupported shape type: %T", shape)
	}
}

func polylineShapeToGeom(parts []int32, points []shp.Point) (geom.T, error) {
	lineParts := splitParts(parts, points)
	if len(lineParts) == 0 {
		return nil, fmt.Errorf("polyline has no points")
	}

	if len(lineParts) == 1 {
		line := geom.NewLineString(geom.XY)
		line.MustSetCoords(pointsToCoords(lineParts[0]))
		return line, nil
	}

	multi := geom.NewMultiLineString(geom.XY)
	coords := make([][]geom.Coord, len(lineParts))
	for i, pts := range lineParts {
		coords[i] = pointsToCoords(pts)
	}
	multi.MustSetCoords(coords)
	return multi, nil
}

func polygonShapeToGeom(parts []int32, points []shp.Point) (geom.T, error) {
	rings := splitParts(parts, points)
	if len(rings) == 0 {
		return nil, fmt.Errorf("polygon has no rings")
	}

	var polygons [][][]geom.Coord
	var current [][]geom.Coord

	for _, pts := range rings {
		closed := ensureRingClosed(pts)
		coords := pointsToCoords(closed)
		area := signedArea(coords)

		if area < 0 || len(current) == 0 {
			// New outer ring (Shapefile spec: clockwise, negative area)
			if len(current) > 0 {
				polygons = append(polygons, current)
			}
			current = [][]geom.Coord{coordsToCCW(coords)}
		} else {
			// Inner ring (counter-clockwise)
			if current == nil {
				current = [][]geom.Coord{coordsToCCW(coords)}
			} else {
				current = append(current, coordsToCW(coords))
			}
		}
	}

	if len(current) > 0 {
		polygons = append(polygons, current)
	}

	if len(polygons) == 1 {
		polygon := geom.NewPolygon(geom.XY)
		polygon.MustSetCoords(polygons[0])
		return polygon, nil
	}

	multi := geom.NewMultiPolygon(geom.XY)
	multi.MustSetCoords(polygons)
	return multi, nil
}

func multipointShapeToGeom(points []shp.Point) geom.T {
	if len(points) == 1 {
		return geom.NewPointFlat(geom.XY, []float64{points[0].X, points[0].Y})
	}

	multi := geom.NewMultiPoint(geom.XY)
	coords := make([]geom.Coord, len(points))
	for i, p := range points {
		coords[i] = geom.Coord{p.X, p.Y}
	}
	multi.MustSetCoords(coords)
	return multi
}

func splitParts(parts []int32, points []shp.Point) [][]shp.Point {
	if len(points) == 0 {
		return nil
	}
	result := make([][]shp.Point, 0, len(parts))
	for i, start := range parts {
		var end int
		if i == len(parts)-1 {
			end = len(points)
		} else {
			end = int(parts[i+1])
		}
		part := points[int(start):end]
		if len(part) > 0 {
			result = append(result, part)
		}
	}
	return result
}

func pointsToCoords(points []shp.Point) []geom.Coord {
	coords := make([]geom.Coord, len(points))
	for i, p := range points {
		coords[i] = geom.Coord{p.X, p.Y}
	}
	return coords
}

func ensureRingClosed(points []shp.Point) []shp.Point {
	if len(points) == 0 {
		return points
	}
	first := points[0]
	last := points[len(points)-1]
	if almostEqual(first.X, last.X) && almostEqual(first.Y, last.Y) {
		return points
	}
	return append(points, first)
}

func coordsToCW(coords []geom.Coord) []geom.Coord {
	if signedArea(coords) < 0 {
		return coords
	}
	return reverseCoords(coords)
}

func coordsToCCW(coords []geom.Coord) []geom.Coord {
	if signedArea(coords) > 0 {
		return coords
	}
	return reverseCoords(coords)
}

func signedArea(coords []geom.Coord) float64 {
	if len(coords) < 3 {
		return 0
	}
	area := 0.0
	for i := 0; i < len(coords)-1; i++ {
		x1, y1 := coords[i][0], coords[i][1]
		x2, y2 := coords[i+1][0], coords[i+1][1]
		area += (x1 * y2) - (x2 * y1)
	}
	return area / 2
}
