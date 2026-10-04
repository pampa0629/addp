package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"

	"github.com/addp/common/format"
)

// These directories intentionally contain no pixels: page facts do not certify
// that compression or pixel blocks can be decoded.
func pageDirectories(order binary.ByteOrder, kinds ...uint32) []byte {
	const size = 42
	data := make([]byte, 8+size*len(kinds))
	copy(data, "II")
	if order == binary.BigEndian {
		copy(data, "MM")
	}
	order.PutUint16(data[2:], 42)
	order.PutUint32(data[4:], 8)
	for i, kind := range kinds {
		off := 8 + i*size
		order.PutUint16(data[off:], 3)
		entry := func(n int, tag uint16, value uint32) {
			p := data[off+2+n*12:]
			order.PutUint16(p, tag)
			order.PutUint16(p[2:], tiffTypeLong)
			order.PutUint32(p[4:], 1)
			order.PutUint32(p[8:], value)
		}
		entry(0, tagNewSubfileType, kind)
		entry(1, tagImageWidth, uint32(10+i))
		entry(2, tagImageLength, uint32(20+i))
		if i+1 < len(kinds) {
			order.PutUint32(data[off+38:], uint32(off+size))
		}
	}
	return data
}

func assertPageStatus(t *testing.T, data tiffMetadata, status string) map[string]interface{} {
	t.Helper()
	got := describeTIFFPages(data)
	if got["page_summary_status"] != status {
		t.Fatalf("summary = %#v, want %s", got, status)
	}
	if status != "parsed" && (got["pages"] != nil || got["page_count"] != nil) {
		t.Fatalf("partial facts escaped: %#v", got)
	}
	return got
}

func TestTIFFPageDirectoriesExcludeOverviewsAndMasks(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			got := assertPageStatus(t, newTIFFMetadata(pageDirectories(order, 0, 1, 4, 2, 5), nil), "parsed")
			want := []map[string]interface{}{{"ifd_index": 0, "width": 10, "height": 20}, {"ifd_index": 3, "width": 13, "height": 23}}
			if got["page_count"] != 2 || !reflect.DeepEqual(got["pages"], want) {
				t.Fatalf("summary=%#v", got)
			}
		})
	}
}

func TestTIFFPageDirectoriesFailWithoutPartialCount(t *testing.T) {
	cases := map[string]func([]byte) []byte{
		"self cycle":      func(b []byte) []byte { binary.LittleEndian.PutUint32(b[46:], 8); return b },
		"later cycle":     func(b []byte) []byte { binary.LittleEndian.PutUint32(b[88:], 8); return b },
		"zero width":      func(b []byte) []byte { binary.LittleEndian.PutUint32(b[30:], 0); return b },
		"bad width type":  func(b []byte) []byte { binary.LittleEndian.PutUint16(b[24:], tiffTypeRational); return b },
		"bad width count": func(b []byte) []byte { binary.LittleEndian.PutUint32(b[26:], 2); return b },
		"bad subtype":     func(b []byte) []byte { binary.LittleEndian.PutUint16(b[12:], tiffTypeASCII); return b },
		"duplicate tag":   func(b []byte) []byte { binary.LittleEndian.PutUint16(b[34:], tagImageWidth); return b },
		"truncated":       func(b []byte) []byte { return b[:len(b)-2] },
		"outside EOF":     func(b []byte) []byte { binary.LittleEndian.PutUint32(b[88:], 9000); return b },
		"header pointer":  func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:], 2); return b },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			assertPageStatus(t, newTIFFMetadata(change(pageDirectories(binary.LittleEndian, 0, 0)), nil), "invalid")
		})
	}
	assertPageStatus(t, newTIFFMetadata([]byte{'I', 'I', 43, 0}, nil), "unsupported")
}

