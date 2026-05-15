package plugin_test

import (
	"reflect"
	"testing"

	. "github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/format/builtin"
)

func TestCapabilityBuildersUseFormatRegistry(t *testing.T) {
	tests := []struct {
		name string
		caps EngineCapabilities
		want []string
	}{
		{
			name: "tabular",
			caps: NewTabularCapabilities("postgresql", "schema", TabularCapabilityOptions{}),
			want: []string{"table"},
		},
		{
			name: "object",
			caps: NewObjectCapabilities("minio"),
			want: []string{"avro", "csv", "json", "markdown", "orc", "parquet", "shapefile", "text", "tsv"},
		},
		{
			name: "file",
			caps: NewFileCapabilities("nfs"),
			want: []string{"avro", "csv", "json", "markdown", "orc", "parquet", "shapefile", "text", "tsv"},
		},
		{
			name: "document",
			caps: NewDocumentCapabilities("mongodb"),
			want: []string{"document", "json", "markdown", "text"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.caps.Transfer == nil {
				t.Fatal("expected transfer capabilities")
			}
			if !reflect.DeepEqual(tt.caps.Transfer.SupportedFormats, tt.want) {
				t.Fatalf("SupportedFormats = %#v, want %#v", tt.caps.Transfer.SupportedFormats, tt.want)
			}
		})
	}
}
