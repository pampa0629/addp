package shapefile

import (
	"fmt"
	"os"
	"strings"

	commonSpatial "github.com/addp/common/spatial"
	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
)

type writer struct {
	*shp.Writer
	filePath      string
	geometryField string
	fields        []shp.Field
	recordCount   int
}

func create(filename string, shapeType shp.ShapeType) (*writer, error) {
	shpWriter, err := shp.Create(filename, shapeType)
	if err != nil {
		return nil, err
	}

	return &writer{
		Writer:   shpWriter,
		filePath: filename,
	}, nil
}

func (w *writer) setFields(fields []shp.Field) error {
	if err := w.Writer.SetFields(fields); err != nil {
		return err
	}
	w.fields = fields

	// WORKAROUND: go-shp v0.1.1 bug - creates "filenamedbf" instead of "filename.dbf"
	w.fixDbfFilename()

	return nil
}

// fixDbfFilename fixes the DBF filename bug in go-shp library
func (w *writer) fixDbfFilename() {
	basePath := w.filePath
	if strings.HasSuffix(strings.ToLower(basePath), ".shp") {
		basePath = basePath[:len(basePath)-4]
	}

	wrongDbfPath := basePath + "dbf"    // Bug: missing dot
	correctDbfPath := basePath + ".dbf" // Correct filename

	if _, err := os.Stat(wrongDbfPath); err == nil {
		if err := os.Rename(wrongDbfPath, correctDbfPath); err == nil {
			// Silent fix
		}
	}
}

func toShapefileGeometry(geomValue interface{}) (shp.Shape, error) {
	geometry, err := commonSpatial.ParseGeometryValue(geomValue)
	if err != nil {
		return nil, err
	}
	return geomToShape(geometry)
}

