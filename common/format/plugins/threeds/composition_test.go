package threeds

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

type observedRanges struct {
	data      []byte
	ranges    [][2]int64
	forbidden [2]int64
}

func (r *observedRanges) ReadAt(p []byte, offset int64) (int, error) {
	if offset < r.forbidden[1] && offset+int64(len(p)) > r.forbidden[0] {
		return 0, fmt.Errorf("geometry payload opened")
	}
	r.ranges = append(r.ranges, [2]int64{offset, int64(len(p))})
	return bytes.NewReader(r.data).ReadAt(p, offset)
}

func TestCompositionSkipsGeometryAndKeepsExactDeclarations(t *testing.T) {
	geometry := chunk(0x4000, bytes.Repeat([]byte{0xff}, 1<<20))
	material := chunk(0xafff, append(chunk(0xa200, chunk(0xa300, []byte(" Textures/A.PNG\x00"))),
		chunk(0xa204, chunk(0xa300, []byte("Textures/a.png\x00")))...))
	data := chunk(0x4d4d, chunk(0x3d3d, append(geometry, material...)))
	r := &observedRanges{data: data, forbidden: [2]int64{18, int64(12 + len(geometry))}}
	p, err := format.GetModel3DCompositionProvider(format.Format3DS)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := p.ReadModel3DComposition(t.Context(), r, int64(len(data)))
	if err != nil || !reflect.DeepEqual(refs, []string{" Textures/A.PNG", "Textures/a.png"}) {
		t.Fatalf("refs=%v err=%v", refs, err)
	}
	var actual int64
	for _, interval := range r.ranges {
		actual += interval[1]
	}
	if actual >= 256 || len(r.ranges) > 10 {
		t.Fatalf("unexpected structure I/O: %v", r.ranges)
	}
}

func TestCompositionRejectsTruncationBudgetsAndInvalidNamesWithoutPartialFacts(t *testing.T) {
	texture := func(name string) []byte { return chunk(0xafff, chunk(0xa200, chunk(0xa300, []byte(name)))) }
	valid := chunk(0x4d4d, chunk(0x3d3d, texture("a.png\x00")))
	oversizedChunk := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint32(oversizedChunk[14:18], 0xffffffff)
	deep := texture("a.png\x00")
	for i := 0; i < 65; i++ {
		deep = chunk(0x3d3d, deep)
	}
	cases := [][]byte{nil, valid[:5], valid[:len(valid)-1], oversizedChunk,
		chunk(0x4d4d, chunk(0x3d3d, append(texture("ok.png\x00"), texture("not-terminated")...))),
		chunk(0x4d4d, chunk(0x3d3d, texture("a\x00b\x00"))),
		chunk(0x4d4d, chunk(0x3d3d, texture("\xff\x00"))),
		chunk(0x4d4d, deep),
		chunk(0x4d4d, chunk(0x3d3d, texture(strings.Repeat("a", maxCompositionName)+"\x00"))),
		chunk(0x4d4d, chunk(0x3d3d, bytes.Repeat(texture("a.png\x00"), 201))),
		chunk(0x4d4d, bytes.Repeat(chunk(0x0000, nil), maxCompositionReads)),
		chunk(0x4d4d, chunk(0x3d3d, bytes.Repeat(texture(strings.Repeat("a", maxCompositionName-1)+"\x00"), 65))),
	}
	for i, data := range cases {
		refs, err := NewPlugin().ReadModel3DComposition(t.Context(), bytes.NewReader(data), int64(len(data)))
		if err == nil || refs != nil {
			t.Fatalf("case %d returned partial/complete facts: refs=%v err=%v", i, refs, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &observedRanges{data: valid}
	if _, err := NewPlugin().ReadModel3DComposition(ctx, r, int64(len(valid))); err == nil || len(r.ranges) != 0 {
		t.Fatal("cancelled read performed I/O")
	}
	for _, size := range []int64{-1, MaxSummaryBytes + 1} {
		if _, err := NewPlugin().ReadModel3DComposition(t.Context(), r, size); err == nil || len(r.ranges) != 0 {
			t.Fatal("size budget performed I/O")
		}
	}
}

type shortCompositionReader struct{}

func (shortCompositionReader) ReadAt(p []byte, off int64) (int, error) { return 0, io.EOF }

func TestCompositionEmptyAndRepeatedReferencesAreCompleteOnlyOnSuccess(t *testing.T) {
	data := chunk(0x4d4d, nil)
	refs, err := NewPlugin().ReadModel3DComposition(t.Context(), bytes.NewReader(data), int64(len(data)))
	if err != nil || refs == nil || len(refs) != 0 {
		t.Fatalf("empty refs=%v err=%v", refs, err)
	}
	data = chunk(0x4d4d, chunk(0x3d3d, bytes.Repeat(chunk(0xafff, chunk(0xa200, chunk(0xa300, []byte("a.png\x00")))), 2)))
	refs, err = NewPlugin().ReadModel3DComposition(t.Context(), bytes.NewReader(data), int64(len(data)))
	if err != nil || !reflect.DeepEqual(refs, []string{"a.png"}) {
		t.Fatalf("repeated refs=%v err=%v", refs, err)
	}
	if refs, err = NewPlugin().ReadModel3DComposition(t.Context(), shortCompositionReader{}, 6); err == nil || refs != nil {
		t.Fatal("short read returned facts")
	}
}
