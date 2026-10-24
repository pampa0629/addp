package stl

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

const compositionTriangle = "solid sample\nfacet normal 0 0 1\nouter loop\nvertex 0 0 0\nvertex 2 0 0\nvertex 0 1 0\nendloop\nendfacet\nendsolid sample\n"

func TestSTLCompositionCompleteASCIIHasNoExternalDeclarations(t *testing.T) {
	for _, source := range []string{compositionTriangle, strings.ReplaceAll(compositionTriangle, "\n", "\r\n"), compositionTriangle + compositionTriangle, "solid empty\nendsolid empty\n", "\n\t" + strings.ReplaceAll(compositionTriangle, " 0 0 0", " -1.2e-3 +0 .5") + "\n"} {
		refs, err := NewPlugin().ReadModel3DComposition(context.Background(), strings.NewReader(source), int64(len(source)))
		if err != nil || refs == nil || len(refs) != 0 {
			t.Fatalf("complete ASCII composition: refs=%v err=%v", refs, err)
		}
	}
}

func TestSTLCompositionRejectsIncompleteAndForeignASCII(t *testing.T) {
	for _, source := range []string{
		"", "solid missing-end", "not STL", "solid x\nmtllib texture.mtl\nendsolid x\n",
		strings.Replace(compositionTriangle, "endfacet\n", "", 1), strings.Replace(compositionTriangle, "endloop\n", "", 1),
		strings.Replace(compositionTriangle, "vertex 0 1 0\n", "", 1), strings.Replace(compositionTriangle, "endloop\n", "vertex 1 1 1\nendloop\n", 1),
		strings.Replace(compositionTriangle, "normal 0 0 1", "normal NaN 0 1", 1), strings.Replace(compositionTriangle, "vertex 0 0 0", "vertex 0x1p1 0 0", 1),
		strings.Replace(compositionTriangle, "vertex 0 0 0", "vertex 1e9999 0 0", 1), strings.Replace(compositionTriangle, "outer loop", "outer anything", 1),
		compositionTriangle + "ignored text\n", strings.Replace(compositionTriangle, "solid sample", "solid\x00sample", 1),
		"\xef\xbb\xbf" + compositionTriangle, "solid " + strings.Repeat("x", MaxCompositionLineBytes) + "\nendsolid\n",
	} {
		refs, err := NewPlugin().ReadModel3DComposition(context.Background(), strings.NewReader(source), int64(len(source)))
		if err == nil || refs != nil {
			t.Fatalf("accepted incomplete composition (%d bytes): %v %v", len(source), refs, err)
		}
	}
}

type boundedBinaryComposition struct {
	header [84]byte
	size   int64
	reads  int
	bytes  int
}

func (r *boundedBinaryComposition) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	r.bytes += len(p)
	if off == 0 && len(p) == 84 {
		copy(p, r.header[:])
		return len(p), nil
	}
	if off == r.size-1 && len(p) == 1 {
		p[0] = 0
		return 1, nil
	}
	return 0, errors.New("unexpected geometry read")
}
func TestSTLCompositionBinaryUsesObservedLengthAndBoundedReads(t *testing.T) {
	for _, triangles := range []uint32{0, 1, 2000001, 0xffffffff} {
		r := &boundedBinaryComposition{size: 84 + 50*int64(triangles)}
		copy(r.header[:], "solid misleading facet normal mtllib texture.png")
		binary.LittleEndian.PutUint32(r.header[80:84], triangles)
		refs, err := NewPlugin().ReadModel3DComposition(context.Background(), r, r.size)
		if err != nil || refs == nil || len(refs) != 0 || r.bytes > 85 || r.reads > 2 {
			t.Fatalf("bounded binary: %v %v %+v", refs, err, r)
		}
	}
}
func TestSTLCompositionRejectsStaleLengthShortReadsAndCancellation(t *testing.T) {
	header := make([]byte, 84)
	binary.LittleEndian.PutUint32(header[80:], 1)
	for _, size := range []int64{83, 84, 133, 134, 135} {
		if _, err := NewPlugin().ReadModel3DComposition(context.Background(), bytes.NewReader(header), size); err == nil {
			t.Fatalf("accepted truncated/stale binary length %d", size)
		}
	}
	full := append(header, make([]byte, 50)...)
	if refs, err := NewPlugin().ReadModel3DComposition(context.Background(), bytes.NewReader(full), 134); err != nil || len(refs) != 0 {
		t.Fatal(refs, err)
	}
	if _, err := NewPlugin().ReadModel3DComposition(context.Background(), strings.NewReader(compositionTriangle), int64(len(compositionTriangle)+1)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	if _, err := NewPlugin().ReadModel3DComposition(context.Background(), nil, 100); err == nil {
		t.Fatal("nil primary accepted")
	}
	if _, err := NewPlugin().ReadModel3DComposition(context.Background(), bytes.NewReader(nil), MaxCompositionASCIIBytes+1); err == nil {
		t.Fatal("over-budget ASCII accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewPlugin().ReadModel3DComposition(ctx, bytes.NewReader(full), 134); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type cancellingCompositionReader struct {
	raw    *bytes.Reader
	cancel context.CancelFunc
}

func (r cancellingCompositionReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.raw.ReadAt(p, off)
	r.cancel()
	return n, err
}
func TestSTLCompositionCancellationDuringLastReadNeverBecomesComplete(t *testing.T) {
	for _, data := range [][]byte{make([]byte, 84), []byte("solid empty\nendsolid empty\n")} {
		ctx, cancel := context.WithCancel(context.Background())
		refs, err := NewPlugin().ReadModel3DComposition(ctx, cancellingCompositionReader{bytes.NewReader(data), cancel}, int64(len(data)))
		if !errors.Is(err, context.Canceled) || refs != nil {
			t.Fatal(refs, err)
		}
	}
}
func TestSTLDeepSummaryCountsASCIIBlocksIndependentlyOfComposition(t *testing.T) {
	content := compositionTriangle + compositionTriangle
	result, err := NewPlugin().DescribeModel3D(context.Background(), strings.NewReader(content), nil)
	if err != nil || result.Model3D.MeshCount == nil || *result.Model3D.MeshCount != 2 {
		t.Fatal(result, err)
	}
	if _, exists := result.FormatInfo["composition_complete"]; exists {
		t.Fatal("deep summary manufactured composition")
	}
}
