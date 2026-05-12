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
