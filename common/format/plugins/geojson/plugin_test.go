package geojsonformat

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestGeoJSONPluginImplementsTargetInterfaces(t *testing.T) {
	plugin := NewPlugin(nil)
	var _ format.FormatPlugin = plugin
	var _ format.FormatDescriptorProvider = plugin
	var _ format.ContentSniffer = plugin
	var _ format.FormatInfoProvider = plugin
	var _ format.TableInfoProvider = plugin
	var _ format.TableSampleReader = plugin
	var _ format.TableReaderProvider = plugin
	var _ format.TableWriterProvider = plugin
}

func TestGeoJSONPluginDescriptor(t *testing.T) {
	descriptor := NewPlugin(nil).Descriptor()
	if descriptor.Format != format.FormatGeoJSON || descriptor.DataType != datatype.Table {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if len(descriptor.Identification.Extensions) != 1 || descriptor.Identification.Extensions[0] != ".geojson" {
		t.Fatalf("extensions = %#v", descriptor.Identification.Extensions)
	}
}

func TestGeoJSONPluginDescribeAndSample(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A","count":1}},
			{"type":"Feature","geometry":{"type":"Point","coordinates":[3,4]},"properties":{"name":"B","count":2}}
		]
	}`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.Table.RowCount == nil || *info.Table.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.Table.RowCount)
	}
	if info.Spatial == nil {
		t.Fatalf("spatial extension missing")
	}
	if field := info.Table.GetField("geometry"); field == nil || field.Type != datatype.FieldTypeGeometry {
		t.Fatalf("geometry field = %#v", field)
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 1, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "B" {
		t.Fatalf("rows = %#v, want second feature", rows)
	}
}

func TestGeoJSONPluginDoesNotInventSpatialInfoWithoutGeometry(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","properties":{"name":"A","count":1}},
			{"type":"Feature","properties":{"name":"B","count":2}}
		]
	}`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.Spatial != nil {
		t.Fatalf("spatial extension should be absent: %#v", info.Spatial)
	}
	if field := info.Table.GetField("geometry"); field != nil {
		t.Fatalf("geometry field should be absent: %#v", field)
	}
}

func TestGeoJSONPluginRejectsPlainJSONRecords(t *testing.T) {
	_, err := NewPlugin(nil).DescribeTable(context.Background(), strings.NewReader(`[{"id":1}]`), nil)
	if err == nil {
		t.Fatal("DescribeTable succeeded for plain JSON records")
	}
}

func TestGeoJSONPluginFieldSelectionPrunesSpatialInfoWhenGeometryExcluded(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A"}}
		]
	}`
	opts := format.DefaultParseOptions()
	opts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"name"}}

	info, err := NewPlugin(nil).DescribeTable(context.Background(), strings.NewReader(data), opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.Spatial != nil {
		t.Fatalf("spatial info = %#v, want nil when geometry is excluded", info.Spatial)
	}
	if field := info.Table.GetField("geometry"); field != nil {
		t.Fatalf("geometry field = %#v, want pruned", field)
	}
}

func TestGeoJSONPluginOpenTableWriter(t *testing.T) {
	plugin := NewPlugin(nil)
	opts := format.DefaultWriteOptions()
	opts.ExtraParams = map[string]interface{}{"geometry_field": "geom"}
	opts.SpatialInfo = datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 0)
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString},
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
	}
	var buf bytes.Buffer

	writer, err := plugin.OpenTableWriter(context.Background(), &buf, tableInfo, opts)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "A", "geom": map[string]interface{}{"type": "Point", "coordinates": []interface{}{float64(1), float64(2)}}},
		{"id": 2, "name": "B", "geom": `{"type":"Point","coordinates":[3,4]}`},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	var collection map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &collection); err != nil {
		t.Fatalf("unmarshal GeoJSON failed: %v; output=%s", err, buf.String())
	}
	if collection["type"] != "FeatureCollection" {
		t.Fatalf("collection type = %#v, want FeatureCollection", collection["type"])
	}
	features, ok := collection["features"].([]interface{})
	if !ok || len(features) != 2 {
		t.Fatalf("features = %#v, want 2 features", collection["features"])
	}
	first := features[0].(map[string]interface{})
	if first["type"] != "Feature" || first["id"].(float64) != 1 {
		t.Fatalf("first feature = %#v", first)
	}
	props := first["properties"].(map[string]interface{})
	if props["name"] != "A" {
		t.Fatalf("properties = %#v, want name A", props)
	}
	if _, ok := props["geom"]; ok {
		t.Fatalf("geometry field leaked into properties: %#v", props)
	}
	geom := first["geometry"].(map[string]interface{})
	if geom["type"] != "Point" {
		t.Fatalf("geometry = %#v, want Point", geom)
	}
}

func TestGeoJSONPluginComputesBoundingBoxWithoutFileBBox(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","geometry":{"type":"LineString","coordinates":[[3,4],[-1,7],[5,-2]]},"properties":{"name":"A"}},
			{"type":"Feature","geometry":{"type":"Point","coordinates":[8,6]},"properties":{"name":"B"}}
		]
	}`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	spatial := info.Spatial
	if spatial == nil || spatial.Extent == nil {
		t.Fatalf("spatial bbox missing: %#v", spatial)
	}
	want := datatype.BoundingBox{-1, -2, 8, 7}
	if got := *spatial.Extent; got != want {
		t.Fatalf("bbox = %#v, want %#v", got, want)
	}
	if srid := spatial.PrimarySRIDValue(); srid != 4326 {
		t.Fatalf("GeoJSON SRID = %d, want 4326", srid)
	}

	formatInfo, err := plugin.DescribeFormat(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeFormat failed: %v", err)
	}
	if formatInfo["bbox"] != nil {
		t.Fatalf("format info should not contain computed bbox: %#v", formatInfo)
	}
}

func TestGeoJSONPluginDescribeFormatKeepsExplicitBBox(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"bbox": [-1, -2, 8, 7],
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A"}}
		]
	}`
	plugin := NewPlugin(nil)

	formatInfo, err := plugin.DescribeFormat(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeFormat failed: %v", err)
	}
	if got, want := formatInfo["bbox"], []float64{-1, -2, 8, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("format bbox = %#v, want %#v", got, want)
	}
}

func TestGeoJSONPluginSampleFromPositionedReader(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A"}},
			{"type":"Feature","geometry":{"type":"Point","coordinates":[3,4]},"properties":{"name":"B"}},
			{"type":"Feature","geometry":{"type":"Point","coordinates":[5,6]},"properties":{"name":"C"}}
		]
	}`
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.AccessIndexStep = 1

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	index := info.AccessIndex
	start := index.Anchors[1].ByteOffset
	tableInfo := format.TableInfoFromDescribeResult(info)
	positioned := format.DefaultParseOptions()
	positioned.TableSample = &format.TableSampleOptions{
		Fields:            tableInfo.Fields,
		InputStartsAtRow:  index.Anchors[1].Row,
		InputIsPositioned: true,
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data[start:]), 2, 1, positioned)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "C" {
		t.Fatalf("rows = %#v, want C", rows)
	}
	if _, ok := rows[0]["properties"]; ok {
		t.Fatalf("positioned row should be flattened properties, got %#v", rows[0])
	}
	if geom, ok := rows[0]["geometry"].(map[string]interface{}); !ok || geom["type"] != "Point" {
		t.Fatalf("positioned row geometry = %#v", rows[0]["geometry"])
	}
}
