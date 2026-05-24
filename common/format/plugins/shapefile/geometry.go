package shapefile

import (
	"fmt"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
	"strings"
)

func shapeToRowValue(shape shp.Shape, opts *format.ParseOptions, srid int) (interface{}, error) {
	geometry, err := shapeToGeom(shape)
	if err != nil {
		return nil, err
	}

	switch geometryEncoding(opts) {
	case format.GeometryEncodingWKT:
		return commonSpatial.GeomToWKT(geometry)
	case format.GeometryEncodingWKB:
		return commonSpatial.GeomToWKB(geometry)
	case format.GeometryEncodingEWKB:
		return commonSpatial.GeomToEWKB(geometry, srid)
	default:
		return nil, fmt.Errorf("unsupported geometry encoding: %s", opts.GeometryEncoding)
	}
}

func geometryEncoding(opts *format.ParseOptions) format.GeometryEncoding {
	if opts == nil || opts.GeometryEncoding == "" {
		return format.GeometryEncodingWKT
	}
	return opts.GeometryEncoding
}

func spatialSRID(info *datatype.SpatialInfo) int {
	if info == nil {
		return 0
	}
	return info.PrimarySRIDValue()
}

func sridFromParseOptions(opts *format.ParseOptions) int {
	if opts == nil || opts.SpatialRefSys == "" {
		return 0
	}
	return commonSpatial.ParseSRID(opts.SpatialRefSys)
}

func determineShapefileGeometryType(shapeType shp.ShapeType) string {
	switch shapeType {
	case shp.POINT, shp.POINTZ, shp.POINTM:
		return "Point"
	case shp.POLYLINE, shp.POLYLINEZ, shp.POLYLINEM:
		return "LineString"
	case shp.POLYGON, shp.POLYGONZ, shp.POLYGONM:
		return "Polygon"
	case shp.MULTIPOINT, shp.MULTIPOINTZ, shp.MULTIPOINTM:
		return "MultiPoint"
	default:
		return "Geometry"
	}
}

func determineShapefileDimension(shapeType shp.ShapeType) int {
	switch shapeType {
	case shp.POINTZ, shp.POLYLINEZ, shp.POLYGONZ, shp.MULTIPOINTZ:
		return 3
	case shp.POINT, shp.POINTM, shp.POLYLINE, shp.POLYLINEM, shp.POLYGON, shp.POLYGONM, shp.MULTIPOINT, shp.MULTIPOINTM:
		return 2
	default:
		return 0
	}
}

func shapeTypeFromSchema(schema *datatype.TableInfo, spatialInfo *datatype.SpatialInfo) (shp.ShapeType, error) {
	geometryType := ""
	dimension := 0
	if spatialInfo != nil {
		geometryType = spatialInfo.PrimaryGeometryType()
		dimension = spatialInfo.PrimaryDimensionValue()
	}
	if geometryType == "" && schema != nil {
		for _, field := range schema.Fields {
			if datatype.IsSpatialFieldType(field.Type) {
				geometryType = string(field.Type)
				break
			}
		}
	}
	return shapefileShapeTypeFromGeometryType(geometryType, dimension)
}

func shapefileShapeTypeFromGeometryType(geometryType string, dimension int) (shp.ShapeType, error) {
	z := dimension >= 3
	switch normalizeGeometryTypeName(geometryType) {
	case "point":
		if z {
			return shp.POINTZ, nil
		}
		return shp.POINT, nil
	case "linestring", "multilinestring":
		if z {
			return shp.POLYLINEZ, nil
		}
		return shp.POLYLINE, nil
	case "polygon", "multipolygon":
		if z {
			return shp.POLYGONZ, nil
		}
		return shp.POLYGON, nil
	case "multipoint":
		if z {
			return shp.MULTIPOINTZ, nil
		}
		return shp.MULTIPOINT, nil
	default:
		return shp.NULL, fmt.Errorf("unsupported or missing shapefile geometry type %q", geometryType)
	}
}

func normalizeGeometryTypeName(geometryType string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(geometryType), "_", ""))
}

