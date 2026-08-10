package pmtiles

import (
	"bytes"
	"compress/gzip"
	"context"
	"testing"
)

func TestWriterProducesReadablePMTilesV3Archive(t *testing.T) {
	writer, err := NewWriter(WriterOptions{
		Bounds:   [4]float64{110, 20, 120, 30},
		MinZoom:  0,
		MaxZoom:  1,
		Metadata: map[string]interface{}{"name": "roads", "format": "pbf"},
	})
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	defer writer.Close()

	tiles := []struct {
		z uint8
		x uint32
		y uint32
		v string
	}{
		{z: 0, x: 0, y: 0, v: "root"},
		{z: 1, x: 0, y: 0, v: "north-west"},
		{z: 1, x: 1, y: 1, v: "south-east"},
	}
	compressed := make(map[[3]uint32][]byte, len(tiles))
	for _, tile := range tiles {
		data := gzipBytes(t, []byte(tile.v))
		compressed[[3]uint32{uint32(tile.z), tile.x, tile.y}] = data
		if err := writer.AddTile(tile.z, tile.x, tile.y, data); err != nil {
			t.Fatalf("AddTile(%d/%d/%d) error = %v", tile.z, tile.x, tile.y, err)
		}
	}

	var archiveBytes bytes.Buffer
	header, err := writer.WriteTo(&archiveBytes)
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if header.SpecVersion != 3 || header.TileType != TileTypeMVT || header.TileCompression != CompressionGzip {
		t.Fatalf("header = %#v, want PMTiles v3 gzip MVT", header)
	}
	if err := ValidateHeader(header, int64(archiveBytes.Len())); err != nil {
		t.Fatalf("ValidateHeader() error = %v", err)
	}
	archive, err := NewArchive(header, func(_ context.Context, offset, length int64) ([]byte, error) {
		return append([]byte(nil), archiveBytes.Bytes()[offset:offset+length]...), nil
	})
	if err != nil {
		t.Fatalf("NewArchive() error = %v", err)
	}
	if err := archive.ValidateDirectories(context.Background()); err != nil {
		t.Fatalf("ValidateDirectories() error = %v", err)
	}
	for key, want := range compressed {
		got, err := archive.GetTile(context.Background(), uint8(key[0]), key[1], key[2])
		if err != nil {
			t.Fatalf("GetTile(%v) error = %v", key, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("GetTile(%v) = %x, want %x", key, got, want)
		}
	}
}

func TestWriterSupportsUnclusteredTilesAndRejectsUncompressedMVT(t *testing.T) {
	writer, err := NewWriter(WriterOptions{Bounds: [4]float64{-180, -85, 180, 85}, MinZoom: 0, MaxZoom: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.AddTile(1, 1, 1, []byte("not gzip")); err == nil {
		t.Fatal("expected uncompressed MVT rejection")
	}
	if err := writer.AddTile(1, 1, 1, gzipBytes(t, []byte("later"))); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddTile(0, 0, 0, gzipBytes(t, []byte("earlier"))); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	header, err := writer.WriteTo(&archive)
	if err != nil {
		t.Fatal(err)
	}
	if header.Clustered {
		t.Fatal("unordered tile data must produce an unclustered archive")
	}
	reader, err := NewArchive(header, func(_ context.Context, offset, length int64) ([]byte, error) {
		return archive.Bytes()[offset : offset+length], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.GetTile(context.Background(), 0, 0, 0); err != nil {
		t.Fatal(err)
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
