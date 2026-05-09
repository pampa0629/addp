package plugin

import (
	"reflect"
	"testing"
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
			want: []string{"csv", "json", "parquet", "shapefile"},
		},
		{
			name: "file",
			caps: NewFileCapabilities("nfs"),
			want: []string{"csv", "json", "parquet", "shapefile"},
		},
		{
			name: "document",
			caps: NewDocumentCapabilities("mongodb"),
			want: []string{"document", "json"},
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
