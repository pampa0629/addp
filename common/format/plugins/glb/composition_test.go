package glb

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

const embeddedGLBJSON = `{"asset":{"version":"2.0"},"buffers":[{"byteLength":5}],"bufferViews":[{"buffer":0,"byteOffset":1,"byteLength":4}],"images":[{"bufferView":0,"mimeType":"image/png"}]}`

func compositionGLB(doc string, bin []byte) []byte {
	raw := buildGLB([]byte(doc))
	if bin != nil {
		payload := append([]byte{}, bin...)
		for len(payload)%4 != 0 {
			payload = append(payload, 0)
		}
		raw = binary.LittleEndian.AppendUint32(raw, uint32(len(payload)))
		raw = binary.LittleEndian.AppendUint32(raw, glbBINChunkType)
		raw = append(raw, payload...)
		binary.LittleEndian.PutUint32(raw[8:12], uint32(len(raw)))
	}
	return raw
}

func TestGLBCompositionProvesExactlyEmptyExternalReferences(t *testing.T) {
	provider, err := format.GetModel3DCompositionProvider(format.FormatGLB)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{
		"JSON only":                compositionGLB(`{"asset":{"version":"2.0"}}`, nil),
		"embedded PNG":             compositionGLB(embeddedGLBJSON, []byte{1, 2, 3, 4, 5}),
		"embedded JPEG":            compositionGLB(strings.ReplaceAll(embeddedGLBJSON, "image/png", "image/jpeg"), []byte{1, 2, 3, 4, 5}),
		"integer decimal notation": compositionGLB(strings.ReplaceAll(embeddedGLBJSON, `"byteLength":5`, `"byteLength":5.0e0`), []byte{1, 2, 3, 4, 5}),
		"triangle scene":           triangleCompositionGLB(),
		"empty declarations":       compositionGLB(`{"asset":{"version":"2.0","minVersion":"2.0"},"extensionsUsed":[],"extensionsRequired":[],"extensions":{},"buffers":[],"bufferViews":[],"images":[]}`, nil),
	} {
		t.Run(name, func(t *testing.T) {
			refs, err := provider.ReadModel3DComposition(t.Context(), bytes.NewReader(raw), int64(len(raw)))
			if err != nil || refs == nil || len(refs) != 0 {
				t.Fatalf("refs=%v err=%v; want a proved empty declaration set", refs, err)
			}
		})
	}
}

// This small core glTF scene is also suitable for the owning consumer's real
// render acceptance. Passing a composition check alone does not prove rendering.
func triangleCompositionGLB() []byte {
	var bin []byte
	for _, value := range []float32{0, 0, 0, 1, 0, 0, 0, 1, 0} {
		bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(value))
	}
	for _, index := range []uint16{0, 1, 2} {
		bin = binary.LittleEndian.AppendUint16(bin, index)
	}
	return compositionGLB(`{"asset":{"version":"2.0"},"buffers":[{"byteLength":42}],"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":36,"target":34962},{"buffer":0,"byteOffset":36,"byteLength":6,"target":34963}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3","min":[0,0,0],"max":[1,1,0]},{"bufferView":1,"componentType":5123,"count":3,"type":"SCALAR"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`, bin)
}

