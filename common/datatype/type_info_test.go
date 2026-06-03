package datatype

import "testing"

func TestTypeInfoDataTypes(t *testing.T) {
	tests := []struct {
		name string
		info TypeInfo
		want DataType
	}{
		{name: "table", info: &TableInfo{}, want: Table},
		{name: "document", info: &DocumentInfo{}, want: Document},
		{name: "media", info: &MediaInfo{}, want: Media},
		{name: "container", info: &ContainerInfo{}, want: Container},
		{name: "graph", info: &GraphInfo{}, want: Graph},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.TypeInfoDataType(); got != tt.want {
				t.Fatalf("TypeInfoDataType() = %q, want %q", got, tt.want)
			}
		})
	}
}
