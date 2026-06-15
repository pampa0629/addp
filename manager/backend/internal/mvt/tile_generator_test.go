package mvt

import "testing"

func TestMVTBufferForExtent(t *testing.T) {
	tests := []struct {
		name   string
		extent int
		want   int
	}{
		{name: "default", extent: 1024, want: 32},
		{name: "half", extent: 512, want: 16},
		{name: "minimum", extent: 256, want: 8},
		{name: "lower bound", extent: 128, want: 8},
		{name: "upper bound", extent: 4096, want: 64},
		{name: "invalid", extent: 0, want: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mvtBufferForExtent(tt.extent); got != tt.want {
				t.Fatalf("mvtBufferForExtent(%d) = %d, want %d", tt.extent, got, tt.want)
			}
		})
	}
}