func TestTIFFPageDirectoriesBoundedWindows(t *testing.T) {
	for _, count := range []int{256, 257} {
		data := pageDirectories(binary.LittleEndian, make([]uint32, count)...)
		status := "parsed"
		if count > 256 {
			status = "budget_exceeded"
		}
		assertPageStatus(t, newTIFFMetadata(data, nil), status)
	}
	b := make([]byte, tiffMetadataReadLimit)
	copy(b, pageDirectories(binary.LittleEndian, 0))
	binary.LittleEndian.PutUint32(b[46:], uint32(tiffMetadataReadLimit+100))
	assertPageStatus(t, newTIFFMetadata(b, nil), "budget_exceeded")
	assertPageStatus(t, newTIFFMetadata(b, &tiffWindow{offset: 3 * tiffMetadataReadLimit, data: make([]byte, tiffMetadataReadLimit)}), "budget_exceeded")
	binary.LittleEndian.PutUint32(b[46:], 5*tiffMetadataReadLimit)
	assertPageStatus(t, newTIFFMetadata(b, &tiffWindow{offset: 3 * tiffMetadataReadLimit, data: make([]byte, tiffMetadataReadLimit)}), "invalid")
	// A valid second page in the tail window remains visible.
	tail := pageDirectories(binary.LittleEndian, 0)[8:]
	binary.LittleEndian.PutUint32(b[46:], 3*tiffMetadataReadLimit)
	got := assertPageStatus(t, newTIFFMetadata(b, &tiffWindow{offset: 3 * tiffMetadataReadLimit, data: tail}), "parsed")
	if got["page_count"] != 2 {
		t.Fatalf("tail page missing: %#v", got)
	}
}

func TestTIFFStandardSubfileTypeAndSubIFD(t *testing.T) {
	b := pageDirectories(binary.LittleEndian, 0, 0)
	// Standard SubfileType=2 is a reduced image; NewSubfileType is absent here.
	binary.LittleEndian.PutUint16(b[10:], tagSubfileType)
	binary.LittleEndian.PutUint16(b[12:], tiffTypeShort)
	binary.LittleEndian.PutUint32(b[18:], 2)
	got := assertPageStatus(t, newTIFFMetadata(b, nil), "parsed")
	if got["page_count"] != 1 {
		t.Fatal(got)
	}
	// A SubIFD pointer is not traversed or counted as a top-level page.
	binary.LittleEndian.PutUint16(b[10:], tagSubIFDs)
	binary.LittleEndian.PutUint16(b[12:], tiffTypeLong)
	binary.LittleEndian.PutUint32(b[18:], 8)
	got = assertPageStatus(t, newTIFFMetadata(b, nil), "parsed")
	if got["page_count"] != 2 {
		t.Fatal(got)
	}
}

type shortReadTIFF struct{ *bytes.Reader }

func (r shortReadTIFF) Read(p []byte) (int, error) {
	if len(p) > 3 {
		p = p[:3]
	}
	return r.Reader.Read(p)
}
func TestTIFFPageDirectoriesReadShortSeekableChunks(t *testing.T) {
	data, err := readTIFFMetadata(shortReadTIFF{bytes.NewReader(pageDirectories(binary.LittleEndian, 0, 0))}, tiffMetadataReadLimit)
	if err != nil {
		t.Fatal(err)
	}
	got := assertPageStatus(t, data, "parsed")
	if got["page_count"] != 2 {
		t.Fatal(got)
	}
}

func TestTIFFMediaOnlyRetainsValidFirstPageDimensions(t *testing.T) {
	for _, invalidPage := range []int{0, 1} {
		data := pageDirectories(binary.LittleEndian, 0, 0)
		binary.LittleEndian.PutUint32(data[26+invalidPage*42:], 2)
		result, err := newPlugin(format.FormatTIFF).DescribeMedia(context.Background(), bytes.NewReader(data), nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.FormatInfo["page_summary_status"] != "invalid" || result.FormatInfo["page_count"] != nil {
			t.Fatalf("incomplete summary=%#v", result)
		}
		if invalidPage == 0 {
			if result.Media != nil {
				t.Fatalf("invalid width count invented dimensions: %#v", result.Media)
			}
		} else if result.Media == nil || result.Media.Width != 10 || result.Media.Height != 20 {
			t.Fatalf("valid first dimensions were lost: %#v", result.Media)
		}
	}
}
