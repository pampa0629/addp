package shapefile

import (
	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
	"testing"
)

func TestShapeToRowValueReturnsWKTByDefault(t *testing.T) {
	t.Parallel()

	got, err := shapeToRowValue(&shp.Point{X: 116.4, Y: 39.9}, nil, 0)
	if err != nil {
		t.Fatalf("shapeToRowValue() error = %v", err)
	}
	if got != "POINT (116.4 39.9)" {
		t.Fatalf("shapeToRowValue() = %q, want real WKT", got)
	}
}

func TestShapeToRowValueWKTPreservesZCoordinate(t *testing.T) {
	t.Parallel()

	got, err := shapeToRowValue(&shp.PointZ{X: 116.4, Y: 39.9, Z: 25}, &format.ParseOptions{
		GeometryEncoding: format.GeometryEncodingWKT,
	}, 0)
	if err != nil {
		t.Fatalf("shapeToRowValue() error = %v", err)
	}
	if got != "POINT Z (116.4 39.9 25)" {
		t.Fatalf("shapeToRowValue() = %q, want Z WKT", got)
	}
}

func TestShapeToRowValueSupportsGeometryEncoding(t *testing.T) {
	t.Parallel()

	value, err := shapeToRowValue(&shp.Point{X: 116.4, Y: 39.9}, &format.ParseOptions{
		GeometryEncoding: format.GeometryEncodingEWKB,
	}, 4326)
	if err != nil {
		t.Fatalf("shapeToRowValue() error = %v", err)
	}
	data, ok := value.([]byte)
	if !ok {
		t.Fatalf("geometry value type = %T, want []byte", value)
	}
	geometry, err := commonSpatial.ParseGeometryValue(data)
	if err != nil {
		t.Fatalf("ParseGeometryValue() error = %v", err)
	}
	if got := geometry.SRID(); got != 4326 {
		t.Fatalf("SRID = %d, want 4326", got)
	}
}

func TestDetermineShapefileDimension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		shapeType shp.ShapeType
		want      int
	}{
		{name: "point", shapeType: shp.POINT, want: 2},
		{name: "point z", shapeType: shp.POINTZ, want: 3},
		{name: "point m", shapeType: shp.POINTM, want: 2},
		{name: "polygon z", shapeType: shp.POLYGONZ, want: 3},
		{name: "multipoint m", shapeType: shp.MULTIPOINTM, want: 2},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := determineShapefileDimension(tt.shapeType); got != tt.want {
				t.Fatalf("determineShapefileDimension() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestShapeTypeFromSchemaUsesSpatialDimension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		geometryType string
		dimension    int
		want         shp.ShapeType
	}{
		{name: "point xy", geometryType: "Point", dimension: 2, want: shp.POINT},
		{name: "point z", geometryType: "Point", dimension: 3, want: shp.POINTZ},
		{name: "line z", geometryType: "LineString", dimension: 3, want: shp.POLYLINEZ},
		{name: "polygon z", geometryType: "Polygon", dimension: 3, want: shp.POLYGONZ},
		{name: "multipoint z", geometryType: "MultiPoint", dimension: 3, want: shp.MULTIPOINTZ},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := shapeTypeFromSchema(&format.TableInfo{
				SpatialInfo: &format.SpatialInfo{
					GeometryType: tt.geometryType,
					Dimension:    tt.dimension,
				},
			})
			if err != nil {
				t.Fatalf("shapeTypeFromSchema() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("shapeType = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShapeToGeomPreservesZLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		shape shp.Shape
		want  geom.Layout
	}{
		{name: "point z", shape: &shp.PointZ{X: 1, Y: 2, Z: 3}, want: geom.XYZ},
		{name: "multipoint z", shape: &shp.MultiPointZ{Points: []shp.Point{{X: 1, Y: 2}, {X: 3, Y: 4}}, ZArray: []float64{5, 6}}, want: geom.XYZ},
		{name: "polyline z", shape: &shp.PolyLineZ{Parts: []int32{0}, Points: []shp.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}, ZArray: []float64{7, 8}}, want: geom.XYZ},
		{name: "polygon z", shape: &shp.PolygonZ{
			Parts:  []int32{0},
			Points: []shp.Point{{X: 0, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 0}, {X: 0, Y: 0}},
			ZArray: []float64{9, 10, 11, 12, 9},
		}, want: geom.XYZ},
		{name: "point m", shape: &shp.PointM{X: 1, Y: 2, M: 3}, want: geom.XY},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := shapeToGeom(tt.shape)
			if err != nil {
				t.Fatalf("shapeToGeom() error = %v", err)
			}
			if got.Layout() != tt.want {
				t.Fatalf("layout = %v, want %v", got.Layout(), tt.want)
			}
		})
	}
}

