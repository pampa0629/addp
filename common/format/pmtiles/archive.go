package pmtiles

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const HeaderSize = 127

const (
	CompressionNone = 1
	CompressionGzip = 2
	TileTypeMVT     = 1
)

var ErrTileNotFound = errors.New("PMTiles tile not found")

type Header struct {
	SpecVersion         uint8
	RootOffset          uint64
	RootLength          uint64
	MetadataOffset      uint64
	MetadataLength      uint64
	LeafDirectoryOffset uint64
	LeafDirectoryLength uint64
	TileDataOffset      uint64
	TileDataLength      uint64
	AddressedTilesCount uint64
	TileEntriesCount    uint64
	TileContentsCount   uint64
	Clustered           bool
	InternalCompression uint8
	TileCompression     uint8
	TileType            uint8
	MinZoom             uint8
	MaxZoom             uint8
	MinLonE7            int32
	MinLatE7            int32
	MaxLonE7            int32
	MaxLatE7            int32
	CenterZoom          uint8
	CenterLonE7         int32
	CenterLatE7         int32
}

type RangeReadFunc func(ctx context.Context, offset, length int64) ([]byte, error)

type Archive struct {
	Header Header
	read   RangeReadFunc
}

type directoryEntry struct {
	tileID    uint64
	offset    uint64
	length    uint32
	runLength uint32
}

func ParseHeader(r io.Reader) (Header, error) {
	data := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, data); err != nil {
		return Header{}, fmt.Errorf("read PMTiles header: %w", err)
	}
	return ParseHeaderBytes(data)
}

func ParseHeaderBytes(data []byte) (Header, error) {
	if len(data) < HeaderSize {
		return Header{}, fmt.Errorf("PMTiles header is too short: %d", len(data))
	}
	if string(data[:7]) != "PMTiles" {
		return Header{}, errors.New("invalid PMTiles magic")
	}
	h := Header{
		SpecVersion:         data[7],
		RootOffset:          binary.LittleEndian.Uint64(data[8:16]),
		RootLength:          binary.LittleEndian.Uint64(data[16:24]),
		MetadataOffset:      binary.LittleEndian.Uint64(data[24:32]),
		MetadataLength:      binary.LittleEndian.Uint64(data[32:40]),
		LeafDirectoryOffset: binary.LittleEndian.Uint64(data[40:48]),
		LeafDirectoryLength: binary.LittleEndian.Uint64(data[48:56]),
		TileDataOffset:      binary.LittleEndian.Uint64(data[56:64]),
		TileDataLength:      binary.LittleEndian.Uint64(data[64:72]),
		AddressedTilesCount: binary.LittleEndian.Uint64(data[72:80]),
		TileEntriesCount:    binary.LittleEndian.Uint64(data[80:88]),
		TileContentsCount:   binary.LittleEndian.Uint64(data[88:96]),
		Clustered:           data[96] == 1,
		InternalCompression: data[97],
		TileCompression:     data[98],
		TileType:            data[99],
		MinZoom:             data[100],
		MaxZoom:             data[101],
		MinLonE7:            int32(binary.LittleEndian.Uint32(data[102:106])),
		MinLatE7:            int32(binary.LittleEndian.Uint32(data[106:110])),
		MaxLonE7:            int32(binary.LittleEndian.Uint32(data[110:114])),
		MaxLatE7:            int32(binary.LittleEndian.Uint32(data[114:118])),
		CenterZoom:          data[118],
		CenterLonE7:         int32(binary.LittleEndian.Uint32(data[119:123])),
		CenterLatE7:         int32(binary.LittleEndian.Uint32(data[123:127])),
	}
	if err := ValidateHeader(h, 0); err != nil {
		return Header{}, err
	}
	return h, nil
}

func HeaderHash(data []byte) (string, error) {
	if _, err := ParseHeaderBytes(data); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data[:HeaderSize])
	return hex.EncodeToString(sum[:]), nil
}

