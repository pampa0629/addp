package oracle

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkb"
)

func TestOracleSelectedFieldsAllowsExcludingUnsupportedColumns(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "ID", Type: datatype.FieldTypeBigInt},
		{Name: "SHAPE", Type: datatype.FieldTypeUnknown, NativeType: "MDSYS.SDO_GEOMETRY"},
	}
	selected, err := oracleSelectedFields(fields, map[string]interface{}{
		format.FieldSelectionOptionKey: format.FieldSelectionOptions{Include: []string{"ID"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Name != "ID" {
		t.Fatalf("selected fields = %#v", selected)
	}
}

func TestOracleScanValuesPreservesDecimalTextAndBytes(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "ID", Type: datatype.FieldTypeBigInt},
		{Name: "AMOUNT", Type: datatype.FieldTypeDecimal},
		{Name: "PAYLOAD", Type: datatype.FieldTypeBytes},
	}
	values, destinations := oracleScanValues(fields, []string{"ID", "AMOUNT", "PAYLOAD"})

	*destinations[0].(*sql.NullInt64) = sql.NullInt64{Int64: 42, Valid: true}
	*destinations[1].(*sql.RawBytes) = sql.RawBytes("12345678901234567890.25")
	*destinations[2].(*[]byte) = []byte{1, 2, 3}

	got := []interface{}{values[0](), values[1](), values[2]()}
	want := []interface{}{int64(42), "12345678901234567890.25", []byte{1, 2, 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scan values = %#v, want %#v", got, want)
	}
}

func TestOracleSpatialInfoAndEWKBConversion(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "SHAPE", Type: datatype.FieldTypeGeometry, NativeType: "MDSYS.SDO_GEOMETRY"}}
	info := oracleSpatialInfoFromFields(fields)
	if info == nil || info.PrimaryGeometryName() != "SHAPE" || info.PrimaryGeometryType() != "Geometry" {
		t.Fatalf("Oracle spatial info = %#v", info)
	}
	point := geom.NewPointFlat(geom.XY, []float64{120.5, 30.25})
	wkbValue, err := wkb.Marshal(point, wkb.NDR)
	if err != nil {
		t.Fatal(err)
	}
	ewkbValue, err := oracleWKBToEWKB(wkbValue, 4326)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ewkb.Unmarshal(ewkbValue)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SRID() != 4326 {
		t.Fatalf("Oracle EWKB SRID = %d, want 4326", decoded.SRID())
	}
	value, err := oracleReadValue("SHAPE", wkbValue, fields, &datatype.SpatialInfo{GeometryColumns: []datatype.GeometryColumnInfo{{Name: "SHAPE", SRID: intPtrForTest(4326)}}}, format.GeometryEncodingEWKB)
	if err != nil || value == nil {
		t.Fatalf("Oracle spatial row value = %#v, error = %v", value, err)
	}
}

func TestOraclePolygonEWKBAndCentroid(t *testing.T) {
	polygonCoords := []float64{
		0, 0,
		2, 0,
		2, 2,
		0, 2,
		0, 0,
	}
	polygon := geom.NewPolygonFlat(geom.XY, polygonCoords, []int{len(polygonCoords)})
	wkbValue, err := wkb.Marshal(polygon, wkb.NDR)
	if err != nil {
		t.Fatal(err)
	}
	ewkbValue, err := oracleWKBToEWKB(wkbValue, 4326)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ewkb.Unmarshal(ewkbValue)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.(*geom.Polygon); !ok || decoded.SRID() != 4326 {
		t.Fatalf("Oracle polygon EWKB = %T SRID=%d, want Polygon SRID=4326", decoded, decoded.SRID())
	}

	centroidEWKB, err := oracleGeometryCentroidEWKB(ewkbValue, 4326)
	if err != nil {
		t.Fatal(err)
	}
	centroid, err := ewkb.Unmarshal(centroidEWKB)
	if err != nil {
		t.Fatal(err)
	}
	point, ok := centroid.(*geom.Point)
	if !ok || point.SRID() != 4326 {
		t.Fatalf("Oracle polygon centroid = %#v, want SRID 4326 point", centroid)
	}
	coords := point.FlatCoords()
	if len(coords) != 2 || coords[0] != 1 || coords[1] != 1 {
		t.Fatalf("Oracle polygon centroid coordinates = %#v, want [1 1]", coords)
	}
}

func intPtrForTest(value int) *int { return &value }
