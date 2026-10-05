package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"io"
	"testing"

	"github.com/addp/common/format"
)

func animationGIF(t *testing.T, delays ...int) []byte {
	t.Helper()
	g := &gif.GIF{Delay: delays, LoopCount: 0, Config: image.Config{Width: 7, Height: 5}}
	for range delays {
		g.Image = append(g.Image, image.NewPaletted(image.Rect(0, 0, 7, 5), color.Palette{color.Black, color.White}))
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func animationChunk(kind string, payload []byte) []byte {
	b := append([]byte(kind), binary.LittleEndian.AppendUint32(nil, uint32(len(payload)))...)
	b = append(b, payload...)
	if len(payload)&1 != 0 {
		b = append(b, 0)
	}
	return b
}
func animationWebP(durations ...int) []byte {
	chunks := animationChunk("VP8X", []byte{2, 0, 0, 0, 6, 0, 0, 4, 0, 0})
	chunks = append(chunks, animationChunk("ANIM", make([]byte, 6))...)
	for _, delay := range durations {
		frame := []byte{0, 0, 0, 0, 0, 0, 6, 0, 0, 4, 0, 0, byte(delay), byte(delay >> 8), byte(delay >> 16), 0}
		frame = append(frame, animationChunk("VP8L", []byte{0x2f, 6, 0, 1, 0})...)
		chunks = append(chunks, animationChunk("ANMF", frame)...)
	}
	return animationRIFF(chunks)
}
func animationRIFF(chunks []byte) []byte {
	b := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(4+len(chunks)))...)
	return append(append(b, []byte("WEBP")...), chunks...)
}
func animationResult(t *testing.T, kind format.FormatType, data []byte) map[string]interface{} {
	t.Helper()
	result, err := newPlugin(kind).DescribeMedia(t.Context(), bytes.NewBuffer(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Media == nil || result.Media.Width != 7 || result.Media.Height != 5 || result.Media.Kind != "image" || result.Media.DurationMS != nil {
		t.Fatalf("wrong media: %#v", result)
	}
	return result.FormatInfo["animation"].(map[string]interface{})
}
func requireAnimationStatus(t *testing.T, info map[string]interface{}, status string) {
	t.Helper()
	if info["summary_status"] != status {
		t.Fatalf("unexpected status: %#v", info)
	}
	if status != "parsed" && (info["frame_count"] != nil || info["duration_ms"] != nil) {
		t.Fatalf("partial summary leaked: %#v", info)
	}
}
func TestAnimationEncodedDelaysAndCompleteFrameCounts(t *testing.T) {
	for _, tt := range []struct {
		kind     format.FormatType
		data     []byte
		frames   int
		duration int64
	}{
		{format.FormatGIF, animationGIF(t, 0, 2, 9), 3, 110},
		{format.FormatGIF, animationGIF(t, 0), 1, 0},
		{format.FormatWebP, animationWebP(0, 2, 9), 3, 11},
		{format.FormatWebP, animationWebP(0xffffff), 1, 0xffffff},
	} {
		info := animationResult(t, tt.kind, tt.data)
		requireAnimationStatus(t, info, "parsed")
		if info["frame_count"] != tt.frames || info["duration_ms"] != tt.duration {
			t.Fatalf("wrong timing: %#v", info)
		}
	}
	// Existing generic image provider must consume the same animation route.
	result, err := newPlugin(format.FormatImage).DescribeMedia(t.Context(), bytes.NewReader(animationGIF(t, 2, 3)), nil)
	if err != nil || result.FormatInfo["animation"].(map[string]interface{})["duration_ms"] != int64(50) {
		t.Fatalf("generic route: %#v %v", result, err)
	}
}
func TestAnimationTruncationNeverPublishesPartialCounts(t *testing.T) {
	for _, tt := range []struct {
		kind format.FormatType
		data []byte
	}{
		{format.FormatGIF, animationGIF(t, 2, 3)}, {format.FormatWebP, animationWebP(2, 3)},
	} {
		// DecodeConfig establishes dimensions before the truncated frame tail.
		for removed := 1; removed < 12; removed++ {
			requireAnimationStatus(t, animationResult(t, tt.kind, tt.data[:len(tt.data)-removed]), "invalid")
		}
	}
}
func TestGIFControlScopeAndUnsupportedRendering(t *testing.T) {
	data := animationGIF(t, 2, 3)
	first := bytes.Index(data, []byte{0x21, 0xf9, 4})
	if first < 0 {
		t.Fatal("missing GCE")
	}
	comment := []byte{0x21, 0xfe, 3, 'a', 'b', 'c', 0}
	withComment := append(append(append([]byte{}, data[:first+8]...), comment...), data[first+8:]...)
	if info := animationResult(t, format.FormatGIF, withComment); info["duration_ms"] != int64(50) {
		t.Fatalf("comment consumed GCE: %#v", info)
	}
	duplicate := append(append(append([]byte{}, data[:first+8]...), data[first:first+8]...), data[first+8:]...)
	requireAnimationStatus(t, animationResult(t, format.FormatGIF, duplicate), "invalid")
	interactive := bytes.Clone(data)
	interactive[first+3] |= 2
	requireAnimationStatus(t, animationResult(t, format.FormatGIF, interactive), "unsupported")
	plainText := append(append([]byte{}, data[:first]...), append([]byte{0x21, 1}, data[first:]...)...)
	requireAnimationStatus(t, animationResult(t, format.FormatGIF, plainText), "unsupported")
	outside := bytes.Clone(data)
	frame := bytes.Index(outside, []byte{0x2c, 0, 0, 0, 0, 7, 0, 5, 0})
	outside[frame+1] = 7
	requireAnimationStatus(t, animationResult(t, format.FormatGIF, outside), "invalid")
	// With no compressed data, a descriptor does not establish a valid frame.
	firstFrame := bytes.Index(data, []byte{0x2c, 0, 0, 0, 0, 7, 0, 5, 0})
	dataStart := firstFrame + 11 // descriptor and LZW minimum code size
	if packed := data[firstFrame+9]; packed&0x80 != 0 {
		dataStart += 3 << (packed&7 + 1)
	}
	empty := append(bytes.Clone(data[:dataStart]), 0, 0x3b)
	requireAnimationStatus(t, animationResult(t, format.FormatGIF, empty), "invalid")
	dangling := append(append([]byte{}, data[:len(data)-1]...), append(data[first:first+8], 0x3b)...)
	requireAnimationStatus(t, animationResult(t, format.FormatGIF, dangling), "invalid")
}
func TestWebPContainerAndFrameBoundaries(t *testing.T) {
	canvas := animationChunk("VP8X", []byte{2, 0, 0, 0, 6, 0, 0, 4, 0, 0})
	anim := animationChunk("ANIM", make([]byte, 6))
	valid := animationWebP(10)[12:]
	frame := valid[len(canvas)+len(anim):]
	tests := [][]byte{
		canvas,
		append(bytes.Clone(canvas), frame...),
		append(append(bytes.Clone(canvas), anim...), anim...),
		append(append(bytes.Clone(valid), anim...), frame...),
		append(append(bytes.Clone(canvas), anim...), animationChunk("ANMF", make([]byte, 16))...),
	}
	for _, chunks := range tests {
		requireAnimationStatus(t, animationResult(t, format.FormatWebP, animationRIFF(chunks)), "invalid")
	}
	outside := animationWebP(10)
	outside[52] = 4
	requireAnimationStatus(t, animationResult(t, format.FormatWebP, outside), "invalid")
	padded := animationWebP(10)
	padded[len(padded)-1] = 1
	requireAnimationStatus(t, animationResult(t, format.FormatWebP, padded), "invalid")
	oversized := animationWebP(10)
	binary.LittleEndian.PutUint32(oversized[48:52], 0xffffffff)
	requireAnimationStatus(t, animationResult(t, format.FormatWebP, oversized), "invalid")
}
func TestAnimationBudgetsAreSharedWithHeaderReads(t *testing.T) {
	valid := animationWebP(1)
	huge := append(bytes.Clone(valid[12:]), animationChunk("JUNK", make([]byte, animationReadLimit))...)
	source := &countedHeaderReader{Reader: bytes.NewReader(animationRIFF(huge))}
	result, err := newPlugin(format.FormatWebP).DescribeMedia(t.Context(), source, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireAnimationStatus(t, result.FormatInfo["animation"].(map[string]interface{}), "budget_exceeded")
	if source.read != animationReadLimit {
		t.Fatalf("read budget: %d", source.read)
	}
	durations := make([]int, animationFrameLimit+1)
	info := animationResult(t, format.FormatWebP, animationWebP(durations[:animationFrameLimit]...))
	requireAnimationStatus(t, info, "parsed")
	if info["frame_count"] != animationFrameLimit {
		t.Fatalf("frame limit boundary: %#v", info)
	}
	requireAnimationStatus(t, animationResult(t, format.FormatWebP, animationWebP(durations...)), "budget_exceeded")
	gifData := animationGIF(t, 1)
	blocks := bytes.Repeat([]byte{0x21, 0xfe, 0}, animationBlockLimit)
	gifData = append(append(bytes.Clone(gifData[:len(gifData)-1]), blocks...), 0x3b)
	requireAnimationStatus(t, animationResult(t, format.FormatGIF, gifData), "budget_exceeded")
}

type animationErrorReader struct{ err error }

func (r animationErrorReader) Read([]byte) (int, error) { return 0, r.err }
func TestAnimationIOAndCancellationPropagate(t *testing.T) {
	data := animationGIF(t, 2)
	sentinel := errors.New("source read failed")
	input := io.MultiReader(bytes.NewReader(data[:len(data)-1]), animationErrorReader{sentinel})
	_, err := newPlugin(format.FormatGIF).DescribeMedia(t.Context(), input, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("I/O swallowed: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = newPlugin(format.FormatWebP).DescribeMedia(ctx, bytes.NewReader(animationWebP(1)), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel swallowed: %v", err)
	}
}
