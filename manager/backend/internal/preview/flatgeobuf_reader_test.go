package preview

import (
	"context"
	"io"
	"testing"

	"github.com/addp/common/datatype"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/twpayne/go-geom"
)

type fakeFlatGeobufTableReader struct {
	fields  []datatype.FieldInfo
	spatial *datatype.SpatialInfo
	rows    []map[string]interface{}
	offset  int
	closed  bool
}

func (r *fakeFlatGeobufTableReader) Fields() []datatype.FieldInfo {
	return r.fields
}

func (r *fakeFlatGeobufTableReader) SpatialInfo() *datatype.SpatialInfo {
	return r.spatial
}

func (r *fakeFlatGeobufTableReader) ReadRows(_ context.Context, limit int) ([]map[string]interface{}, error) {
	if r.offset >= len(r.rows) {
		return nil, nil
	}
	if limit <= 0 {
		limit = len(r.rows) - r.offset
	}
	end := r.offset + limit
	if end > len(r.rows) {
		end = len(r.rows)
	}
	rows := r.rows[r.offset:end]
	r.offset = end
	return rows, nil
}

func (r *fakeFlatGeobufTableReader) Close(context.Context) error {
	r.closed = true
	return nil
}

func TestFlatGeobufTableReaderUsesEWKBFeatureStream(t *testing.T) {
	geomValue, err := commonSpatial.EncodeGeometryValue(geom.NewPointFlat(geom.XY, []float64{120.1, 30.2}), string(commonSpatial.GeometryEncodingEWKB), 4326)
	if err != nil {
		t.Fatalf("EncodeGeometryValue() error = %v", err)
	}
	ewkb, ok := geomValue.([]byte)
	if !ok {
		t.Fatalf("EncodeGeometryValue() = %T, want []byte", geomValue)
	}
	srid := 4326
	reader := &fakeFlatGeobufTableReader{
		fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "shape", Type: datatype.FieldTypeGeometry},
			{Name: "name", Type: datatype.FieldTypeString},
		},
		spatial: &datatype.SpatialInfo{
			GeometryColumns: []datatype.GeometryColumnInfo{{
				Name:         "shape",
				GeometryType: "Point",
				SRID:         &srid,
			}},
			PrimaryGeometryColumn: "shape",
		},
		rows: []map[string]interface{}{
			{"id": int64(1), "shape": ewkb, "name": "a"},
			{"id": int64(2), "shape": ewkb, "name": "b"},
		},
	}

	result, err := flatGeobufResultFromTableReader(reader, "", 1, nil, reader.Close)
	if err != nil {
		t.Fatalf("flatGeobufResultFromTableReader() error = %v", err)
	}
	if result.Options.SRID != 4326 || result.Options.GeometryType != "Point" {
		t.Fatalf("options = %#v, want EPSG:4326 Point", result.Options)
	}
	if len(result.Options.Columns) != 2 || result.Options.Columns[0].Name != "id" || result.Options.Columns[1].Name != "name" {
		t.Fatalf("columns = %#v, want non-geometry fields", result.Options.Columns)
	}

	feature, err := result.Reader.NextFlatGeobufFeature(context.Background())
	if err != nil {
		t.Fatalf("NextFlatGeobufFeature() error = %v", err)
	}
	if feature.GeometryEncoding != string(commonSpatial.GeometryEncodingEWKB) {
		t.Fatalf("GeometryEncoding = %q, want ewkb", feature.GeometryEncoding)
	}
	if _, ok := feature.Properties["shape"]; ok {
		t.Fatalf("geometry column leaked into properties: %#v", feature.Properties)
	}
	if feature.Properties["name"] != "a" {
		t.Fatalf("properties = %#v, want first row", feature.Properties)
	}
	if _, err := result.Reader.NextFlatGeobufFeature(context.Background()); err != io.EOF {
		t.Fatalf("second NextFlatGeobufFeature() error = %v, want io.EOF due row limit", err)
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !reader.closed {
		t.Fatal("reader was not closed")
	}
}
