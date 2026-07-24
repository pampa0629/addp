package pmtiles

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

func TestArchiveReadsRootTileByRange(t *testing.T) {
	tile := []byte{0x1f, 0x8b, 0x08, 0x00}
	directory := encodeDirectory(t, []directoryEntry{{tileID: 0, offset: 0, length: uint32(len(tile)), runLength: 1}})
	header := testHeader(uint64(len(directory)), uint64(len(tile)))
	headerBytes := serializeTestHeader(header)
	archiveBytes := append(append(headerBytes, directory...), tile...)

	archive, err := NewArchive(header, func(_ context.Context, offset, length int64) ([]byte, error) {
		return append([]byte(nil), archiveBytes[offset:offset+length]...), nil
	})
	if err != nil {
		t.Fatalf("NewArchive() error = %v", err)
	}
	got, err := archive.GetTile(context.Background(), 0, 0, 0)
	if err != nil {
		t.Fatalf("GetTile() error = %v", err)
	}
	if !bytes.Equal(got, tile) {
		t.Fatalf("tile = %x, want %x", got, tile)
	}
	if _, err := archive.GetTile(context.Background(), 1, 0, 0); !errors.Is(err, ErrTileNotFound) {
		t.Fatalf("out of range error = %v", err)
	}
}

func TestValidateHeaderRejectsUnsupportedArchive(t *testing.T) {
	header := testHeader(1, 1)
	header.SpecVersion = 2
	if err := ValidateHeader(header, 129); err == nil {
		t.Fatal("expected PMTiles v2 rejection")
	}
	header.SpecVersion = 3
	header.TileCompression = CompressionNone
	if err := ValidateHeader(header, 129); err == nil {
		t.Fatal("expected uncompressed MVT rejection")
	}
}

func TestValidateHeaderRejectsSectionPastObjectSize(t *testing.T) {
	header := testHeader(10, 20)
	if err := ValidateHeader(header, HeaderSize+5); err == nil {
		t.Fatal("expected section size rejection")
	}
}

func testHeader(rootLength, tileLength uint64) Header {
	return Header{
		SpecVersion: 3, RootOffset: HeaderSize, RootLength: rootLength,
		TileDataOffset: HeaderSize + rootLength, TileDataLength: tileLength,
		AddressedTilesCount: 1, TileEntriesCount: 1, TileContentsCount: 1,
		Clustered: true, InternalCompression: CompressionGzip, TileCompression: CompressionGzip,
		TileType: TileTypeMVT, MinZoom: 0, MaxZoom: 0,
		MinLonE7: -1800000000, MinLatE7: -850000000, MaxLonE7: 1800000000, MaxLatE7: 850000000,
	}
}

func serializeTestHeader(h Header) []byte {
	b := make([]byte, HeaderSize)
	copy(b, "PMTiles")
	b[7] = h.SpecVersion
	binary.LittleEndian.PutUint64(b[8:16], h.RootOffset)
	binary.LittleEndian.PutUint64(b[16:24], h.RootLength)
	binary.LittleEndian.PutUint64(b[56:64], h.TileDataOffset)
	binary.LittleEndian.PutUint64(b[64:72], h.TileDataLength)
	b[96], b[97], b[98], b[99] = 1, h.InternalCompression, h.TileCompression, h.TileType
	b[100], b[101] = h.MinZoom, h.MaxZoom
	binary.LittleEndian.PutUint32(b[102:106], uint32(h.MinLonE7))
	binary.LittleEndian.PutUint32(b[106:110], uint32(h.MinLatE7))
	binary.LittleEndian.PutUint32(b[110:114], uint32(h.MaxLonE7))
	binary.LittleEndian.PutUint32(b[114:118], uint32(h.MaxLatE7))
	return b
}

func encodeDirectory(t *testing.T, entries []directoryEntry) []byte {
	t.Helper()
	var raw bytes.Buffer
	writeUvarint := func(value uint64) {
		var buf [binary.MaxVarintLen64]byte
		n := binary.PutUvarint(buf[:], value)
		raw.Write(buf[:n])
	}
	writeUvarint(uint64(len(entries)))
	var last uint64
	for _, entry := range entries {
		writeUvarint(entry.tileID - last)
		last = entry.tileID
	}
	for _, entry := range entries {
		writeUvarint(uint64(entry.runLength))
	}
	for _, entry := range entries {
		writeUvarint(uint64(entry.length))
	}
	for _, entry := range entries {
		writeUvarint(entry.offset + 1)
	}
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := w.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}
