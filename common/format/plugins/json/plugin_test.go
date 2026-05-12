package jsonformat

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestJSONPluginImplementsTargetInterfaces(t *testing.T) {
	plugin := NewPlugin(nil)
	var _ format.FormatPlugin = plugin
	var _ format.FormatInfoProvider = plugin
	var _ format.DocumentInfoProvider = plugin
	var _ format.TableInfoProvider = plugin
	var _ format.TableSampleReader = plugin
}

func TestJSONPluginDescribeAndSampleGeoJSON(t *testing.T) {
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
	if info.RowCount == nil || *info.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.RowCount)
	}
	if info.GetSpatialInfo() == nil {
		t.Fatalf("spatial extension missing")
	}
	if field := info.GetField("geometry"); field == nil || field.Type != format.FieldTypeGeometry {
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

func TestJSONPluginDoesNotInventSpatialInfoWithoutGeometry(t *testing.T) {
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
	if info.GetSpatialInfo() != nil {
		t.Fatalf("spatial extension should be absent: %#v", info.GetSpatialInfo())
	}
	if field := info.GetField("geometry"); field != nil {
		t.Fatalf("geometry field should be absent: %#v", field)
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 0, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if _, ok := rows[0]["geometry"]; ok {
		t.Fatalf("row should not contain geometry: %#v", rows[0])
	}
}

func TestJSONPluginDescribeFormatDistinguishesDocumentAndFeatureCollection(t *testing.T) {
	plugin := NewPlugin(nil)

	docInfo, err := plugin.DescribeFormat(context.Background(), strings.NewReader(`{"name":"A"}`), nil)
	if err != nil {
		t.Fatalf("DescribeFormat(document) failed: %v", err)
	}
	if docInfo["structure"] != StructureDocument {
		t.Fatalf("document structure = %#v", docInfo)
	}

	fcInfo, err := plugin.DescribeFormat(context.Background(), strings.NewReader(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{}}]}`), nil)
	if err != nil {
		t.Fatalf("DescribeFormat(feature collection) failed: %v", err)
	}
	if fcInfo["structure"] != StructureGeoJSONFeatureSet || fcInfo["has_geometry"] != true {
		t.Fatalf("feature collection info = %#v", fcInfo)
	}
}

func TestJSONPluginDescribeAndSampleObjectArray(t *testing.T) {
	data := `[
		{"id":"1","name":"A","area":"356.16704388138885"},
		{"id":"2","name":"B","area":"129.1114944814742"}
	]`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.RowCount == nil || *info.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.RowCount)
	}
	if info.GetSpatialInfo() != nil {
		t.Fatalf("spatial extension should be absent: %#v", info.GetSpatialInfo())
	}
	for _, name := range []string{"id", "name", "area"} {
		if field := info.GetField(name); field == nil {
			t.Fatalf("field %q missing: %#v", name, info.Fields)
		}
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 1, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "B" {
		t.Fatalf("rows = %#v, want second object", rows)
	}
}

func TestJSONPluginObjectArrayDetectsVerifiedWKBGeometry(t *testing.T) {
	data := `[
		{
			"id":"1",
			"SmGeometry":"0101000000000000000000F03F0000000000000040",
			"name":"A"
		}
	]`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	spatial := info.GetSpatialInfo()
	if spatial == nil {
		t.Fatalf("spatial extension missing")
	}
	if spatial.GeometryColumn != "SmGeometry" || spatial.GeometryType != "Point" {
		t.Fatalf("spatial = %#v", spatial)
	}
	if field := info.GetField("SmGeometry"); field == nil || field.Type != format.FieldTypeGeometry {
		t.Fatalf("geometry field = %#v", field)
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 0, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	geom, ok := rows[0]["SmGeometry"].(map[string]interface{})
	coords, _ := geom["coordinates"].([]interface{})
	if !ok || geom["type"] != "Point" || geom["wkb"] == "" || len(coords) != 2 {
		t.Fatalf("geometry row value = %#v", rows[0]["SmGeometry"])
	}
}

func TestJSONPluginDescribeTableBuildsSparseRowIndex(t *testing.T) {
	data := `[
		{"id":1,"name":"A"},
		{"id":2,"name":"B"},
		{"id":3,"name":"C"},
		{"id":4,"name":"D"}
	]`
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.ContentIndexStep = 2

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	indexInfo := info.GetContentIndexInfo()
	if indexInfo == nil || indexInfo.Table == nil {
		t.Fatalf("content index missing")
	}
	index := indexInfo.Table
	if index.Kind != format.ContentIndexKindSparseRow || index.RowCount != 4 || len(index.Anchors) != 3 {
		t.Fatalf("index = %#v", index)
	}
	if index.Anchors[1].Row != 2 || index.Anchors[1].ByteOffset <= index.Anchors[0].ByteOffset {
		t.Fatalf("anchors = %#v", index.Anchors)
	}
}

func TestJSONPluginSampleTableFromPositionedReader(t *testing.T) {
	data := `[
		{"id":1,"name":"A"},
		{"id":2,"name":"B"},
		{"id":3,"name":"C"},
		{"id":4,"name":"D"}
	]`
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.ContentIndexStep = 2

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	index := info.GetContentIndexInfo().Table
	start := index.Anchors[1].ByteOffset
	fragment := data[start:]
	positioned := format.DefaultParseOptions()
	positioned.TableSample = &format.TableSampleOptions{
		Fields:            info.Fields,
		InputStartsAtRow:  index.Anchors[1].Row,
		InputIsPositioned: true,
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(fragment), 3, 1, positioned)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "D" {
		t.Fatalf("rows = %#v, want D", rows)
	}
}
