package datatype

import "testing"

func TestTypeInfoDataTypes(t *testing.T) {
	tests := []struct {
		name string
		info TypeInfo
		want DataType
	}{
		{name: "table", info: &TableInfo{}, want: DataTypeTable},
		{name: "document", info: &DocumentInfo{}, want: DataTypeDocument},
		{name: "media", info: &MediaInfo{}, want: DataTypeMedia},
		{name: "container", info: &ContainerInfo{}, want: DataTypeContainer},
		{name: "graph", info: &GraphInfo{}, want: DataTypeGraph},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.TypeInfoDataType(); got != tt.want {
				t.Fatalf("TypeInfoDataType() = %q, want %q", got, tt.want)
			}
		})
	}
}