func TestGeomToShapePreservesZForTargetShapeType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		geometry  geom.T
		shapeType shp.ShapeType
		assert    func(t *testing.T, shape shp.Shape)
	}{
		{
			name:      "point z",
			geometry:  geom.NewPointFlat(geom.XYZ, []float64{1, 2, 3}),
			shapeType: shp.POINTZ,
			assert: func(t *testing.T, shape shp.Shape) {
				got, ok := shape.(*shp.PointZ)
				if !ok {
					t.Fatalf("shape = %T, want *shp.PointZ", shape)
				}
				if got.Z != 3 {
					t.Fatalf("Z = %v, want 3", got.Z)
				}
				if got.M != shapefileNoDataMeasure {
					t.Fatalf("M = %v, want shapefile no-data", got.M)
				}
			},
		},
		{
			name:      "polyline z",
			geometry:  geom.NewLineStringFlat(geom.XYZ, []float64{0, 0, 5, 1, 1, 6}),
			shapeType: shp.POLYLINEZ,
			assert: func(t *testing.T, shape shp.Shape) {
				got, ok := shape.(*shp.PolyLineZ)
				if !ok {
					t.Fatalf("shape = %T, want *shp.PolyLineZ", shape)
				}
				if got.ZArray[0] != 5 || got.ZArray[1] != 6 {
					t.Fatalf("ZArray = %#v, want [5 6]", got.ZArray)
				}
				assertNoDataMeasures(t, got.MArray)
			},
		},
		{
			name:      "polygon z",
			geometry:  geom.NewPolygonFlat(geom.XYZ, []float64{0, 0, 7, 0, 1, 8, 1, 1, 9}, []int{9}),
			shapeType: shp.POLYGONZ,
			assert: func(t *testing.T, shape shp.Shape) {
				got, ok := shape.(*shp.PolygonZ)
				if !ok {
					t.Fatalf("shape = %T, want *shp.PolygonZ", shape)
				}
				if got.ZArray[0] != 7 || got.ZArray[1] != 8 || got.ZArray[2] != 9 || got.ZArray[len(got.ZArray)-1] != 7 {
					t.Fatalf("ZArray = %#v, want closed Z values [7 8 9 ... 7]", got.ZArray)
				}
				assertNoDataMeasures(t, got.MArray)
			},
		},
		{
			name:      "multipoint z",
			geometry:  geom.NewMultiPointFlat(geom.XYZ, []float64{1, 2, 10, 3, 4, 11}),
			shapeType: shp.MULTIPOINTZ,
			assert: func(t *testing.T, shape shp.Shape) {
				got, ok := shape.(*shp.MultiPointZ)
				if !ok {
					t.Fatalf("shape = %T, want *shp.MultiPointZ", shape)
				}
				if got.ZArray[0] != 10 || got.ZArray[1] != 11 {
					t.Fatalf("ZArray = %#v, want [10 11]", got.ZArray)
				}
				assertNoDataMeasures(t, got.MArray)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			shape, err := geomToShape(tt.geometry, tt.shapeType)
			if err != nil {
				t.Fatalf("geomToShape() error = %v", err)
			}
			tt.assert(t, shape)
		})
	}
}

func assertNoDataMeasures(t *testing.T, values []float64) {
	t.Helper()
	for i, value := range values {
		if value != shapefileNoDataMeasure {
			t.Fatalf("MArray[%d] = %v, want shapefile no-data", i, value)
		}
	}
}