func TestGLBCompositionRejectsAmbiguousOrExternalResourceDeclarations(t *testing.T) {
	for name, doc := range map[string]string{
		"missing asset":             `{}`,
		"wrong asset type":          `{"asset":[]}`,
		"version 1":                 `{"asset":{"version":"1.0"}}`,
		"minimum future version":    `{"asset":{"version":"2.0","minVersion":"2.1"}}`,
		"buffer URI":                strings.Replace(embeddedGLBJSON, `"byteLength":5`, `"byteLength":5,"uri":"../secret.bin"`, 1),
		"data buffer":               strings.Replace(embeddedGLBJSON, `"byteLength":5`, `"byteLength":5,"uri":"data:application/octet-stream;base64,AAAA"`, 1),
		"image URL":                 strings.Replace(embeddedGLBJSON, `"mimeType":"image/png"`, `"mimeType":"image/png","uri":"https://example.invalid/a.png"`, 1),
		"data image":                strings.Replace(embeddedGLBJSON, `"mimeType":"image/png"`, `"mimeType":"image/png","uri":"data:image/png;base64,AAAA"`, 1),
		"empty URI":                 strings.Replace(embeddedGLBJSON, `"mimeType":"image/png"`, `"mimeType":"image/png","uri":""`, 1),
		"null URI":                  strings.Replace(embeddedGLBJSON, `"mimeType":"image/png"`, `"mimeType":"image/png","uri":null`, 1),
		"escaped URI key":           strings.Replace(embeddedGLBJSON, `"mimeType":"image/png"`, `"mimeType":"image/png","\u0075ri":"external.png"`, 1),
		"URI in extras":             `{"asset":{"version":"2.0"},"extras":{"uri":"other.bin"}}`,
		"used extension":            `{"asset":{"version":"2.0"},"extensionsUsed":["KHR_materials_unlit"]}`,
		"required extension":        `{"asset":{"version":"2.0"},"extensionsRequired":["UNKNOWN_resource"]}`,
		"undeclared extension":      `{"asset":{"version":"2.0"},"nodes":[{"extensions":{"UNKNOWN_resource":{"url":"secret"}}}]}`,
		"null extension":            `{"asset":{"version":"2.0"},"extensions":null}`,
		"wrong extension list type": `{"asset":{"version":"2.0"},"extensionsUsed":{}}`,
		"duplicate keys":            `{"asset":{"version":"2.0"},"buffers":[{"uri":"a.bin"}],"buffers":[]}`,
		"escaped duplicate keys":    `{"asset":{"version":"2.0"},"extras":{"x":1,"\u0078":2}}`,
		"trailing document":         `{"asset":{"version":"2.0"}} {}`,
		"root array":                `[]`,
		"truncated JSON":            `{"asset":{"version":"2.0"}`,
		"invalid UTF8":              "{\"asset\":{\"version\":\"2.0\"},\"extras\":\"\xff\"}",
	} {
		t.Run(name, func(t *testing.T) {
			assertBadComposition(t, compositionGLB(doc, []byte{1, 2, 3, 4, 5}))
		})
	}
}

func TestGLBCompositionRejectsInvalidEmbeddedRanges(t *testing.T) {
	for name, replacement := range map[string][2]string{
		"buffer count":             {`"buffers":[{"byteLength":5}]`, `"buffers":[{"byteLength":5},{"byteLength":1}]`},
		"null resource array":      {`"buffers":[{"byteLength":5}]`, `"buffers":null`},
		"bad resource entry":       {`"buffers":[{"byteLength":5}]`, `"buffers":[null]`},
		"missing buffer length":    {`"byteLength":5`, `"name":"x"`},
		"zero buffer length":       {`"byteLength":5`, `"byteLength":0`},
		"too much padding":         {`"byteLength":5`, `"byteLength":4`},
		"buffer exceeds BIN":       {`"byteLength":5`, `"byteLength":9`},
		"boolean buffer length":    {`"byteLength":5`, `"byteLength":true`},
		"fractional buffer length": {`"byteLength":5`, `"byteLength":5.00000000000000000000000000001`},
		"negative exponent":        {`"byteLength":5`, `"byteLength":5e-999999999`},
		"huge exponent":            {`"byteLength":5`, `"byteLength":5e999999999`},
		"negative offset":          {`"byteOffset":1`, `"byteOffset":-1`},
		"overflow offset":          {`"byteOffset":1`, `"byteOffset":9223372036854775808`},
		"out of range view":        {`"byteOffset":1`, `"byteOffset":5`},
		"wrong buffer":             {`"buffer":0`, `"buffer":1`},
		"negative buffer":          {`"buffer":0`, `"buffer":-1`},
		"zero view length":         {`"byteLength":4`, `"byteLength":0`},
		"bad view length":          {`"byteLength":4`, `"byteLength":"4"`},
		"view exceeds declaration": {`"byteLength":4`, `"byteLength":5`},
		"missing image view":       {`"bufferView":0`, `"name":"x"`},
		"negative image view":      {`"bufferView":0`, `"bufferView":-1`},
		"out of range image view":  {`"bufferView":0`, `"bufferView":1`},
		"no image MIME":            {`"mimeType":"image/png"`, `"name":"x"`},
		"extension image MIME":     {`"mimeType":"image/png"`, `"mimeType":"image/ktx2"`},
	} {
		t.Run(name, func(t *testing.T) {
			doc := strings.Replace(embeddedGLBJSON, replacement[0], replacement[1], 1)
			assertBadComposition(t, compositionGLB(doc, []byte{1, 2, 3, 4, 5}))
		})
	}
	t.Run("resources without BIN", func(t *testing.T) {
		assertBadComposition(t, compositionGLB(embeddedGLBJSON, nil))
	})
	t.Run("BIN without resource", func(t *testing.T) {
		assertBadComposition(t, compositionGLB(`{"asset":{"version":"2.0"}}`, []byte{1, 2, 3, 4}))
	})
}