func ValidateHeader(h Header, size int64) error {
	if h.SpecVersion != 3 {
		return fmt.Errorf("unsupported PMTiles version %d", h.SpecVersion)
	}
	if h.TileType != TileTypeMVT {
		return fmt.Errorf("unsupported PMTiles tile type %d: ADDP requires MVT", h.TileType)
	}
	if h.TileCompression != CompressionGzip {
		return fmt.Errorf("unsupported PMTiles tile compression %d: ADDP requires gzip", h.TileCompression)
	}
	if h.InternalCompression != CompressionNone && h.InternalCompression != CompressionGzip {
		return fmt.Errorf("unsupported PMTiles internal compression %d", h.InternalCompression)
	}
	if h.RootLength == 0 || h.TileDataOffset < HeaderSize || h.MaxZoom < h.MinZoom || h.MaxZoom > 31 {
		return errors.New("invalid PMTiles header ranges")
	}
	if h.MinLonE7 < -1800000000 || h.MaxLonE7 > 1800000000 || h.MinLatE7 < -900000000 || h.MaxLatE7 > 900000000 || h.MinLonE7 > h.MaxLonE7 || h.MinLatE7 > h.MaxLatE7 {
		return errors.New("invalid PMTiles bounds")
	}
	if size > 0 {
		for _, section := range []struct {
			name           string
			offset, length uint64
		}{{"root", h.RootOffset, h.RootLength}, {"metadata", h.MetadataOffset, h.MetadataLength}, {"leaf directory", h.LeafDirectoryOffset, h.LeafDirectoryLength}, {"tile data", h.TileDataOffset, h.TileDataLength}} {
			if section.offset > uint64(size) || section.length > uint64(size)-section.offset {
				return fmt.Errorf("PMTiles %s section exceeds archive size", section.name)
			}
		}
	}
	return nil
}

func NewArchive(header Header, read RangeReadFunc) (*Archive, error) {
	if read == nil {
		return nil, errors.New("PMTiles range reader is required")
	}
	if err := ValidateHeader(header, 0); err != nil {
		return nil, err
	}
	return &Archive{Header: header, read: read}, nil
}

func (a *Archive) GetTile(ctx context.Context, z uint8, x, y uint32) ([]byte, error) {
	if a == nil || a.read == nil {
		return nil, errors.New("PMTiles archive is not initialized")
	}
	if z < a.Header.MinZoom || z > a.Header.MaxZoom || z > 31 || x >= 1<<z || y >= 1<<z {
		return nil, ErrTileNotFound
	}
	tileID := zxyToID(z, x, y)
	offset, length := a.Header.RootOffset, a.Header.RootLength
	for depth := 0; depth < 4; depth++ {
		data, err := a.read(ctx, int64(offset), int64(length))
		if err != nil {
			return nil, fmt.Errorf("read PMTiles directory: %w", err)
		}
		entries, err := decodeDirectory(data, a.Header.InternalCompression)
		if err != nil {
			return nil, err
		}
		entry, ok := findTile(entries, tileID)
		if !ok {
			return nil, ErrTileNotFound
		}
		if entry.runLength == 0 {
			offset = a.Header.LeafDirectoryOffset + entry.offset
			length = uint64(entry.length)
			continue
		}
		tile, err := a.read(ctx, int64(a.Header.TileDataOffset+entry.offset), int64(entry.length))
		if err != nil {
			return nil, fmt.Errorf("read PMTiles tile: %w", err)
		}
		return tile, nil
	}
	return nil, errors.New("PMTiles directory depth exceeds v3 limit")
}

// ValidateDirectories reads and validates the complete PMTiles directory tree without reading tile payloads.
func (a *Archive) ValidateDirectories(ctx context.Context) error {
	if a == nil || a.read == nil {
		return errors.New("PMTiles archive is not initialized")
	}
	type section struct {
		offset, length uint64
		depth          int
	}
	queue := []section{{a.Header.RootOffset, a.Header.RootLength, 0}}
	seen := map[[2]uint64]struct{}{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= 4 {
			return errors.New("PMTiles directory depth exceeds v3 limit")
		}
		key := [2]uint64{current.offset, current.length}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		data, err := a.read(ctx, int64(current.offset), int64(current.length))
		if err != nil {
			return fmt.Errorf("read PMTiles directory: %w", err)
		}
		entries, err := decodeDirectory(data, a.Header.InternalCompression)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.runLength != 0 {
				continue
			}
			offset := a.Header.LeafDirectoryOffset + entry.offset
			length := uint64(entry.length)
			if offset < a.Header.LeafDirectoryOffset || length == 0 || offset > a.Header.TileDataOffset || length > a.Header.TileDataOffset-offset {
				return errors.New("invalid PMTiles leaf directory range")
			}
			queue = append(queue, section{offset, length, current.depth + 1})
		}
	}
	return nil
}

