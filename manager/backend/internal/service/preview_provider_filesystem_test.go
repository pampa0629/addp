package service

import "testing"

func TestNFSPhysicalPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		table  string
		want   string
	}{
		{name: "root", want: "/"},
		{name: "root file", table: "README.md", want: "/README.md"},
		{name: "directory", schema: "gis-data", want: "/gis-data"},
		{name: "nested file", schema: "gis-data", table: "sample.csv", want: "/gis-data/sample.csv"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := nfsPhysicalPath(tt.schema, tt.table); got != tt.want {
				t.Fatalf("nfsPhysicalPath(%q, %q) = %q, want %q", tt.schema, tt.table, got, tt.want)
			}
		})
	}
}
