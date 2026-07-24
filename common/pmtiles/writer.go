package pmtiles

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
)

const targetRootDirectoryLength = 16*1024 - HeaderSize

type WriterOptions struct {
	Bounds   [4]float64
	MinZoom  uint8
	MaxZoom  uint8
	Center   [3]float64
	Metadata map[string]interface{}
}

// Writer assembles gzip-compressed MVT tiles into a PMTiles v3 archive.
// Tiles must be added in ascending PMTiles tile-id order.
type Writer struct {
	options    WriterOptions
	tileData   *os.File
	entries    []directoryEntry
	tileOffset uint64
	lastTileID uint64
	hasTile    bool
	clustered  bool
	closed     bool
}

func NewWriter(options WriterOptions) (*Writer, error) {
	if options.MaxZoom < options.MinZoom || options.MaxZoom > 31 {
		return nil, errors.New("invalid PMTiles writer zoom range")
	}
	if err := validateWriterBounds(options.Bounds); err != nil {
		return nil, err
	}
	tileData, err := os.CreateTemp("", "addp-pmtiles-tile-data-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create PMTiles tile data file: %w", err)
	}
	return &Writer{options: options, tileData: tileData, clustered: true}, nil
}

func (w *Writer) AddTile(z uint8, x, y uint32, data []byte) error {
	if w == nil || w.closed || w.tileData == nil {
		return errors.New("PMTiles writer is closed")
	}
	if z > 31 || z < w.options.MinZoom || z > w.options.MaxZoom || x >= uint32(1)<<z || y >= uint32(1)<<z {
		return fmt.Errorf("invalid PMTiles tile coordinate %d/%d/%d", z, x, y)
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return errors.New("PMTiles MVT tile must be gzip-compressed")
	}
	if uint64(len(data)) > uint64(^uint32(0)) {
		return errors.New("PMTiles tile exceeds uint32 length")
	}
	tileID := zxyToID(z, x, y)
	if w.hasTile && tileID <= w.lastTileID {
		w.clustered = false
	}
	if _, err := w.tileData.Write(data); err != nil {
		return fmt.Errorf("write PMTiles tile data: %w", err)
	}
	w.entries = append(w.entries, directoryEntry{
		tileID: tileID, offset: w.tileOffset, length: uint32(len(data)), runLength: 1,
	})
	w.tileOffset += uint64(len(data))
	w.lastTileID = tileID
	w.hasTile = true
	return nil
}

func (w *Writer) WriteTo(output io.Writer) (Header, error) {
	if w == nil || w.closed || w.tileData == nil {
		return Header{}, errors.New("PMTiles writer is closed")
	}
	if output == nil {
		return Header{}, errors.New("PMTiles output writer is required")
	}
	if len(w.entries) == 0 {
		return Header{}, errors.New("PMTiles archive requires at least one tile")
	}

	entries := append([]directoryEntry(nil), w.entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].tileID < entries[j].tileID })
	for index := 1; index < len(entries); index++ {
		if entries[index].tileID == entries[index-1].tileID {
			return Header{}, errors.New("PMTiles archive contains a duplicate tile coordinate")
		}
	}
	root, leaves := buildDirectories(entries, targetRootDirectoryLength)
	metadata, err := serializeMetadata(w.options.Metadata)
	if err != nil {
		return Header{}, err
	}
	header := writerHeader(w.options, entries, w.clustered, uint64(len(root)), uint64(len(metadata)), uint64(len(leaves)), w.tileOffset)
	if _, err := output.Write(serializeHeader(header)); err != nil {
		return Header{}, fmt.Errorf("write PMTiles header: %w", err)
	}
	for _, section := range []struct {
		name string
		data []byte
	}{{"root directory", root}, {"metadata", metadata}, {"leaf directories", leaves}} {
		if _, err := output.Write(section.data); err != nil {
			return Header{}, fmt.Errorf("write PMTiles %s: %w", section.name, err)
		}
	}
	if _, err := w.tileData.Seek(0, io.SeekStart); err != nil {
		return Header{}, fmt.Errorf("seek PMTiles tile data: %w", err)
	}
	if _, err := io.Copy(output, w.tileData); err != nil {
		return Header{}, fmt.Errorf("write PMTiles tile data: %w", err)
	}
	return header, nil
}

func (w *Writer) Close() error {
	if w == nil || w.closed {
		return nil
	}
	w.closed = true
	name := ""
	if w.tileData != nil {
		name = w.tileData.Name()
		_ = w.tileData.Close()
	}
	if name != "" {
		return os.Remove(name)
	}
	return nil
}

func writerHeader(options WriterOptions, entries []directoryEntry, clustered bool, rootLength, metadataLength, leavesLength, tileDataLength uint64) Header {
	minZoom, maxZoom := uint8(31), uint8(0)
	for _, entry := range entries {
		z, _, _ := idToZXY(entry.tileID)
		if z < minZoom {
			minZoom = z
		}
		if z > maxZoom {
			maxZoom = z
		}
	}
	center := options.Center
	if center == [3]float64{} {
		center = [3]float64{
			(options.Bounds[0] + options.Bounds[2]) / 2,
			(options.Bounds[1] + options.Bounds[3]) / 2,
			float64(minZoom),
		}
	}
	header := Header{
		SpecVersion: 3, RootOffset: HeaderSize, RootLength: rootLength,
		AddressedTilesCount: uint64(len(entries)), TileEntriesCount: uint64(len(entries)), TileContentsCount: uint64(len(entries)),
		Clustered: clustered, InternalCompression: CompressionGzip, TileCompression: CompressionGzip, TileType: TileTypeMVT,
		MinZoom: minZoom, MaxZoom: maxZoom,
		MinLonE7: e7(options.Bounds[0]), MinLatE7: e7(options.Bounds[1]), MaxLonE7: e7(options.Bounds[2]), MaxLatE7: e7(options.Bounds[3]),
		CenterZoom: uint8(math.Round(clamp(center[2], float64(minZoom), float64(maxZoom)))), CenterLonE7: e7(center[0]), CenterLatE7: e7(center[1]),
	}
	header.MetadataOffset = header.RootOffset + header.RootLength
	header.MetadataLength = metadataLength
	header.LeafDirectoryOffset = header.MetadataOffset + header.MetadataLength
	header.LeafDirectoryLength = leavesLength
	header.TileDataOffset = header.LeafDirectoryOffset + header.LeafDirectoryLength
	header.TileDataLength = tileDataLength
	return header
}

