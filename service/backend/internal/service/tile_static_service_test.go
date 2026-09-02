package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/service/internal/models"
)

type unusedStaticTileEngineClient struct{}

func (unusedStaticTileEngineClient) GetEngineForTenant(context.Context, uint, uint) (*commonModels.Engine, error) {
	return nil, nil
}

func TestStaticTileOutsideArchiveRangeReturnsEmptyGzipMVT(t *testing.T) {
	reader := NewStaticTileService(unusedStaticTileEngineClient{})
	layer := &models.TileServiceLayer{LayerConfig: map[string]interface{}{
		"source": map[string]interface{}{
			"locator": "addp://engine/9/path/addp/vector-tiles/farmland.pmtiles?type=object&item_id=51561",
		},
		"source_snapshot": map[string]interface{}{
			"tile_format": "mvt",
			"min_zoom":    4,
			"max_zoom":    12,
		},
	}}

	tile, err := reader.GetStaticTile(context.Background(), 1, layer, 3, 0, 0, "mvt")
	if err != nil {
		t.Fatalf("GetStaticTile() error = %v", err)
	}
	if tile.ContentType != "application/vnd.mapbox-vector-tile" || tile.ContentEncoding != "gzip" {
		t.Fatalf("tile headers = %#v", tile)
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(tile.Data))
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	decompressed, err := io.ReadAll(gzipReader)
	if err != nil {
		t.Fatalf("read empty MVT: %v", err)
	}
	if err := gzipReader.Close(); err != nil {
		t.Fatalf("close gzip reader: %v", err)
	}
	if len(decompressed) != 0 {
		t.Fatalf("decompressed tile length = %d, want 0", len(decompressed))
	}
}