func TestGLBCompositionRejectsMalformedContainerAndPadding(t *testing.T) {
	for name, mutate := range map[string]func([]byte) []byte{
		"magic":           func(b []byte) []byte { b[0] = 0; return b },
		"version":         func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:8], 1); return b },
		"declared length": func(b []byte) []byte { binary.LittleEndian.PutUint32(b[8:12], uint32(len(b)-4)); return b },
		"JSON alignment":  func(b []byte) []byte { b[12]--; return b },
		"JSON over budget": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[12:16], uint32(MaxCompositionJSONBytes+4))
			return b
		},
		"first chunk BIN":      func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:20], glbBINChunkType); return b },
		"JSON NUL padding":     func(b []byte) []byte { n := binary.LittleEndian.Uint32(b[12:16]); b[19+n] = 0; return b },
		"JSON newline padding": func(b []byte) []byte { n := binary.LittleEndian.Uint32(b[12:16]); b[19+n] = '\n'; return b },
		"BIN nonzero padding":  func(b []byte) []byte { b[len(b)-1] = 1; return b },
		"BIN alignment":        func(b []byte) []byte { n := binary.LittleEndian.Uint32(b[12:16]); b[20+n]--; return b },
		"BIN duplicate JSON": func(b []byte) []byte {
			n := binary.LittleEndian.Uint32(b[12:16])
			binary.LittleEndian.PutUint32(b[24+n:28+n], glbJSONChunkType)
			return b
		},
		"unknown chunk": func(b []byte) []byte {
			n := binary.LittleEndian.Uint32(b[12:16])
			binary.LittleEndian.PutUint32(b[24+n:28+n], 0x12345678)
			return b
		},
		"extra chunk": func(b []byte) []byte {
			b = append(b, make([]byte, 8)...)
			binary.LittleEndian.PutUint32(b[8:12], uint32(len(b)))
			return b
		},
		"short BIN header": func(b []byte) []byte {
			n := binary.LittleEndian.Uint32(b[12:16])
			b = b[:24+n]
			binary.LittleEndian.PutUint32(b[8:12], uint32(len(b)))
			return b
		},
	} {
		t.Run(name, func(t *testing.T) {
			assertBadComposition(t, mutate(compositionGLB(embeddedGLBJSON, []byte{1, 2, 3, 4, 5})))
		})
	}
}

func assertBadComposition(t *testing.T, raw []byte) {
	t.Helper()
	refs, err := NewPlugin().ReadModel3DComposition(t.Context(), bytes.NewReader(raw), int64(len(raw)))
	if err == nil || refs != nil {
		t.Fatalf("refs=%v err=%v; invalid input returned a proof", refs, err)
	}
}

type compositionRange struct{ offset, length int64 }

type compositionReader struct {
	data   []byte
	ranges []compositionRange
	failAt int64
	cancel context.CancelFunc
}

func (r *compositionReader) ReadAt(p []byte, offset int64) (int, error) {
	r.ranges = append(r.ranges, compositionRange{offset, int64(len(p))})
	if r.cancel != nil {
		r.cancel()
	}
	if offset == r.failAt {
		return 0, io.ErrClosedPipe
	}
	return bytes.NewReader(r.data).ReadAt(p, offset)
}

func TestGLBCompositionReadsOnlyStructureAndEnd(t *testing.T) {
	bin := make([]byte, 8<<20)
	doc := `{"asset":{"version":"2.0"},"buffers":[{"byteLength":8388608}]}`
	raw := compositionGLB(doc, bin)
	r := &compositionReader{data: raw, failAt: -1}
	refs, err := NewPlugin().ReadModel3DComposition(t.Context(), r, int64(len(raw)))
	if err != nil || refs == nil {
		t.Fatal(refs, err)
	}
	jsonLength := int64(binary.LittleEndian.Uint32(raw[12:16]))
	want := []compositionRange{{0, 20}, {20, jsonLength}, {20 + jsonLength, 8}, {int64(len(raw)) - 1, 1}}
	if len(r.ranges) != len(want) {
		t.Fatalf("unexpected read count: %v", r.ranges)
	}
	for i := range want {
		if r.ranges[i] != want[i] {
			t.Fatalf("read range %d=%v, want %v; BIN payload must not be scanned", i, r.ranges[i], want[i])
		}
	}
}

