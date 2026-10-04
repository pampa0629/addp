package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

// These fixtures deliberately contain only headers: a media summary must not
// require pixels, animation frames or a complete-file decode.
func webpHeader(chunk string, payload []byte) []byte {
	data := append([]byte("RIFF\x00\x00\x00\x00WEBP"), []byte(chunk)...)
	data = binary.LittleEndian.AppendUint32(data, uint32(len(payload)))
	data = append(data, payload...)
	if len(payload)%2 != 0 {
		data = append(data, 0)
	}
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	return data
}

func bmpHeader(dib, bpp int, height int32, compression uint32) []byte {
	palette := 0
	if bpp == 8 {
		palette = 256 * 4
	}
	data := make([]byte, 14+dib+palette)
	copy(data, "BM")
	binary.LittleEndian.PutUint32(data[2:6], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[10:14], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[14:18], uint32(dib))
	binary.LittleEndian.PutUint32(data[18:22], 7)
	binary.LittleEndian.PutUint32(data[22:26], uint32(height))
	binary.LittleEndian.PutUint16(data[26:28], 1)
	binary.LittleEndian.PutUint16(data[28:30], uint16(bpp))
	binary.LittleEndian.PutUint32(data[30:34], compression)
	if dib >= 108 {
		for i, mask := range []uint32{0xff0000, 0xff00, 0xff, 0xff000000} {
			binary.LittleEndian.PutUint32(data[54+4*i:], mask)
		}
	}
	return data
}

type countedHeaderReader struct {
	io.Reader
	read int
}

func (r *countedHeaderReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func TestWebPAndBMPReadOnlyHeaderSummaries(t *testing.T) {
	tests := []struct {
		name    string
		format  format.FormatType
		content []byte
	}{
		{"vp8", format.FormatWebP, webpHeader("VP8 ", []byte{0x10, 0, 0, 0x9d, 1, 0x2a, 7, 0, 5, 0})},
		{"vp8l", format.FormatWebP, webpHeader("VP8L", []byte{0x2f, 6, 0, 1, 0})},
		{"vp8x", format.FormatWebP, webpHeader("VP8X", []byte{0, 0, 0, 0, 6, 0, 0, 4, 0, 0})},
		{"animation_canvas", format.FormatWebP, webpHeader("VP8X", []byte{2, 0, 0, 0, 6, 0, 0, 4, 0, 0})},
		{"alpha_canvas", format.FormatWebP, webpHeader("VP8X", []byte{16, 0, 0, 0, 6, 0, 0, 4, 0, 0})},
		{"bmp_palette", format.FormatBMP, bmpHeader(40, 8, 5, 0)},
		{"bmp_rgb", format.FormatBMP, bmpHeader(40, 24, 5, 0)},
		{"bmp_top_down", format.FormatBMP, bmpHeader(40, 32, -5, 0)},
		{"bmp_v4", format.FormatBMP, bmpHeader(108, 24, 5, 0)},
		{"bmp_v5_bitfields", format.FormatBMP, bmpHeader(124, 32, 5, 3)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := format.GetMediaInfoProvider(tt.format)
			if err != nil {
				t.Fatal(err)
			}
			reader := &countedHeaderReader{Reader: io.MultiReader(bytes.NewReader(tt.content), io.LimitReader(zeroHeaderReader{}, 8<<20))}
			result, err := provider.DescribeMedia(t.Context(), reader, nil)
			if err != nil {
				t.Fatal(err)
			}
			m := result.Media
			if m == nil || m.Kind != "image" || m.Width != 7 || m.Height != 5 || m.Encoding != string(tt.format) || m.MIMEType != "image/"+string(tt.format) {
				t.Fatalf("unexpected media summary: %#v", m)
			}
			if m.ColorSpace != "" || m.DurationMS != nil || len(result.FormatInfo) != 0 || result.Spatial != nil {
				t.Fatalf("invented color, animation, EXIF or spatial facts: %#v", result)
			}
			if reader.read > len(tt.content)+4096 {
				t.Fatalf("read pixel payload: %d bytes", reader.read)
			}
		})
	}
}

type zeroHeaderReader struct{}

func (zeroHeaderReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestHeaderImagesRejectMalformedAndUnsupportedContent(t *testing.T) {
	tests := []struct {
		name    string
		format  format.FormatType
		content []byte
	}{
		{"truncated_webp", format.FormatWebP, []byte("RIFF\x20\x00\x00\x00WEBPVP8X")},
		{"wrong_riff_form", format.FormatWebP, []byte("RIFF\x04\x00\x00\x00WAVE")},
		{"invalid_vp8l", format.FormatWebP, webpHeader("VP8L", []byte{0, 6, 0, 1, 0})},
		{"truncated_bmp", format.FormatBMP, []byte("BM\x00")},
		{"unsupported_dib", format.FormatBMP, bmpHeader(64, 24, 5, 0)},
		{"unsupported_bits", format.FormatBMP, bmpHeader(40, 16, 5, 0)},
		{"compressed_bmp", format.FormatBMP, bmpHeader(40, 8, 5, 1)},
		{"zero_height", format.FormatBMP, bmpHeader(40, 24, 0, 0)},
		{"format_mismatch", format.FormatWebP, bmpHeader(40, 24, 5, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := newPlugin(tt.format).DescribeMedia(t.Context(), bytes.NewBuffer(tt.content), nil)
			if err == nil || result != nil {
				t.Fatalf("accepted invalid header: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestWebPHeaderReadBudgetIncludesUnknownChunks(t *testing.T) {
	// Match the registered WebP signature but force the decoder to skip an
	// unknown oversized chunk before it can reach a dimensions header.
	data := webpHeader("VP8?", make([]byte, imageHeaderReadLimit+64))
	for _, kind := range []format.FormatType{format.FormatWebP, format.FormatImage} {
		t.Run(string(kind), func(t *testing.T) {
			reader := &countedHeaderReader{Reader: bytes.NewBuffer(data)}
			result, err := newPlugin(kind).DescribeMedia(t.Context(), reader, nil)
			if err == nil || result != nil || !strings.Contains(err.Error(), "read budget") {
				t.Fatalf("budget not reported: result=%#v err=%v", result, err)
			}
			if reader.read != imageHeaderReadLimit {
				t.Fatalf("read %d bytes, want %d", reader.read, imageHeaderReadLimit)
			}
		})
	}
}

func TestHeaderImageCancelledBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := &countedHeaderReader{Reader: zeroHeaderReader{}}
	_, err := newPlugin(format.FormatBMP).DescribeMedia(ctx, reader, nil)
	if !errors.Is(err, context.Canceled) || reader.read != 0 {
		t.Fatalf("read after cancellation: %d, %v", reader.read, err)
	}
}
