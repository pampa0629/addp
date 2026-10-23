package obj

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestOBJCompositionUsesEntireMTLNameAndCompleteInput(t *testing.T) {
	provider, err := format.GetModel3DCompositionProvider(format.FormatOBJ)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("# material declaration may occur after geometry\n" + strings.Repeat("v 0 0 0\n", 20000) +
		"mtllib two  names.mtl\r\nmtllib other.mtl\nmtllib two  names.mtl\n")
	reader := &compositionReader{raw: raw}
	refs, err := provider.ReadModel3DComposition(t.Context(), reader, int64(len(raw)))
	if err != nil || !reflect.DeepEqual(refs, []string{"two  names.mtl", "other.mtl"}) {
		t.Fatalf("refs=%#v err=%v", refs, err)
	}
	if reader.calls < 2 || reader.read != len(raw) {
		t.Fatalf("must consume full range input: calls=%d bytes=%d/%d", reader.calls, reader.read, len(raw))
	}
	refs, err = provider.ReadModel3DComposition(t.Context(), strings.NewReader("v 0 0 0\n"), 8)
	if err != nil || refs == nil || len(refs) != 0 {
		t.Fatalf("known empty OBJ: refs=%#v err=%v", refs, err)
	}
}

func TestMTLCompositionPreservesNamesAndParsesAssimpOptionArities(t *testing.T) {
	raw := `# named material, multiple maps and exact duplicate
newmtl paint
Kd 1 1 1
map_Kd -clamp on -o -1 0 1 -s 1 1 1 textures/albedo  red.png
map_Ka -mm 0 1 -boost 1.5 -texres 512 -blendu off -blendv on textures/ambient.JPG
bump -bm 0.25 -imfchan r textures/normal%20literal.jpeg
map_Ke -t 0 0 0 textures/albedo  red.png
`
	refs, err := ReadMTLTextureRefs(t.Context(), strings.NewReader(raw), int64(len(raw)))
	want := []string{"textures/albedo  red.png", "textures/ambient.JPG", "textures/normal%20literal.jpeg"}
	if err != nil || !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs=%#v want=%#v err=%v", refs, want, err)
	}
	refs, err = ReadMTLTextureRefs(t.Context(), strings.NewReader(""), 0)
	if err != nil || refs == nil || len(refs) != 0 {
		t.Fatalf("empty MTL must remain explicit: %#v %v", refs, err)
	}
	for _, keyword := range []string{"map_Ks", "map_d", "map_emissive", "map_bump", "map_Kn", "norm", "map_disp", "disp", "map_Ns", "map_Pr", "map_Pm", "map_Ps"} {
		t.Run(keyword, func(t *testing.T) {
			text := keyword + " image.png\n"
			refs, err := ReadMTLTextureRefs(t.Context(), strings.NewReader(text), int64(len(text)))
			if err != nil || !reflect.DeepEqual(refs, []string{"image.png"}) {
				t.Fatalf("refs=%#v err=%v", refs, err)
			}
		})
	}
}

func TestOBJCompositionRejectsAmbiguousOrExternalDependencySyntaxAtomically(t *testing.T) {
	for _, tail := range []string{
		"mtllib", "mtllib a.mtl # comment", "mtllib \"a b.mtl\"", "mtllib 'a.mtl'",
		"mtllib dir\\a.mtl", "mtllib a.mtl\\\nother.mtl", "MTLLIB a.mtl", "mtllib_extra a.mtl",
		"mtllib a.mtl ", "mtllib a.mtl\t",
		"call model.obj", "csh command", "maplib old.map", "usemap old", "shadow_obj shadow.obj", "trace_obj trace.obj",
		"v 0 \x00 0", "v \xff 0 0", "\ufeffmtllib a.mtl", strings.Repeat("#", MaxCompositionLineBytes+1),
		"mtllib " + strings.Repeat("a", MaxCompositionNameBytes+1),
	} {
		raw := "mtllib valid.mtl\n" + tail + "\n"
		refs, err := NewPlugin().ReadModel3DComposition(t.Context(), strings.NewReader(raw), int64(len(raw)))
		if err == nil || refs != nil {
			t.Fatalf("must reject without partial refs, input=%q refs=%#v err=%v", tail[:min(len(tail), 100)], refs, err)
		}
	}
}