func shapeToGeom(shape shp.Shape) (geom.T, error) {
	switch s := shape.(type) {
	case *shp.Point:
		return geom.NewPointFlat(geom.XY, []float64{s.X, s.Y}), nil
	case *shp.PointZ:
		return geom.NewPointFlat(geom.XYZ, []float64{s.X, s.Y, s.Z}), nil
	case *shp.PointM:
		return geom.NewPointFlat(geom.XY, []float64{s.X, s.Y}), nil
	case *shp.MultiPoint:
		return multipointShapeToGeom(s.Points), nil
	case *shp.MultiPointZ:
		return multipointZShapeToGeom(s.Points, s.ZArray), nil
	case *shp.MultiPointM:
		return multipointShapeToGeom(s.Points), nil
	case *shp.PolyLine:
		return polylineShapeToGeom(s.Parts, s.Points)
	case *shp.PolyLineZ:
		return polylineZShapeToGeom(s.Parts, s.Points, s.ZArray)
	case *shp.PolyLineM:
		return polylineShapeToGeom(s.Parts, s.Points)
	case *shp.Polygon:
		return polygonShapeToGeom(s.Parts, s.Points)
	case *shp.PolygonZ:
		return polygonZShapeToGeom(s.Parts, s.Points, s.ZArray)
	case *shp.PolygonM:
		return polygonShapeToGeom(s.Parts, s.Points)
	default:
		return nil, fmt.Errorf("unsupported shape type: %T", shape)
	}
}

func polylineShapeToGeom(parts []int32, points []shp.Point) (geom.T, error) {
	return polylineCoordsToGeom(splitCoordParts(parts, pointsToCoords(points)), geom.XY)
}

func polylineZShapeToGeom(parts []int32, points []shp.Point, zArray []float64) (geom.T, error) {
	return polylineCoordsToGeom(splitCoordParts(parts, xyzCoords(points, zArray)), geom.XYZ)
}

func polylineCoordsToGeom(lineParts [][]geom.Coord, layout geom.Layout) (geom.T, error) {
	if len(lineParts) == 0 {
		return nil, fmt.Errorf("polyline has no points")
	}

	if len(lineParts) == 1 {
		line := geom.NewLineString(layout)
		line.MustSetCoords(lineParts[0])
		return line, nil
	}

	multi := geom.NewMultiLineString(layout)
	multi.MustSetCoords(lineParts)
	return multi, nil
}

func polygonShapeToGeom(parts []int32, points []shp.Point) (geom.T, error) {
	return polygonCoordsToGeom(splitCoordParts(parts, pointsToCoords(points)), geom.XY)
}

