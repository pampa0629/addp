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