func FormatInfo(h Header) map[string]interface{} {
	return map[string]interface{}{
		"spec_version": h.SpecVersion, "tile_type": "mvt", "tile_compression": compressionName(h.TileCompression),
		"internal_compression": compressionName(h.InternalCompression), "min_zoom": h.MinZoom, "max_zoom": h.MaxZoom,
		"addressed_tiles_count": h.AddressedTilesCount, "tile_entries_count": h.TileEntriesCount,
		"tile_contents_count": h.TileContentsCount, "clustered": h.Clustered,
		"center": []float64{float64(h.CenterLonE7) / 1e7, float64(h.CenterLatE7) / 1e7, float64(h.CenterZoom)},
	}
}

func Bounds(h Header) [4]float64 {
	return [4]float64{float64(h.MinLonE7) / 1e7, float64(h.MinLatE7) / 1e7, float64(h.MaxLonE7) / 1e7, float64(h.MaxLatE7) / 1e7}
}

func compressionName(value uint8) string {
	switch value {
	case CompressionNone:
		return "none"
	case CompressionGzip:
		return "gzip"
	default:
		return "unknown"
	}
}

func decodeDirectory(data []byte, compression uint8) ([]directoryEntry, error) {
	var reader io.Reader = bytes.NewReader(data)
	if compression == CompressionGzip {
		gzipReader, err := gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("open PMTiles directory gzip: %w", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}
	byteReader := &byteReadAdapter{r: reader}
	count, err := binary.ReadUvarint(byteReader)
	if err != nil || count > 10_000_000 {
		return nil, errors.New("invalid PMTiles directory entry count")
	}
	entries := make([]directoryEntry, int(count))
	var lastID uint64
	for i := range entries {
		delta, err := binary.ReadUvarint(byteReader)
		if err != nil {
			return nil, fmt.Errorf("read PMTiles tile id: %w", err)
		}
		lastID += delta
		entries[i].tileID = lastID
	}
	for i := range entries {
		value, err := binary.ReadUvarint(byteReader)
		if err != nil || value > uint64(^uint32(0)) {
			return nil, errors.New("invalid PMTiles run length")
		}
		entries[i].runLength = uint32(value)
	}
	for i := range entries {
		value, err := binary.ReadUvarint(byteReader)
		if err != nil || value > uint64(^uint32(0)) {
			return nil, errors.New("invalid PMTiles entry length")
		}
		entries[i].length = uint32(value)
	}
	for i := range entries {
		value, err := binary.ReadUvarint(byteReader)
		if err != nil {
			return nil, fmt.Errorf("read PMTiles entry offset: %w", err)
		}
		if i > 0 && value == 0 {
			entries[i].offset = entries[i-1].offset + uint64(entries[i-1].length)
		} else if value == 0 {
			return nil, errors.New("invalid PMTiles first entry offset")
		} else {
			entries[i].offset = value - 1
		}
	}
	return entries, nil
}

type byteReadAdapter struct{ r io.Reader }

func (r *byteReadAdapter) ReadByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(r.r, b[:])
	return b[0], err
}

func findTile(entries []directoryEntry, tileID uint64) (directoryEntry, bool) {
	lo, hi := 0, len(entries)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if entries[mid].tileID < tileID {
			lo = mid + 1
		} else if entries[mid].tileID > tileID {
			hi = mid - 1
		} else {
			return entries[mid], true
		}
	}
	if hi >= 0 && entries[hi].runLength > 0 && tileID-entries[hi].tileID < uint64(entries[hi].runLength) {
		return entries[hi], true
	}
	if hi >= 0 && entries[hi].runLength == 0 {
		return entries[hi], true
	}
	return directoryEntry{}, false
}

func zxyToID(z uint8, x, y uint32) uint64 {
	acc := (uint64(1)<<(z*2) - 1) / 3
	for n := int(z) - 1; n >= 0; n-- {
		s := uint32(1) << n
		rx, ry := s&x, s&y
		acc += uint64((3*rx)^ry) << n
		if ry == 0 {
			if rx != 0 {
				x, y = s-1-x, s-1-y
			}
			x, y = y, x
		}
	}
	return acc
}

// TileID returns the PMTiles v3 Hilbert tile id for a Z/X/Y coordinate.
func TileID(z uint8, x, y uint32) uint64 {
	return zxyToID(z, x, y)
}
