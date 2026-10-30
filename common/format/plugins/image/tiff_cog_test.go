package image

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/addp/common/format"
)

// Metadata-only layout controls: these do not certify pixel decoding or full
// COG conformance. The real GDAL-validated artifact is checked separately.
func testCOGLayout(t *testing.T, order binary.ByteOrder, ghost bool) []byte {
	t.Helper()
	var header []byte
	if ghost {
		body := "LAYOUT=IFDS_BEFORE_DATA\nBLOCK_ORDER=ROW_MAJOR\nBLOCK_LEADER=SIZE_AS_UINT4\nBLOCK_TRAILER=LAST_4_BYTES_REPEATED\nKNOWN_INCOMPATIBLE_EDITION=NO\n "
		header = []byte(fmt.Sprintf("GDAL_STRUCTURAL_METADATA_SIZE=%06d bytes\n%s", len(body), body))
	}
	ifdOffset := 8 + len(header)
	if ifdOffset%2 != 0 {
		header = append(header, 0)
		ifdOffset++
	}
	const count = 8
	arrayOffset := ifdOffset + 2 + count*12 + 4
	dataOffset := arrayOffset + 16
	gap := 0
	if ghost {
		gap = 8
		dataOffset += 4
	}
	result := make([]byte, dataOffset+16+gap+4)
	copy(result[:2], "II")
	if order == binary.BigEndian {
		copy(result[:2], "MM")
	}
	order.PutUint16(result[2:], 42)
	order.PutUint32(result[4:], uint32(ifdOffset))
	copy(result[8:], header)
	order.PutUint16(result[ifdOffset:], count)
	entries := [][]byte{
		buildIFDEntry(order, tagImageWidth, tiffTypeLong, 1, 32),
		buildIFDEntry(order, tagImageLength, tiffTypeLong, 1, 16),
		buildIFDEntry(order, 277, tiffTypeLong, 1, 1),
		buildIFDEntry(order, 284, tiffTypeLong, 1, 1),
		buildIFDEntry(order, tagTileWidth, tiffTypeLong, 1, 16),
		buildIFDEntry(order, tagTileLength, tiffTypeLong, 1, 16),
		buildIFDEntry(order, tagTileOffsets, tiffTypeLong, 2, uint32(arrayOffset)),
		buildIFDEntry(order, tagTileByteCounts, tiffTypeLong, 2, uint32(arrayOffset+8)),
	}
	for i, entry := range entries {
		copy(result[ifdOffset+2+i*12:], entry)
	}
	order.PutUint32(result[arrayOffset:], uint32(dataOffset))
	order.PutUint32(result[arrayOffset+4:], uint32(dataOffset+8+gap))
	order.PutUint32(result[arrayOffset+8:], 8)
	order.PutUint32(result[arrayOffset+12:], 8)
	for _, offset := range []int{dataOffset, dataOffset + 8 + gap} {
		copy(result[offset:offset+8], "payload!")
		if ghost {
			binary.LittleEndian.PutUint32(result[offset-4:], 8)
			copy(result[offset+8:offset+12], result[offset+4:offset+8])
		}
	}
	return result
}

func TestDescribeUnlocatedCOGRegressionFixture(t *testing.T) {
	// Generated with GDAL 3.12.1: Float64 (arange(28800)%103)-30, 240x120,
	// first 5x7 NaN, DEFLATE/BLOCKSIZE512. No CRS, transform or overview.
	// Independent full COG validation and all-pixel equality passed at creation.
	raw, err := os.ReadFile("testdata/unlocated-cog.tif")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != "6144eb7f009169629a547917f1a8eb008b06764f4892ad70da57dff010021936" {
		t.Fatal("frozen numeric fixture changed", err)
	}
	got, err := newPlugin(format.FormatTIFF).DescribeMedia(context.Background(), bytes.NewReader(raw), nil)
	if err != nil || got.Media == nil || got.Media.Width != 240 || got.Media.Height != 120 || got.Spatial != nil || got.FormatInfo["profile"] != "cog" || got.FormatInfo["is_cloud_optimized"] != true || got.FormatInfo["has_overviews"] != false {
		t.Fatalf("unlocated COG: %#v %v", got, err)
	}
}

func TestTIFFCOGLayoutDoesNotRequireSpatialOrOverviews(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, ghost := range []bool{false, true} {
			data := newTIFFMetadata(testCOGLayout(t, order, ghost), nil)
			info := extractTIFFFormatInfo(data, nil)
			if info["profile"] != "cog" || info["is_cloud_optimized"] != true || info["cog_check_level"] != "heuristic" || info["has_overviews"] != false {
				t.Fatalf("unlocated/no-overview layout: %#v", info)
			}
			if spatial := extractGeoTIFFSpatial(data, 32, 16); spatial != nil {
				t.Fatal("fabricated positioning")
			}
		}
	}
}

func TestTIFFCOGUnknownDoesNotBecomeFalse(t *testing.T) {
	for _, raw := range [][]byte{testTIFFWithTags(t, nil, 0), testGeoTIFF(t), testUnresolvedCOGTagsTIFF(t)} {
		info := extractTIFFFormatInfo(newTIFFMetadata(raw, nil), nil)
		if _, exists := info["is_cloud_optimized"]; exists || info["profile"] == "cog" {
			t.Fatalf("unproved layout must omit cloud assertion: %#v", info)
		}
	}
}

