package format_test

import (
	"testing"

	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestDefaultWriteExtensionUsesFormatDescriptor(t *testing.T) {
	tests := []struct {
		formatType format.FormatType
		want       string
	}{
		{format.FormatCSV, ".csv"},
		{format.FormatTSV, ".tsv"},
		{format.FormatGeoJSON, ".geojson"},
		{format.FormatParquet, ".parquet"},
		{format.FormatShapefile, ".shp"},
	}

	for _, tt := range tests {
		t.Run(string(tt.formatType), func(t *testing.T) {
			if got := format.DefaultWriteExtension(tt.formatType, nil); got != tt.want {
				t.Fatalf("DefaultWriteExtension(%q) = %q, want %q", tt.formatType, got, tt.want)
			}
		})
	}
}

func TestDefaultWriteExtensionRefinesJSONWriterOptions(t *testing.T) {
	tests := []struct {
		name    string
		options *format.WriteOptions
		want    string
	}{
		{name: "default json", options: nil, want: ".json"},
		{name: "json lines", options: &format.WriteOptions{ExtraParams: map[string]interface{}{"json_mode": "jsonl"}}, want: ".jsonl"},
		{name: "ndjson", options: &format.WriteOptions{ExtraParams: map[string]interface{}{"json_mode": "ndjson"}}, want: ".jsonl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := format.DefaultWriteExtension(format.FormatJSON, tt.options); got != tt.want {
				t.Fatalf("DefaultWriteExtension(json) = %q, want %q", got, tt.want)
			}
		})
	}
}
