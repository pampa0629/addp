package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	geomjson "github.com/twpayne/go-geom/encoding/geojson"
	"github.com/twpayne/go-geom/encoding/wkb"
	"github.com/twpayne/go-geom/encoding/wkt"
)

func TestGeoPackageReadCapabilitiesMatchRegisteredReader(t *testing.T) {
	capability, err := format.GetSpatialEncodingCapability(format.FormatGeoPackage)
	if err != nil || capability.DefaultReadEncoding != format.GeometryEncodingEWKB || len(capability.GeometryReadEncodings) != 4 || len(capability.GeometryWriteEncodings) != 0 {
		t.Fatalf("GeoPackage read capabilities = %#v, error = %v", capability, err)
	}
	if _, err := format.GetTableReaderProvider(format.FormatGeoPackage); err != nil {
		t.Fatal(err)
	}
	if _, err := format.GetSpatialEncodingCapability(format.FormatSQLite); err == nil {
		t.Fatal("ordinary SQLite advertises unverified geometry encoding")
	}
	if _, err := format.GetTableReaderProvider(format.FormatSQLite); err == nil {
		t.Fatal("ordinary SQLite reader capability expanded outside this change")
	}
}

func TestGeoPackageGeometrySamplesAndContinuousReads(t *testing.T) {
	data, native := geometryReadDatabase(t, true, false)
	expected := geom.NewLineStringFlat(geom.XY, []float64{0.5, 0, 10, 0})
	defaults, err := NewGeoPackagePlugin(nil).SampleTable(context.Background(), bytes.NewReader(data), 0, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults) != 1 {
		t.Fatalf("default feature sample=%#v", defaults)
	}
	if encoded, ok := defaults[0]["route_shape"].([]byte); !ok || len(encoded) != 45 {
		t.Fatalf("default selection/encoding = %#v", defaults)
	}

	for _, encoding := range []format.GeometryEncoding{"", format.GeometryEncodingEWKB, format.GeometryEncodingWKB, format.GeometryEncodingWKT, format.GeometryEncodingGeoJSON} {
		t.Run(string(encoding), func(t *testing.T) {
			plugin := NewGeoPackagePlugin(nil)
			opts := &format.ParseOptions{GeometryEncoding: encoding, ExtraParams: map[string]interface{}{format.ChildTableParam: "routes"}}
			info, err := plugin.DescribeTable(context.Background(), bytes.NewReader(data), opts)
			if err != nil {
				t.Fatal(err)
			}
			if info.Spatial.PrimaryGeometryName() != "route_shape" || info.Spatial.PrimarySRIDValue() != 3857 || info.Table.GetField("route_shape").Type != datatype.FieldTypeGeometry || info.Table.GetField("geom").Type != datatype.FieldTypeBytes {
				t.Fatalf("wrong schema/spatial facts: %#v %#v", info.Table, info.Spatial)
			}
			check := func(rows []map[string]interface{}) {
				t.Helper()
				for _, row := range rows {
					id := row["fid"].(int64)
					if row["name"] != "kept" {
						t.Fatalf("scalar changed: %#v", row)
					}
					if b, ok := row["geom"].([]byte); !ok || !bytes.Equal(b, native) {
						t.Fatalf("non-geometry BLOB changed: %T", row["geom"])
					}
					value := row["route_shape"]
					if id == 2 {
						if value != nil {
							t.Fatalf("null geometry changed: %#v", value)
						}
						continue
					}
					var shape geom.T
					var err error
					switch encoding {
					case "", format.GeometryEncodingEWKB:
						b, ok := value.([]byte)
						if !ok {
							t.Fatalf("EWKB type %T", value)
						}
						shape, err = ewkb.Unmarshal(b)
						if err == nil && shape.SRID() != 3857 {
							t.Fatalf("EWKB SRID=%d", shape.SRID())
						}
						if id == 1 && len(b) != 45 {
							t.Fatalf("EWKB len=%d, want 45 (not 81-byte GP)", len(b))
						}
						wire, _ := json.Marshal(value)
						var wireBytes []byte
						if err := json.Unmarshal(wire, &wireBytes); err != nil || !bytes.Equal(b, wireBytes) {
							t.Fatal("EWKB JSON must be original standard bytes in Base64")
						}
					case format.GeometryEncodingWKB:
						b, ok := value.([]byte)
						if !ok {
							t.Fatalf("WKB type %T", value)
						}
						shape, err = wkb.Unmarshal(b)
					case format.GeometryEncodingWKT:
						text, ok := value.(string)
						if !ok {
							t.Fatalf("WKT type %T", value)
						}
						shape, err = wkt.Unmarshal(text)
					case format.GeometryEncodingGeoJSON:
						object, ok := value.(map[string]interface{})
						if !ok {
							t.Fatalf("GeoJSON type %T", value)
						}
						wire, _ := json.Marshal(object)
						err = geomjson.Unmarshal(wire, &shape)
					}
					if err != nil {
						t.Fatal(err)
					}
					if id == 3 {
						if !shape.Empty() {
							t.Fatal("empty geometry changed")
						}
					} else if !reflect.DeepEqual(shape.FlatCoords(), expected.FlatCoords()) {
						t.Fatalf("source coordinates changed: %v", shape.FlatCoords())
					}
				}
			}
			rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 0, 5, opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 3 {
				t.Fatalf("sample rows=%d", len(rows))
			}
			check(rows)
			window, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 1, 1, opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(window) != 1 || window[0]["fid"] != int64(2) {
				t.Fatal("sample window lost null row")
			}
			check(window)
			reader, err := plugin.OpenTableReader(context.Background(), bytes.NewReader(data), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close(context.Background())
			if !reflect.DeepEqual(reader.Fields(), info.Table.Fields) || reader.(format.TableSpatialInfoProvider).SpatialInfo().PrimarySRIDValue() != 3857 {
				t.Fatal("continuous reader lost schema/SRID")
			}
			var read []map[string]interface{}
			for {
				batch, err := reader.ReadRows(context.Background(), 2)
				if err != nil {
					t.Fatal(err)
				}
				if len(batch) == 0 {
					break
				}
				check(batch)
				read = append(read, batch...)
			}
			if !reflect.DeepEqual(read, rows) {
				t.Fatal("sample and continuous rows differ")
			}
			if err := reader.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.ReadRows(context.Background(), 1); err == nil {
				t.Fatal("closed reader accepted rows")
			}
		})
	}
}

func TestSQLiteBLOBIsNeverGuessedAsGeometry(t *testing.T) {
	data, native := geometryReadDatabase(t, false, false)
	plugin := NewPlugin(nil)
	opts := &format.ParseOptions{GeometryEncoding: format.GeometryEncodingEWKB, ExtraParams: map[string]interface{}{format.ChildTableParam: "routes"}}
	info, err := plugin.DescribeTable(context.Background(), bytes.NewReader(data), opts)
	if err != nil {
		t.Fatal(err)
	}
	if info.Spatial != nil {
		t.Fatal("ordinary SQLite inferred spatial facts")
	}
	sample, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 0, 1, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, rows := range [][]map[string]interface{}{sample} {
		for _, column := range []string{"route_shape", "geom"} {
			value, ok := rows[0][column].([]byte)
			if !ok || !bytes.Equal(value, native) {
				t.Fatalf("BLOB %s reinterpreted: %T", column, rows[0][column])
			}
		}
	}
}

func TestGeoPackageRejectsBrokenGeometryAndUnsupportedEncoding(t *testing.T) {
	data, _ := geometryReadDatabase(t, true, true)
	plugin := NewGeoPackagePlugin(nil)
	opts := &format.ParseOptions{ExtraParams: map[string]interface{}{format.ChildTableParam: "routes"}}
	if _, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 0, 5, opts); err == nil {
		t.Fatal("malformed geometry was returned or silently skipped")
	}
	reader, err := plugin.OpenTableReader(context.Background(), bytes.NewReader(data), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(context.Background())
	if _, err := reader.ReadRows(context.Background(), 5); err == nil {
		t.Fatal("continuous reader accepted malformed geometry")
	}
	opts.GeometryEncoding = format.GeometryEncodingShapefileShape
	if _, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 0, 5, opts); err == nil || !strings.Contains(err.Error(), "unsupported geometry encoding") {
		t.Fatalf("unsupported native encoding error = %v", err)
	}
}