func TestGLBCompositionRejectsSizeBudgetBeforeReading(t *testing.T) {
	for _, size := range []int64{-1, 0, 19, 21, MaxCompositionBytes + 4, 1 << 40} {
		r := &compositionReader{failAt: -1}
		refs, err := NewPlugin().ReadModel3DComposition(t.Context(), r, size)
		if err == nil || refs != nil || len(r.ranges) != 0 {
			t.Fatalf("size=%d refs=%v ranges=%v err=%v", size, refs, r.ranges, err)
		}
	}
	if refs, err := NewPlugin().ReadModel3DComposition(t.Context(), nil, 20); err == nil || refs != nil {
		t.Fatal("nil reader produced proof", refs, err)
	}
}

func TestGLBCompositionRejectsJSONComplexityBudget(t *testing.T) {
	assertBadComposition(t, compositionGLB(`{"asset":{"version":"2.0"},"extras":`+strings.Repeat("[", 65)+`0`+strings.Repeat("]", 65)+`}`, nil))
	assertBadComposition(t, compositionGLB(`{"asset":{"version":"2.0"},"extras":[`+strings.Repeat("0,", 200000)+`0]}`, nil))
}

func TestGLBCompositionIntegerDoesNotRoundDeclaredRanges(t *testing.T) {
	for _, tc := range []struct {
		literal string
		want    int64
		valid   bool
	}{
		{"0", 0, true}, {"-0", 0, true}, {"0e999999999", 0, true},
		{"1.0", 1, true}, {"1200e-2", 12, true}, {"0.01e2", 1, true},
		{"1e3", 1000, true}, {"1.2000e1", 12, true},
		{"9223372036854775807", 9223372036854775807, true},
		{"-1", 0, false}, {"1.000000000000000000001", 0, false},
		{"1e-999999999", 0, false}, {"1e999999999", 0, false},
		{"9223372036854775808", 0, false},
	} {
		t.Run(tc.literal, func(t *testing.T) {
			got, valid := compositionInteger(json.Number(tc.literal))
			if valid != tc.valid || valid && got != tc.want {
				t.Fatalf("integer=%d valid=%v, want %d/%v", got, valid, tc.want, tc.valid)
			}
		})
	}
}

func TestGLBCompositionNeverProvesPartialReadOrCancellation(t *testing.T) {
	raw := compositionGLB(embeddedGLBJSON, []byte{1, 2, 3, 4, 5})
	jsonLength := int64(binary.LittleEndian.Uint32(raw[12:16]))
	for _, offset := range []int64{0, 20, 20 + jsonLength, int64(len(raw)) - 3} {
		r := &compositionReader{data: raw, failAt: offset}
		refs, err := NewPlugin().ReadModel3DComposition(t.Context(), r, int64(len(raw)))
		if !errors.Is(err, io.ErrClosedPipe) || refs != nil {
			t.Fatalf("range failure at %d: refs=%v err=%v", offset, refs, err)
		}
	}
	refs, err := NewPlugin().ReadModel3DComposition(t.Context(), bytes.NewReader(raw[:len(raw)-4]), int64(len(raw)))
	if err == nil || refs != nil {
		t.Fatal("unreadable observed end produced proof", refs, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &compositionReader{data: raw, failAt: -1}
	refs, err = NewPlugin().ReadModel3DComposition(ctx, r, int64(len(raw)))
	if !errors.Is(err, context.Canceled) || refs != nil || len(r.ranges) != 0 {
		t.Fatal("pre-cancelled call read input or produced proof", refs, err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	r = &compositionReader{data: raw, failAt: -1, cancel: cancel}
	refs, err = NewPlugin().ReadModel3DComposition(ctx, r, int64(len(raw)))
	if !errors.Is(err, context.Canceled) || refs != nil {
		t.Fatal("interrupted call produced proof", refs, err)
	}
}
