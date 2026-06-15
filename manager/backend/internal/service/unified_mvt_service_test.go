package service

import (
	"context"
	"testing"
)

func TestUnifiedMVTServiceReturnsEmptyTileWithoutRealtimeTarget(t *testing.T) {
	svc := NewUnifiedMVTService(nil, nil, nil)
	tenantID := uint(1)

	response, err := svc.GetTile(
		context.Background(),
		&tenantID,
		8,
		"public",
		"test",
		"SmGeometry",
		nil,
		10, 856, 419,
		4549,
		nil,
	)
	if err != nil {
		t.Fatalf("GetTile returned error: %v", err)
	}
	if response == nil {
		t.Fatal("GetTile returned nil response")
	}
	if len(response.Data) != 0 {
		t.Fatalf("tile data length = %d, want empty tile", len(response.Data))
	}
	if response.RenderSource != QuickViewRenderSourceRealtimeTile {
		t.Fatalf("render_source = %s, want %s", response.RenderSource, QuickViewRenderSourceRealtimeTile)
	}
	if response.Status != TileStatusDegraded {
		t.Fatalf("status = %s, want %s", response.Status, TileStatusDegraded)
	}
}

func TestTileStatusForData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "empty", data: []byte{}, want: TileStatusEmpty},
		{name: "non empty", data: []byte{1, 2, 3}, want: TileStatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tileStatusForData(tt.data); got != tt.want {
				t.Fatalf("tileStatusForData() = %s, want %s", got, tt.want)
			}
		})
	}
}