func TestMTLCompositionRejectsUnknownAndMalformedStatementsAtomically(t *testing.T) {
	for _, tail := range []string{
		"map_Kd", "map_Kd -unknown 1 t.png", "map_Kd -o 1 2 t.png", "map_Kd -s 1 t.png",
		"map_Kd -t 0 0 t.png", "map_Kd -mm 1 t.png", "map_Kd -clamp yes t.png",
		"map_Kd -boost NaN t.png", "map_Kd -bm Inf t.png", "map_Kd -texres 0 t.png",
		"map_Kd -imfchan invalid t.png", "map_Kd -clamp on", "map_Kd -type sphere t.png",
		"map_Kd -mm 0 1", "map_Kd \"a b.png\"", "map_Kd t.png # note",
		"map_Kd dir\\t.png", "map_Kd t.png\\\ncontinued", "map_unknown t.png", "map_Kd_extra t.png",
		"map_Kd t.png ", "map_Kd t.png\t",
		"refl t.png", "decal t.png", "call other.mtl", "mystery t.png",
	} {
		raw := "newmtl paint\nmap_Kd valid.png\n" + tail + "\n"
		refs, err := ReadMTLTextureRefs(t.Context(), strings.NewReader(raw), int64(len(raw)))
		if err == nil || refs != nil {
			t.Fatalf("must reject without partial refs, input=%q refs=%#v err=%v", tail, refs, err)
		}
	}
}

func TestOBJAndMTLCompositionEnforceReadAndDeclarationBudgets(t *testing.T) {
	for _, material := range []bool{false, true} {
		parse := NewPlugin().ReadModel3DComposition
		line := "mtllib same.mtl\n"
		if material {
			parse = ReadMTLTextureRefs
			line = "map_Kd same.png\n"
		}
		raw := []byte(strings.Repeat(line, MaxCompositionRefs+1))
		if refs, err := parse(t.Context(), bytes.NewReader(raw), int64(len(raw))); err == nil || refs != nil {
			t.Fatalf("repeated declarations must count: %#v %v", refs, err)
		}
		boundary := []byte(strings.Repeat(line, MaxCompositionRefs))
		if refs, err := parse(t.Context(), bytes.NewReader(boundary), int64(len(boundary))); err != nil || len(refs) != 1 {
			t.Fatalf("exact declaration budget must be valid: %#v %v", refs, err)
		}
		for _, size := range []int64{-1, MaxCompositionBytes + 1} {
			r := &compositionReader{raw: raw}
			if refs, err := parse(t.Context(), r, size); err == nil || refs != nil || r.calls != 0 {
				t.Fatalf("invalid size made I/O: size=%d refs=%#v err=%v calls=%d", size, refs, err, r.calls)
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		r := &compositionReader{raw: raw}
		if refs, err := parse(ctx, r, int64(len(raw))); !errors.Is(err, context.Canceled) || refs != nil || r.calls != 0 {
			t.Fatalf("canceled parser made I/O: %#v %v calls=%d", refs, err, r.calls)
		}
		if refs, err := parse(t.Context(), nil, 4); err == nil || refs != nil {
			t.Fatalf("nil reader: %#v %v", refs, err)
		}
		short := []byte(line)
		if refs, err := parse(t.Context(), bytes.NewReader(short[:len(short)-2]), int64(len(short))); err == nil || refs != nil {
			t.Fatalf("truncation published refs: %#v %v", refs, err)
		}
		large := []byte(line + strings.Repeat("# no deps here\n", 12000))
		if refs, err := parse(t.Context(), failedCompositionReader{large}, int64(len(large))); err == nil || refs != nil {
			t.Fatalf("late I/O failure published refs: %#v %v", refs, err)
		}
	}
}

type compositionReader struct {
	raw   []byte
	calls int
	read  int
}

func (r *compositionReader) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	n, err := bytes.NewReader(r.raw).ReadAt(p, off)
	r.read += n
	return n, err
}

type failedCompositionReader struct{ raw []byte }

func (r failedCompositionReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= 64<<10 {
		return 0, errors.New("fixture late range error")
	}
	return bytes.NewReader(r.raw).ReadAt(p, off)
}
