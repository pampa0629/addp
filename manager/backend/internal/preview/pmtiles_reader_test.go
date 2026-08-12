package preview

import (
	"bytes"
	"compress/gzip"
	"context"
	"testing"

	commonPMTiles "github.com/addp/common/format/pmtiles"
)

func TestReadPMTilesTileFromRange(t *testing.T) {
	writer, err := commonPMTiles.NewWriter(commonPMTiles.WriterOptions{Bounds: [4]float64{116, 31, 122, 40}, MinZoom: 3, MaxZoom: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var want bytes.Buffer
	gz := gzip.NewWriter(&want)
	_, _ = gz.Write([]byte("business tile"))
	_ = gz.Close()
	if err := writer.AddTile(3, 3, 3, want.Bytes()); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err := writer.WriteTo(&archive); err != nil {
		t.Fatal(err)
	}
	data, err := readPMTilesTileFromRange(context.Background(), int64(archive.Len()), 3, 3, 3, func(_ context.Context, offset, length int64) ([]byte, error) {
		return append([]byte(nil), archive.Bytes()[offset:offset+length]...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want.Bytes()) {
		t.Fatalf("tile = %x, want %x", data, want.Bytes())
	}
}