func polygonZShapeToGeom(parts []int32, points []shp.Point, zArray []float64) (geom.T, error) {
	return polygonCoordsToGeom(splitCoordParts(parts, xyzCoords(points, zArray)), geom.XYZ)
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

func multipointZShapeToGeom(points []shp.Point, zArray []float64) geom.T {
	coords := xyzCoords(points, zArray)
	if len(coords) == 1 {
		return geom.NewPointFlat(geom.XYZ, []float64{coords[0][0], coords[0][1], coords[0][2]})
	}

	multi := geom.NewMultiPoint(geom.XYZ)
	multi.MustSetCoords(coords)
	return multi
}

func splitCoordParts(parts []int32, coords []geom.Coord) [][]geom.Coord {
	if len(coords) == 0 {
		return nil
	}
	if len(parts) == 0 {
		return [][]geom.Coord{coords}
	}
	result := make([][]geom.Coord, 0, len(parts))
	for i, start := range parts {
		begin := int(start)
		if begin < 0 || begin >= len(coords) {
			continue
		}
		var end int
		if i == len(parts)-1 {
			end = len(coords)
		} else {
			end = int(parts[i+1])
		}
		if end <= begin || end > len(coords) {
			continue
		}
		result = append(result, coords[begin:end])
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

func xyzCoords(points []shp.Point, zArray []float64) []geom.Coord {
	coords := make([]geom.Coord, len(points))
	for i, p := range points {
		z := 0.0
		if i < len(zArray) {
			z = zArray[i]
		}
		coords[i] = geom.Coord{p.X, p.Y, z}
	}
	return coords
}

func polygonCoordsToGeom(rings [][]geom.Coord, layout geom.Layout) (geom.T, error) {
	if len(rings) == 0 {
		return nil, fmt.Errorf("polygon has no rings")
	}

	var polygons [][][]geom.Coord
	var current [][]geom.Coord

	for _, ring := range rings {
		closed := closeCoordsIfNeeded(ring)
		area := signedArea(closed)

		if area < 0 || len(current) == 0 {
			// New outer ring (Shapefile spec: clockwise, negative area).
			if len(current) > 0 {
				polygons = append(polygons, current)
			}
			current = [][]geom.Coord{ensureCounterClockwise(closed)}
		} else {
			// Inner ring (Shapefile spec: counter-clockwise).
			if current == nil {
				current = [][]geom.Coord{ensureCounterClockwise(closed)}
			} else {
				current = append(current, ensureClockwise(closed))
			}
		}
	}

	if len(current) > 0 {
		polygons = append(polygons, current)
	}

	if len(polygons) == 1 {
		polygon := geom.NewPolygon(layout)
		polygon.MustSetCoords(polygons[0])
		return polygon, nil
	}

	multi := geom.NewMultiPolygon(layout)
	multi.MustSetCoords(polygons)
	return multi, nil
}

func toShapefileGeometry(geomValue interface{}, shapeType shp.ShapeType) (shp.Shape, error) {
	geometry, err := commonSpatial.ParseGeometryValue(geomValue)
	if err != nil {
		return nil, err
	}
	return geomToShape(geometry, shapeType)
}

func geomToShape(geometry geom.T, shapeType shp.ShapeType) (shp.Shape, error) {
	switch g := geometry.(type) {
	case *geom.Point:
		if shapeType == shp.POINTZ {
			return &shp.PointZ{X: g.X(), Y: g.Y(), Z: coordZ(g.Coords()), M: shapefileNoDataMeasure}, nil
		}
		return &shp.Point{X: g.X(), Y: g.Y()}, nil

	case *geom.LineString:
		return lineStringToShapefile(g, shapeType)

	case *geom.MultiLineString:
		return multiLineStringToShapefile(g, shapeType)

	case *geom.Polygon:
		return polygonToShapefile(g, shapeType)

	case *geom.MultiPolygon:
		return multiPolygonToShapefile(g, shapeType)

	case *geom.MultiPoint:
		numPoints := g.NumPoints()
		points := make([]shp.Point, numPoints)
		zArray := make([]float64, numPoints)
		for i := 0; i < numPoints; i++ {
			p := g.Point(i)
			points[i] = shp.Point{X: p.X(), Y: p.Y()}
			zArray[i] = coordZ(p.Coords())
		}
		if shapeType == shp.MULTIPOINTZ {
			return &shp.MultiPointZ{
				Box:       calculateBox(points),
				NumPoints: int32(numPoints),
				Points:    points,
				ZRange:    floatRangeArray(zArray),
				ZArray:    zArray,
				MRange:    noDataMeasureRange(),
				MArray:    noDataMeasureArray(numPoints),
			}, nil
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

func lineStringToShapefile(line *geom.LineString, shapeType shp.ShapeType) (shp.Shape, error) {
	if line.NumCoords() < 2 {
		return nil, fmt.Errorf("linestring requires at least two points")
	}

	coords := line.Coords()
	points := coordsToShpPoints(coords)
	if shapeType == shp.POLYLINEZ {
		zArray := coordsToZArray(coords)
		return &shp.PolyLineZ{
			Box:       calculateBox(points),
			NumParts:  1,
			NumPoints: int32(len(points)),
			Parts:     []int32{0},
			Points:    points,
			ZRange:    floatRangeArray(zArray),
			ZArray:    zArray,
			MRange:    noDataMeasureRange(),
			MArray:    noDataMeasureArray(len(points)),
		}, nil
	}
	return &shp.PolyLine{
		Box:       calculateBox(points),
		NumParts:  1,
		NumPoints: int32(len(points)),
		Parts:     []int32{0},
		Points:    points,
	}, nil
}

func multiLineStringToShapefile(multiline *geom.MultiLineString, shapeType shp.ShapeType) (shp.Shape, error) {
	if multiline.NumLineStrings() == 0 {
		return nil, fmt.Errorf("multilinestring has no parts")
	}

	var allPoints []shp.Point
	var zArray []float64
	parts := make([]int32, multiline.NumLineStrings())
	offset := 0

	for i := 0; i < multiline.NumLineStrings(); i++ {
		line := multiline.LineString(i)
		if line.NumCoords() < 2 {
			return nil, fmt.Errorf("linestring requires at least two points")
		}
		coords := line.Coords()
		partPoints := coordsToShpPoints(coords)
		parts[i] = int32(offset)
		allPoints = append(allPoints, partPoints...)
		zArray = append(zArray, coordsToZArray(coords)...)
		offset += len(partPoints)
	}

	if shapeType == shp.POLYLINEZ {
		return &shp.PolyLineZ{
			Box:       calculateBox(allPoints),
			NumParts:  int32(len(parts)),
			NumPoints: int32(len(allPoints)),
			Parts:     parts,
			Points:    allPoints,
			ZRange:    floatRangeArray(zArray),
			ZArray:    zArray,
			MRange:    noDataMeasureRange(),
			MArray:    noDataMeasureArray(len(allPoints)),
		}, nil
	}
	return &shp.PolyLine{
		Box:       calculateBox(allPoints),
		NumParts:  int32(len(parts)),
		NumPoints: int32(len(allPoints)),
		Parts:     parts,
		Points:    allPoints,
	}, nil
}

func polygonToShapefile(polygon *geom.Polygon, shapeType shp.ShapeType) (shp.Shape, error) {
	if polygon.NumLinearRings() == 0 {
		return nil, fmt.Errorf("polygon has no rings")
	}
	return buildShapefilePolygon([]*geom.Polygon{polygon}, shapeType)
}

func multiPolygonToShapefile(multi *geom.MultiPolygon, shapeType shp.ShapeType) (shp.Shape, error) {
	if multi.NumPolygons() == 0 {
		return nil, fmt.Errorf("multipolygon has no polygons")
	}

	polygons := make([]*geom.Polygon, multi.NumPolygons())
	for i := 0; i < multi.NumPolygons(); i++ {
		polygons[i] = multi.Polygon(i)
	}
	return buildShapefilePolygon(polygons, shapeType)
}

func buildShapefilePolygon(polygons []*geom.Polygon, shapeType shp.ShapeType) (shp.Shape, error) {
	var allPoints []shp.Point
	var zArray []float64
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
			zArray = append(zArray, coordsToZArray(closed)...)
			offset += len(partPoints)
		}
	}

	if len(allPoints) == 0 {
		return nil, fmt.Errorf("polygon contains no points")
	}

	if shapeType == shp.POLYGONZ {
		return &shp.PolygonZ{
			Box:       calculateBox(allPoints),
			NumParts:  int32(len(parts)),
			NumPoints: int32(len(allPoints)),
			Parts:     parts,
			Points:    allPoints,
			ZRange:    floatRangeArray(zArray),
			ZArray:    zArray,
			MRange:    noDataMeasureRange(),
			MArray:    noDataMeasureArray(len(allPoints)),
		}, nil
	}
	return &shp.Polygon{
		Box:       calculateBox(allPoints),
		NumParts:  int32(len(parts)),
		NumPoints: int32(len(allPoints)),
		Parts:     parts,
		Points:    allPoints,
	}, nil
}

const shapefileNoDataMeasure = -1e39

func coordsToShpPoints(coords []geom.Coord) []shp.Point {
	points := make([]shp.Point, len(coords))
	for i, c := range coords {
		points[i] = shp.Point{X: c.X(), Y: c.Y()}
	}
	return points
}

func coordsToZArray(coords []geom.Coord) []float64 {
	values := make([]float64, len(coords))
	for i, coord := range coords {
		values[i] = coordZ(coord)
	}
	return values
}

func coordZ(coord geom.Coord) float64 {
	if len(coord) >= 3 {
		return coord[2]
	}
	return 0
}

func floatRangeArray(values []float64) [2]float64 {
	min, max := floatRange(values)
	return [2]float64{min, max}
}

func floatRange(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	min, max := values[0], values[0]
	for _, value := range values[1:] {
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}
	return min, max
}

func noDataMeasureRange() [2]float64 {
	return [2]float64{shapefileNoDataMeasure, shapefileNoDataMeasure}
}

func noDataMeasureArray(length int) []float64 {
	values := make([]float64, length)
	for i := range values {
		values[i] = shapefileNoDataMeasure
	}
	return values
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

func almostEqual(a, b float64) bool {
	const epsilon = 1e-9
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
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
	closed := append([]geom.Coord(nil), coords...)
	closed = append(closed, append(geom.Coord(nil), first...))
	return closed
}

func ensureClockwise(coords []geom.Coord) []geom.Coord {
	if signedArea(coords) <= 0 {
		return coords
	}
	return reverseCoords(coords)
}

func ensureCounterClockwise(coords []geom.Coord) []geom.Coord {
	if signedArea(coords) >= 0 {
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

func signedArea(coords []geom.Coord) float64 {
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