func TestTIFFCOGNeedsKnownLengthWithinMetadataReadBudget(t *testing.T) {
	for _, size := range []int{2 * tiffMetadataReadLimit, 4 * tiffMetadataReadLimit} {
		full := make([]byte, size)
		copy(full, testCOGLayout(t, binary.LittleEndian, true))
		data, err := readTIFFMetadata(io.LimitReader(bytes.NewReader(full), int64(size)), tiffMetadataReadLimit)
		if err != nil || data.sizeKnown || hasTIFFCOGLayout(data) {
			t.Fatal("a full sequential prefix does not establish EOF", err)
		}
		data, err = readTIFFMetadata(bytes.NewReader(full), tiffMetadataReadLimit)
		if err != nil || !data.sizeKnown || !hasTIFFCOGLayout(data) {
			t.Fatal("bounded seekable windows must retain known EOF after merge", err)
		}
	}
}

func TestTIFFCOGRequiresLargeToSmallIFDsAndSmallToLargeData(t *testing.T) {
	const directoryBytes = 2 + 9*12 + 4
	const dataStart = 8 + 3*directoryBytes
	order := binary.LittleEndian
	raw := make([]byte, dataStart+24)
	copy(raw, "II")
	order.PutUint16(raw[2:], 42)
	order.PutUint32(raw[4:], 8)
	for i, width := range []uint32{64, 32, 16} {
		offset := 8 + i*directoryBytes
		order.PutUint16(raw[offset:], 9)
		kind := uint32(1)
		if i == 0 {
			kind = 0
		}
		for j, pair := range [][2]uint32{{254, kind}, {256, width}, {257, 16}, {277, 1}, {284, 1}, {322, 64}, {323, 16}, {324, uint32(dataStart + (2-i)*8)}, {325, 8}} {
			copy(raw[offset+2+j*12:], buildIFDEntry(order, uint16(pair[0]), tiffTypeLong, 1, pair[1]))
		}
		if i < 2 {
			order.PutUint32(raw[offset+directoryBytes-4:], uint32(offset+directoryBytes))
		}
	}
	if !hasTIFFCOGLayout(newTIFFMetadata(raw, nil)) {
		t.Fatal("ordered IFD and tile control")
	}
	// Preserve all valid image fields and data order, but reverse the overview
	// directory order; merely seeing tiles and reduced-resolution tags is not proof.
	second, third := 8+directoryBytes, 8+2*directoryBytes
	a, b := append([]byte(nil), raw[second:second+directoryBytes]...), append([]byte(nil), raw[third:third+directoryBytes]...)
	copy(raw[second:], b)
	copy(raw[third:], a)
	order.PutUint32(raw[second+directoryBytes-4:], uint32(third))
	order.PutUint32(raw[third+directoryBytes-4:], 0)
	if hasTIFFCOGLayout(newTIFFMetadata(raw, nil)) {
		t.Fatal("reversed overview IFDs must not get a COG hint")
	}
}

func TestTIFFCOGRejectsContradictionsAndUnresolvedLayout(t *testing.T) {
	for name, change := range map[string]func([]byte) []byte{
		"truncated-tile":     func(b []byte) []byte { return b[:len(b)-10] },
		"duplicate-tag":      func(b []byte) []byte { copy(b[22:24], b[10:12]); return b },
		"zero-width":         func(b []byte) []byte { binary.LittleEndian.PutUint32(b[18:], 0); return b },
		"out-of-range-array": func(b []byte) []byte { binary.LittleEndian.PutUint32(b[90:], 0xfffffff0); return b },
		"wrong-tile-count":   func(b []byte) []byte { binary.LittleEndian.PutUint32(b[86:], 3); return b },
		"reversed-tiles":     func(b []byte) []byte { copy(b[110:114], b[114:118]); return b },
		"tile-in-metadata":   func(b []byte) []byte { binary.LittleEndian.PutUint32(b[110:], 8); return b },
		"cycle":              func(b []byte) []byte { binary.LittleEndian.PutUint32(b[106:], 8); return b },
	} {
		t.Run(name, func(t *testing.T) {
			info := extractTIFFFormatInfo(newTIFFMetadata(change(testCOGLayout(t, binary.LittleEndian, false)), nil), nil)
			if _, exists := info["is_cloud_optimized"]; exists || info["profile"] == "cog" {
				t.Fatalf("invalid layout got a cloud assertion: %#v", info)
			}
		})
	}
	for _, replacement := range [][]byte{[]byte("KNOWN_INCOMPATIBLE_EDITION=YES\n"), []byte("KNOWN_INCOMPATIBLE_EDITION=??\n ")} {
		raw := testCOGLayout(t, binary.LittleEndian, true)
		raw = bytes.Replace(raw, []byte("KNOWN_INCOMPATIBLE_EDITION=NO\n "), replacement, 1)
		info := extractTIFFFormatInfo(newTIFFMetadata(raw, nil), nil)
		if info["profile"] == "cog" {
			t.Fatal("incompatible or malformed structural declaration")
		}
	}
}
