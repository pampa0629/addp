package shapefile

import (
	"bytes"
	"encoding/binary"
	"github.com/jonas-p/go-shp"
	"testing"
)

func TestParseIndexedShapeContentSupportsZAndMTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		shapeType shp.ShapeType
		content   []byte
		wantType  interface{}
	}{
		{name: "point z", shapeType: shp.POINTZ, content: shapeContent(&shp.PointZ{X: 1, Y: 2, Z: 3, M: 4}), wantType: &shp.PointZ{}},
		{name: "point m", shapeType: shp.POINTM, content: shapeContent(&shp.PointM{X: 1, Y: 2, M: 4}), wantType: &shp.PointM{}},
		{name: "polyline z", shapeType: shp.POLYLINEZ, content: polyLineZContent([]int32{0}, []shp.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}, []float64{5, 6}), wantType: &shp.PolyLineZ{}},
		{name: "polyline m", shapeType: shp.POLYLINEM, content: polyLineMContent([]int32{0}, []shp.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}, []float64{5, 6}), wantType: &shp.PolyLineM{}},
		{name: "polygon z", shapeType: shp.POLYGONZ, content: polyLineZContent([]int32{0}, []shp.Point{{X: 0, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}, {X: 0, Y: 0}}, []float64{5, 6, 7, 5}), wantType: &shp.PolygonZ{}},
		{name: "polygon m", shapeType: shp.POLYGONM, content: polyLineMContent([]int32{0}, []shp.Point{{X: 0, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}, {X: 0, Y: 0}}, []float64{5, 6, 7, 5}), wantType: &shp.PolygonM{}},
		{name: "multipoint z", shapeType: shp.MULTIPOINTZ, content: multiPointZContent([]shp.Point{{X: 1, Y: 2}, {X: 3, Y: 4}}, []float64{5, 6}), wantType: &shp.MultiPointZ{}},
		{name: "multipoint m", shapeType: shp.MULTIPOINTM, content: multiPointMContent([]shp.Point{{X: 1, Y: 2}, {X: 3, Y: 4}}, []float64{5, 6}), wantType: &shp.MultiPointM{}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			shape, err := parseShapeContent(tt.shapeType, bytes.NewReader(tt.content))
			if err != nil {
				t.Fatalf("parseShapeContent() error = %v", err)
			}
			if _, ok := shapeOfSameType(shape, tt.wantType); !ok {
				t.Fatalf("shape type = %T, want %T", shape, tt.wantType)
			}
		})
	}
}

func TestParseIndexedShapeContentAllowsMissingOptionalMeasureArray(t *testing.T) {
	t.Parallel()

	shape, err := parseShapeContent(
		shp.POLYLINEZ,
		bytes.NewReader(polyLineZContentWithoutMeasures([]int32{0}, []shp.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}, []float64{5, 6})),
	)
	if err != nil {
		t.Fatalf("parseShapeContent() error = %v", err)
	}
	line, ok := shape.(*shp.PolyLineZ)
	if !ok {
		t.Fatalf("shape type = %T, want *shp.PolyLineZ", shape)
	}
	if len(line.MArray) != 2 {
		t.Fatalf("MArray length = %d, want point count", len(line.MArray))
	}
}

func shapeContent(value interface{}) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, value)
	return buf.Bytes()
}

func polyLineZContent(parts []int32, points []shp.Point, zArray []float64) []byte {
	var buf bytes.Buffer
	writeLineHeader(&buf, parts, points)
	writeFloatRangeAndArray(&buf, zArray)
	writeFloatRangeAndArray(&buf, make([]float64, len(points)))
	return buf.Bytes()
}

func polyLineZContentWithoutMeasures(parts []int32, points []shp.Point, zArray []float64) []byte {
	var buf bytes.Buffer
	writeLineHeader(&buf, parts, points)
	writeFloatRangeAndArray(&buf, zArray)
	return buf.Bytes()
}

func polyLineMContent(parts []int32, points []shp.Point, mArray []float64) []byte {
	var buf bytes.Buffer
	writeLineHeader(&buf, parts, points)
	writeFloatRangeAndArray(&buf, mArray)
	return buf.Bytes()
}

func multiPointZContent(points []shp.Point, zArray []float64) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, calculateBox(points))
	_ = binary.Write(&buf, binary.LittleEndian, int32(len(points)))
	_ = binary.Write(&buf, binary.LittleEndian, points)
	writeFloatRangeAndArray(&buf, zArray)
	writeFloatRangeAndArray(&buf, make([]float64, len(points)))
	return buf.Bytes()
}

func multiPointMContent(points []shp.Point, mArray []float64) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, calculateBox(points))
	_ = binary.Write(&buf, binary.LittleEndian, int32(len(points)))
	_ = binary.Write(&buf, binary.LittleEndian, points)
	writeFloatRangeAndArray(&buf, mArray)
	return buf.Bytes()
}

func writeLineHeader(buf *bytes.Buffer, parts []int32, points []shp.Point) {
	_ = binary.Write(buf, binary.LittleEndian, calculateBox(points))
	_ = binary.Write(buf, binary.LittleEndian, int32(len(parts)))
	_ = binary.Write(buf, binary.LittleEndian, int32(len(points)))
	_ = binary.Write(buf, binary.LittleEndian, parts)
	_ = binary.Write(buf, binary.LittleEndian, points)
}

func writeFloatRangeAndArray(buf *bytes.Buffer, values []float64) {
	min, max := floatRange(values)
	_ = binary.Write(buf, binary.LittleEndian, [2]float64{min, max})
	_ = binary.Write(buf, binary.LittleEndian, values)
}

func shapeOfSameType(shape shp.Shape, want interface{}) (shp.Shape, bool) {
	switch want.(type) {
	case *shp.PointZ:
		typed, ok := shape.(*shp.PointZ)
		return typed, ok
	case *shp.PointM:
		typed, ok := shape.(*shp.PointM)
		return typed, ok
	case *shp.PolyLineZ:
		typed, ok := shape.(*shp.PolyLineZ)
		return typed, ok
	case *shp.PolyLineM:
		typed, ok := shape.(*shp.PolyLineM)
		return typed, ok
	case *shp.PolygonZ:
		typed, ok := shape.(*shp.PolygonZ)
		return typed, ok
	case *shp.PolygonM:
		typed, ok := shape.(*shp.PolygonM)
		return typed, ok
	case *shp.MultiPointZ:
		typed, ok := shape.(*shp.MultiPointZ)
		return typed, ok
	case *shp.MultiPointM:
		typed, ok := shape.(*shp.MultiPointM)
		return typed, ok
	default:
		return shape, false
	}
}