func geometryReadDatabase(t *testing.T, geopackage, broken bool) ([]byte, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.gpkg")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if geopackage {
		for _, stmt := range []string{
			`CREATE TABLE gpkg_contents(table_name TEXT, data_type TEXT, identifier TEXT,srs_id INTEGER,min_x DOUBLE,min_y DOUBLE,max_x DOUBLE,max_y DOUBLE)`,
			`CREATE TABLE gpkg_geometry_columns(table_name TEXT,column_name TEXT,geometry_type_name TEXT,srs_id INTEGER)`,
			`INSERT INTO gpkg_contents VALUES('routes','features','Route Layer',3857,0.5,0,10,0)`,
			`INSERT INTO gpkg_geometry_columns VALUES('routes','route_shape','LINESTRING',3857)`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec(`CREATE TABLE routes(fid INTEGER PRIMARY KEY,route_shape BLOB,geom BLOB,name TEXT)`); err != nil {
		t.Fatal(err)
	}
	native := testGeoPackageBinary(t, geom.NewLineStringFlat(geom.XY, []float64{0.5, 0, 10, 0}), false)
	geometry := native
	if broken {
		geometry = []byte{0}
	}
	for i, value := range []interface{}{geometry, nil, testGeoPackageBinary(t, geom.NewLineString(geom.XY), true)} {
		if _, err := db.Exec(`INSERT INTO routes VALUES(?,?,?,'kept')`, i+1, value, native); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data, native
}

func testGeoPackageBinary(t *testing.T, shape geom.T, empty bool) []byte {
	t.Helper()
	header := make([]byte, 40)
	copy(header, "GP")
	header[3] = 3
	binary.LittleEndian.PutUint32(header[4:8], 3857)
	binary.LittleEndian.PutUint64(header[8:16], math.Float64bits(0.5))
	binary.LittleEndian.PutUint64(header[16:24], math.Float64bits(10))
	if empty {
		header = header[:8]
		header[3] = 17
	}
	payload, err := wkb.Marshal(shape, wkb.NDR)
	if err != nil {
		t.Fatal(err)
	}
	return append(header, payload...)
}
