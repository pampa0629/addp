package capability

import (
	"reflect"
	"testing"
)

func TestListSorted(t *testing.T) {
	capabilities := List()
	if len(capabilities) == 0 {
		t.Fatal("expected built-in format capabilities")
	}

	for i := 1; i < len(capabilities); i++ {
		if capabilities[i-1].Format > capabilities[i].Format {
			t.Fatalf("format capabilities are not sorted: %s before %s", capabilities[i-1].Format, capabilities[i].Format)
		}
	}
}

func TestListTransferFormatsForEngineFamily(t *testing.T) {
	tests := []struct {
		name         string
		engineFamily string
		want         []string
	}{
		{
			name:         "tabular",
			engineFamily: EngineFamilyTabular,
			want:         []string{"table"},
		},
		{
			name:         "object",
			engineFamily: EngineFamilyObject,
			want:         []string{"csv", "geojson", "json", "parquet", "shapefile"},
		},
		{
			name:         "file",
			engineFamily: EngineFamilyFile,
			want:         []string{"csv", "geojson", "json", "parquet", "shapefile"},
		},
		{
			name:         "document",
			engineFamily: EngineFamilyDocument,
			want:         []string{"document", "json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ListTransferFormatsForEngineFamily(tt.engineFamily)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ListTransferFormatsForEngineFamily(%q) = %#v, want %#v", tt.engineFamily, got, tt.want)
			}
		})
	}
}

func TestGetReturnsClone(t *testing.T) {
	capability, ok := Get(FormatCSV)
	if !ok {
		t.Fatal("expected csv capability")
	}
	capability.Extensions[0] = ".changed"

	got, ok := Get(FormatCSV)
	if !ok {
		t.Fatal("expected csv capability")
	}
	if got.Extensions[0] == ".changed" {
		t.Fatal("format capability registry returned mutable internal slices")
	}
}