func serializeHeader(header Header) []byte {
	data := make([]byte, HeaderSize)
	copy(data, "PMTiles")
	data[7] = 3
	values := []struct {
		offset int
		value  uint64
	}{
		{8, header.RootOffset}, {16, header.RootLength}, {24, header.MetadataOffset}, {32, header.MetadataLength},
		{40, header.LeafDirectoryOffset}, {48, header.LeafDirectoryLength}, {56, header.TileDataOffset}, {64, header.TileDataLength},
		{72, header.AddressedTilesCount}, {80, header.TileEntriesCount}, {88, header.TileContentsCount},
	}
	for _, item := range values {
		binary.LittleEndian.PutUint64(data[item.offset:item.offset+8], item.value)
	}
	if header.Clustered {
		data[96] = 1
	}
	data[97], data[98], data[99] = header.InternalCompression, header.TileCompression, header.TileType
	data[100], data[101] = header.MinZoom, header.MaxZoom
	binary.LittleEndian.PutUint32(data[102:106], uint32(header.MinLonE7))
	binary.LittleEndian.PutUint32(data[106:110], uint32(header.MinLatE7))
	binary.LittleEndian.PutUint32(data[110:114], uint32(header.MaxLonE7))
	binary.LittleEndian.PutUint32(data[114:118], uint32(header.MaxLatE7))
	data[118] = header.CenterZoom
	binary.LittleEndian.PutUint32(data[119:123], uint32(header.CenterLonE7))
	binary.LittleEndian.PutUint32(data[123:127], uint32(header.CenterLatE7))
	return data
}

func serializeMetadata(metadata map[string]interface{}) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal PMTiles metadata: %w", err)
	}
	return gzipData(data)
}

func serializeDirectory(entries []directoryEntry) []byte {
	var raw bytes.Buffer
	write := func(value uint64) {
		var buf [binary.MaxVarintLen64]byte
		n := binary.PutUvarint(buf[:], value)
		raw.Write(buf[:n])
	}
	write(uint64(len(entries)))
	lastID := uint64(0)
	for _, entry := range entries {
		write(entry.tileID - lastID)
		lastID = entry.tileID
	}
	for _, entry := range entries {
		write(uint64(entry.runLength))
	}
	for _, entry := range entries {
		write(uint64(entry.length))
	}
	for index, entry := range entries {
		if index > 0 && entry.offset == entries[index-1].offset+uint64(entries[index-1].length) {
			write(0)
		} else {
			write(entry.offset + 1)
		}
	}
	data, _ := gzipData(raw.Bytes())
	return data
}

func buildDirectories(entries []directoryEntry, targetRootLength int) ([]byte, []byte) {
	root := serializeDirectory(entries)
	if len(root) <= targetRootLength {
		return root, nil
	}
	leafSize := len(entries) / 3500
	if leafSize < 4096 {
		leafSize = 4096
	}
	for {
		rootEntries := make([]directoryEntry, 0, (len(entries)+leafSize-1)/leafSize)
		leaves := make([]byte, 0)
		for start := 0; start < len(entries); start += leafSize {
			end := start + leafSize
			if end > len(entries) {
				end = len(entries)
			}
			leaf := serializeDirectory(entries[start:end])
			rootEntries = append(rootEntries, directoryEntry{
				tileID: entries[start].tileID, offset: uint64(len(leaves)), length: uint32(len(leaf)), runLength: 0,
			})
			leaves = append(leaves, leaf...)
		}
		root = serializeDirectory(rootEntries)
		if len(root) <= targetRootLength {
			return root, leaves
		}
		leafSize = int(math.Ceil(float64(leafSize) * 1.2))
	}
}

func gzipData(data []byte) ([]byte, error) {
	var compressed bytes.Buffer
	zw, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return compressed.Bytes(), nil
}

func validateWriterBounds(bounds [4]float64) error {
	if bounds[0] < -180 || bounds[2] > 180 || bounds[1] < -90 || bounds[3] > 90 || bounds[0] > bounds[2] || bounds[1] > bounds[3] {
		return errors.New("invalid PMTiles writer bounds")
	}
	return nil
}

func e7(value float64) int32 { return int32(math.Round(value * 1e7)) }

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func idToZXY(id uint64) (uint8, uint32, uint32) {
	z := uint8(0)
	for z < 31 && ((uint64(1)<<((z+1)*2))-1)/3 <= id {
		z++
	}
	base := (uint64(1)<<(z*2) - 1) / 3
	t := id - base
	var x, y uint32
	for level := uint8(0); level < z; level++ {
		s := uint32(1) << level
		rx := uint32(t>>1) & 1
		ry := (uint32(t) ^ rx) & 1
		if ry == 0 {
			if rx != 0 {
				x, y = s-1-x, s-1-y
			}
			x, y = y, x
		}
		x += rx << level
		y += ry << level
		t >>= 2
	}
	return z, x, y
}
