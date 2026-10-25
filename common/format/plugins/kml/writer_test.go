package kml

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
)

func fixtureTable() *datatype.TableInfo {
	return &datatype.TableInfo{Name: "Layer #0 中文 &", Fields: []datatype.FieldInfo{
		{Name: "seq", Type: datatype.FieldTypeInt}, {Name: "标题", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "flag", Type: datatype.FieldTypeBool}, {Name: "measure", Type: datatype.FieldTypeDouble},
		{Name: "route_shape", Type: datatype.FieldTypeGeometry, Nullable: true},
	}}
}
func fixtureOptions() *format.WriteOptions {
	return &format.WriteOptions{SpatialInfo: datatype.NewSingleGeometrySpatialInfo("route_shape", "Geometry", 4326, 2)}
}
func TestKMLWriterGoldenNativeInteroperability(t *testing.T) {
	var output bytes.Buffer
	p := NewPlugin()
	var _ format.TableWriterProvider = p
	var _ format.RuntimeContainerInfoProviderFactory = p
	w, err := p.OpenTableWriter(t.Context(), &output, fixtureTable(), fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	shapes := []interface{}{
		"POINT (120 30)", "LINESTRING (120 30,121 31)",
		"POLYGON ((0 0,4 0,4 4,0 4,0 0),(1 1,1 2,2 2,2 1,1 1))",
		"MULTIPOINT ((1 2),(3 4))", "MULTILINESTRING ((1 2,3 4),(5 6,7 8))",
		"MULTIPOLYGON (((0 0,1 0,1 1,0 0)))", "GEOMETRYCOLLECTION (POINT (1 2),LINESTRING (3 4,5 6))", nil,
	}
	for i, shape := range shapes {
		var title interface{} = "中文 <&> \"quote\""
		if i == 1 {
			title = ""
		}
		if i == 2 {
			title = nil
		}
		if err = w.WriteRows(t.Context(), []map[string]interface{}{{"seq": i, "标题": title, "flag": i%2 == 0, "measure": 1.25, "route_shape": shape}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/simple-features.kml")
	if err != nil {
		t.Fatalf("golden fixture missing: %s", output.String())
	}
	if !bytes.Equal(expected, output.Bytes()) {
		t.Fatalf("golden KML differs:\n%s", output.String())
	}
	// Independent XML decoding also checks the namespace and escaped attributes.
	var parsed struct {
		XMLName  xml.Name
		Document struct {
			Folder struct {
				Name string     `xml:"name"`
				Rows []struct{} `xml:"Placemark"`
			} `xml:"Folder"`
		} `xml:"Document"`
	}
	if err = xml.Unmarshal(output.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.XMLName.Space != "http://www.opengis.net/kml/2.2" || parsed.Document.Folder.Name != fixtureTable().Name || len(parsed.Document.Folder.Rows) != 8 {
		t.Fatalf("decoded: %#v", parsed)
	}
}
func TestKMLSchemaAndCRSRejectBeforeHeader(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*datatype.TableInfo, *format.WriteOptions)
	}{
		{"unknown CRS", func(_ *datatype.TableInfo, o *format.WriteOptions) {
			o.SpatialInfo = datatype.NewSingleGeometrySpatialInfo("route_shape", "Point", 0, 2)
		}},
		{"projected CRS", func(_ *datatype.TableInfo, o *format.WriteOptions) {
			o.SpatialInfo = datatype.NewSingleGeometrySpatialInfo("route_shape", "Point", 3857, 2)
		}},
		{"conflicting CRS", func(_ *datatype.TableInfo, o *format.WriteOptions) { o.SpatialInfo.CRSRef = "EPSG:3857" }},
		{"Z metadata", func(_ *datatype.TableInfo, o *format.WriteOptions) {
			o.SpatialInfo = datatype.NewSingleGeometrySpatialInfo("route_shape", "Point", 4326, 3)
		}},
		{"duplicate fields", func(ti *datatype.TableInfo, _ *format.WriteOptions) { ti.Fields = append(ti.Fields, ti.Fields[0]) }},
		{"missing geometry", func(ti *datatype.TableInfo, _ *format.WriteOptions) { ti.Fields = ti.Fields[:4] }},
		{"invalid text", func(ti *datatype.TableInfo, _ *format.WriteOptions) { ti.Name = "bad\x00" }},
		{"resume", func(_ *datatype.TableInfo, o *format.WriteOptions) { o.ResumeMarker = &resume.Marker{} }},
	}
	for _, kind := range []datatype.FieldType{datatype.FieldTypeBigInt, datatype.FieldTypeDecimal, datatype.FieldTypeBytes, datatype.FieldTypeDate, datatype.FieldTypeTime, datatype.FieldTypeTimestamp, datatype.FieldTypeJSON, datatype.FieldTypeArray, datatype.FieldTypeUnknown, datatype.FieldTypeUUID} {
		tests = append(tests, struct {
			name   string
			mutate func(*datatype.TableInfo, *format.WriteOptions)
		}{string(kind), func(ti *datatype.TableInfo, _ *format.WriteOptions) { ti.Fields[0].Type = kind }})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			ti, o := fixtureTable(), fixtureOptions()
			tt.mutate(ti, o)
			if _, err := NewPlugin().OpenTableWriter(t.Context(), &out, ti, o); err == nil {
				t.Fatal("expected rejection")
			}
			if out.Len() != 0 {
				t.Fatal("schema rejection wrote header")
			}
		})
	}
}
func TestKMLRowsRejectWithoutCompletingArtifact(t *testing.T) {
	for name, change := range map[string]func(map[string]interface{}){
		"wrong scalar": func(r map[string]interface{}) { r["seq"] = "1" }, "overflow": func(r map[string]interface{}) { r["seq"] = int64(math.MaxInt32) + 1 },
		"fraction": func(r map[string]interface{}) { r["seq"] = 1.5 }, "nan": func(r map[string]interface{}) { r["measure"] = math.NaN() },
		"invalid XML": func(r map[string]interface{}) { r["标题"] = "bad\x00" }, "invalid UTF8": func(r map[string]interface{}) { r["标题"] = string([]byte{0xff}) },
		"unknown column": func(r map[string]interface{}) { r["secret"] = "value" }, "required null": func(r map[string]interface{}) { r["seq"] = nil },
		"Z geometry": func(r map[string]interface{}) { r["route_shape"] = "POINT Z (1 2 3)" }, "M geometry": func(r map[string]interface{}) { r["route_shape"] = "POINT M (1 2 3)" },
		"range": func(r map[string]interface{}) { r["route_shape"] = "POINT (181 0)" }, "empty": func(r map[string]interface{}) { r["route_shape"] = "POINT EMPTY" },
		"bad polygon": func(r map[string]interface{}) { r["route_shape"] = "POLYGON ((0 0,1 0,1 1,2 2))" },
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			w, err := NewPlugin().OpenTableWriter(t.Context(), &out, fixtureTable(), fixtureOptions())
			if err != nil {
				t.Fatal(err)
			}
			headerLen := out.Len()
			row := map[string]interface{}{"seq": 1, "标题": "ok", "flag": true, "measure": 1.25, "route_shape": "POINT (120 30)"}
			change(row)
			if err = w.WriteRows(t.Context(), []map[string]interface{}{row}); err == nil {
				t.Fatal("expected row rejection")
			}
			if out.Len() != headerLen {
				t.Fatal("invalid row partially written")
			}
			if w.Close(t.Context()) == nil || w.WriteRows(t.Context(), nil) == nil {
				t.Fatal("failed writer recovered")
			}
			if strings.HasSuffix(out.String(), "</kml>") {
				t.Fatal("failed artifact finalized")
			}
		})
	}
}
func TestKMLCancelledAndBrokenOutputs(t *testing.T) {
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewPlugin().OpenTableWriter(ctx, &out, fixtureTable(), fixtureOptions()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, sink := range []io.Writer{shortSink{}, brokenSink{}} {
		if _, err := NewPlugin().OpenTableWriter(t.Context(), sink, fixtureTable(), fixtureOptions()); err == nil {
			t.Fatal("write error ignored")
		}
	}
	w, err := NewPlugin().OpenTableWriter(t.Context(), &out, fixtureTable(), fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err = w.WriteRows(ctx, []map[string]interface{}{{}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !errors.Is(w.Close(t.Context()), context.Canceled) {
		t.Fatal("cancelled writer finalized")
	}
}

type shortSink struct{}

func (shortSink) Write(p []byte) (int, error) { return len(p) - 1, nil }

type brokenSink struct{}

func (brokenSink) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestKMLFloatAndCRS84WriterLifecycle(t *testing.T) {
	var output bytes.Buffer
	ti, options := fixtureTable(), fixtureOptions()
	ti.Fields[3].Type = datatype.FieldTypeFloat
	options.SpatialInfo.GeometryColumns[0].SRID = nil
	options.SpatialInfo.GeometryColumns[0].CRSRef = "OGC:CRS84"
	w, err := NewPlugin().OpenTableWriter(t.Context(), &output, ti, options)
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]interface{}{"seq": int32(-2147483648), "标题": "", "flag": false, "measure": float32(0.25), "route_shape": "POINT (-180 -90)"}
	if err = w.WriteRows(t.Context(), []map[string]interface{}{row}); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = w.WriteRows(t.Context(), []map[string]interface{}{row}); err == nil {
		t.Fatal("write after close")
	}
	if !strings.Contains(output.String(), `type="float"`) {
		t.Fatal(output.String())
	}
	var aborted bytes.Buffer
	aw, err := NewPlugin().OpenTableWriter(t.Context(), &aborted, ti, options)
	if err != nil {
		t.Fatal(err)
	}
	if err = aw.(format.AbortableTableWriter).Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	if aw.Close(t.Context()) == nil || strings.HasSuffix(aborted.String(), "</kml>") {
		t.Fatal("aborted document completed")
	}
}

func TestKMLBooleanUsesNumericXMLLiterals(t *testing.T) {
	for _, tc := range []struct {
		value interface{}
		want  string
	}{{true, "1"}, {false, "0"}} {
		got, err := scalarText(datatype.FieldTypeBool, tc.value)
		if err != nil || got != tc.want {
			t.Fatalf("bool %v encoded as %q: %v", tc.value, got, err)
		}
	}
	for _, value := range []interface{}{"true", "false", "1", "0", 0, 1, 2} {
		if _, err := scalarText(datatype.FieldTypeBool, value); err == nil {
			t.Fatalf("non-boolean input accepted: %T(%v)", value, value)
		}
	}
}