func geomToShape(geometry geom.T) (shp.Shape, error) {
	switch g := geometry.(type) {
	case *geom.Point:
		return &shp.Point{X: g.X(), Y: g.Y()}, nil

	case *geom.LineString:
		return lineStringToShapefile(g)

	case *geom.MultiLineString:
		return multiLineStringToShapefile(g)

	case *geom.Polygon:
		return polygonToShapefile(g)

	case *geom.MultiPolygon:
		return multiPolygonToShapefile(g)

	case *geom.MultiPoint:
		numPoints := g.NumPoints()
		points := make([]shp.Point, numPoints)
		for i := 0; i < numPoints; i++ {
			p := g.Point(i)
			points[i] = shp.Point{X: p.X(), Y: p.Y()}
		}
		return &shp.MultiPoint{
			Box:       calculateBox(points),
			NumPoints: int32(numPoints),
			Points:    points,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported geometry type: %T", geometry)
	}
}

func lineStringToShapefile(line *geom.LineString) (*shp.PolyLine, error) {
	if line.NumCoords() < 2 {
		return nil, fmt.Errorf("linestring requires at least two points")
	}

	points := coordsToShpPoints(line.Coords())
	return &shp.PolyLine{
		Box:       calculateBox(points),
		NumParts:  1,
		NumPoints: int32(len(points)),
		Parts:     []int32{0},
		Points:    points,
	}, nil
}

func multiLineStringToShapefile(multiline *geom.MultiLineString) (*shp.PolyLine, error) {
	if multiline.NumLineStrings() == 0 {
		return nil, fmt.Errorf("multilinestring has no parts")
	}

	var allPoints []shp.Point
	parts := make([]int32, multiline.NumLineStrings())
	offset := 0

	for i := 0; i < multiline.NumLineStrings(); i++ {
		line := multiline.LineString(i)
		if line.NumCoords() < 2 {
			return nil, fmt.Errorf("linestring requires at least two points")
		}
		partPoints := coordsToShpPoints(line.Coords())
		parts[i] = int32(offset)
		allPoints = append(allPoints, partPoints...)
		offset += len(partPoints)
	}

	return &shp.PolyLine{
		Box:       calculateBox(allPoints),
		NumParts:  int32(len(parts)),
		NumPoints: int32(len(allPoints)),
		Parts:     parts,
		Points:    allPoints,
	}, nil
}

func polygonToShapefile(polygon *geom.Polygon) (*shp.Polygon, error) {
	if polygon.NumLinearRings() == 0 {
		return nil, fmt.Errorf("polygon has no rings")
	}
	return buildShapefilePolygon([]*geom.Polygon{polygon})
}

func multiPolygonToShapefile(multi *geom.MultiPolygon) (*shp.Polygon, error) {
	if multi.NumPolygons() == 0 {
		return nil, fmt.Errorf("multipolygon has no polygons")
	}

	polygons := make([]*geom.Polygon, multi.NumPolygons())
	for i := 0; i < multi.NumPolygons(); i++ {
		polygons[i] = multi.Polygon(i)
	}
	return buildShapefilePolygon(polygons)
}

func buildShapefilePolygon(polygons []*geom.Polygon) (*shp.Polygon, error) {
	var allPoints []shp.Point
	var parts []int32
	offset := 0

	for _, polygon := range polygons {
		if polygon.NumLinearRings() == 0 {
			continue
		}

		for ringIdx := 0; ringIdx < polygon.NumLinearRings(); ringIdx++ {
			ring := polygon.LinearRing(ringIdx)
			coords := ring.Coords()
			closed := closeCoordsIfNeeded(coords)

			if ringIdx == 0 {
				closed = ensureClockwise(closed)
			} else {
				closed = ensureCounterClockwise(closed)
			}

			partPoints := coordsToShpPoints(closed)
			parts = append(parts, int32(offset))
			allPoints = append(allPoints, partPoints...)
			offset += len(partPoints)
		}
	}

	if len(allPoints) == 0 {
		return nil, fmt.Errorf("polygon contains no points")
	}

	return &shp.Polygon{
		Box:       calculateBox(allPoints),
		NumParts:  int32(len(parts)),
		NumPoints: int32(len(allPoints)),
		Parts:     parts,
		Points:    allPoints,
	}, nil
}

func coordsToShpPoints(coords []geom.Coord) []shp.Point {
	points := make([]shp.Point, len(coords))
	for i, c := range coords {
		points[i] = shp.Point{X: c.X(), Y: c.Y()}
	}
	return points
}

func closeCoordsIfNeeded(coords []geom.Coord) []geom.Coord {
	if len(coords) == 0 {
		return coords
	}
	first := coords[0]
	last := coords[len(coords)-1]
	if almostEqual(first.X(), last.X()) && almostEqual(first.Y(), last.Y()) {
		return coords
	}
	return append(coords, geom.Coord{first.X(), first.Y()})
}

func ensureClockwise(coords []geom.Coord) []geom.Coord {
	if ringArea(coords) <= 0 {
		return coords
	}
	return reverseCoords(coords)
}

func ensureCounterClockwise(coords []geom.Coord) []geom.Coord {
	if ringArea(coords) >= 0 {
		return coords
	}
	return reverseCoords(coords)
}

func reverseCoords(coords []geom.Coord) []geom.Coord {
	reversed := make([]geom.Coord, len(coords))
	for i := range coords {
		reversed[i] = coords[len(coords)-1-i]
	}
	return reversed
}

func ringArea(coords []geom.Coord) float64 {
	if len(coords) < 3 {
		return 0
	}
	sum := 0.0
	for i := 0; i < len(coords)-1; i++ {
		x1, y1 := coords[i].X(), coords[i].Y()
		x2, y2 := coords[i+1].X(), coords[i+1].Y()
		sum += (x1 * y2) - (x2 * y1)
	}
	return sum / 2
}

func calculateBox(points []shp.Point) shp.Box {
	if len(points) == 0 {
		return shp.Box{}
	}

	minX, minY := points[0].X, points[0].Y
	maxX, maxY := points[0].X, points[0].Y

	for _, p := range points {
		if p.X < minX {
			minX = p.X
		}
		if p.X > maxX {
			maxX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}

	return shp.Box{
		MinX: minX,
		MinY: minY,
		MaxX: maxX,
		MaxY: maxY,
	}
}

func findGeometryValue(row map[string]interface{}, field string) (interface{}, bool) {
	if value, ok := row[field]; ok {
		return value, true
	}

	lowerField := strings.ToLower(field)
	for key, value := range row {
		if strings.ToLower(key) == lowerField {
			return value, true
		}
	}
	return nil, false
}
