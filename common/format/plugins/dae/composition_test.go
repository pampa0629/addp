package dae

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

type observedDAEReader struct {
	data  []byte
	calls int
	bytes int
}

func (r *observedDAEReader) ReadAt(p []byte, offset int64) (int, error) {
	r.calls++
	n, err := bytes.NewReader(r.data).ReadAt(p, offset)
	r.bytes += n
	return n, err
}
func daeCompositionXML(ref string) string {
	return `<COLLADA xmlns="http://www.collada.org/2005/11/COLLADASchema" version="1.4.1"><library_images><image><init_from>` + ref + `</init_from></image></library_images></COLLADA>`
}
func TestDAECompositionReadsCompletePrimaryWithoutGeometryFacts(t *testing.T) {
	p, err := format.GetModel3DCompositionProvider(format.FormatDAE)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(daeCompositionXML("textures/color%20A.PNG"))
	r := &observedDAEReader{data: raw}
	refs, err := p.ReadModel3DComposition(t.Context(), r, int64(len(raw)))
	if err != nil || len(refs) != 1 || refs[0] != "textures/color%20A.PNG" || r.bytes != len(raw) || r.calls != 1 {
		t.Fatalf("refs=%v read=%+v err=%v", refs, r, err)
	}
	empty := []byte(`<COLLADA xmlns="http://www.collada.org/2005/11/COLLADASchema" version="1.4.1"/>`)
	refs, err = p.ReadModel3DComposition(t.Context(), bytes.NewReader(empty), int64(len(empty)))
	if err != nil || refs == nil || len(refs) != 0 {
		t.Fatal("known-empty declarations were lost", refs, err)
	}
}
func TestDAECompositionRejectsPartialOrUnsafeXML(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(daeCompositionXML("a.png"), `</COLLADA>`, `<instance_node url="other.dae#node"/></COLLADA>`, 1),
		strings.Replace(daeCompositionXML("a.png"), `</COLLADA>`, `<input source="https://host/vertices"/></COLLADA>`, 1),
		daeCompositionXML("a.png") + "trailing", daeCompositionXML("a.png") + daeCompositionXML("b.png"),
		`<!DOCTYPE COLLADA [<!ENTITY x "a.png">]>` + daeCompositionXML("&x;"),
		daeCompositionXML("<nested>a.png</nested>"), daeCompositionXML(""),
		strings.Replace(daeCompositionXML("a.png"), `<init_from>a.png</init_from>`, `<data>embedded</data>`, 1),
		strings.Replace(daeCompositionXML("a.png"), `</init_from>`, `</init_from><init_from>b.png</init_from>`, 1),
		daeCompositionXML(strings.Repeat("x", 4097)),
		strings.Replace(daeCompositionXML("a.png"), `</library_images>`, strings.Repeat(`<image><init_from>b.png</init_from></image>`, 200)+`</library_images>`, 1),
		strings.Replace(daeCompositionXML("a.png"), `<init_from>`, `<init_from xmlns="urn:wrong">`, 1),
	} {
		refs, err := NewPlugin().ReadModel3DComposition(t.Context(), strings.NewReader(raw), int64(len(raw)))
		if err == nil || refs != nil {
			t.Fatalf("partial or unsafe XML accepted: %q %v %v", raw[:min(len(raw), 150)], refs, err)
		}
	}
	raw := []byte(daeCompositionXML("a.png"))
	for _, size := range []int64{0, -1, MaxSummaryBytes + 1, int64(len(raw) + 5)} {
		r := &observedDAEReader{data: raw}
		refs, err := NewPlugin().ReadModel3DComposition(t.Context(), r, size)
		if err == nil || refs != nil {
			t.Fatal("invalid observed length accepted", size)
		}
		if size <= 0 || size > MaxSummaryBytes {
			if r.calls != 0 {
				t.Fatal("read before size validation")
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &observedDAEReader{data: raw}
	if _, err := NewPlugin().ReadModel3DComposition(ctx, r, int64(len(raw))); err == nil || r.calls != 0 {
		t.Fatal("canceled composition read")
	}
}
