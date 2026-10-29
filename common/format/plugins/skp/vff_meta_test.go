package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
)

func vffMetaTestLimits() vffMetaLimits { return vffMetaLimits{bytes: 1 << 20, records: 1000, depth: 8} }
func vffMetaImageValue(name string) []byte {
	return vffRecord("2823", vffRecord("2923", []byte{4, 0, 0, 0}), vffRecord("2A23", []byte(name)))
}
func vffMetaControl(change func(map[string][]byte, *vffContainer)) *vffContainer {
	c := &vffContainer{members: map[string][]byte{}}
	p := map[string][]byte{
		"entry":        vffRecord("0A02", []byte("custom/meta.dat")),
		"model-opaque": vffRecord("0902", vffMetaImageValue("not-a-reference.png")),
		"front":        vffRecord("6700", vffMetaImageValue("images/model%20view.png")),
		"preview":      vffRecord("6800", vffMetaImageValue("images/preview.png")),
		"meta-opaque":  vffRecord("7800", vffMetaImageValue("also-not-a-reference.png")),
	}
	c.members["images/model%20view.png"] = vffAuthoredPNG(8, []byte{1, 2, 3, 0}, 1)
	c.members["images/preview.png"] = vffAuthoredPNG(8, []byte{4, 5, 6, 255}, 1)
	if change != nil {
		change(p, c)
	}
	c.members["model.dat"] = vffRecord("F401", p["entry"], p["model-opaque"])
	c.members["custom/meta.dat"] = vffRecord("6400", p["front"], p["preview"], p["meta-opaque"])
	return c
}

func TestVFFMetaReferencesOffsetsImagesAndUnexplainedRanges(t *testing.T) {
	c := vffMetaControl(nil)
	got, err := readVFFMetaReferences(context.Background(), c, vffMetaTestLimits())
	if err != nil || got.member != "custom/meta.dat" || len(got.images) != 2 || len(got.opaque) != 2 {
		t.Fatalf("facts: %#v %v", got, err)
	}
	if got.modelSHA256 != sha256.Sum256(c.members["model.dat"]) || got.metaSHA256 != sha256.Sum256(c.members[got.member]) {
		t.Fatal("input identity")
	}
	var refs []vffImageReference
	for i, v := range got.images {
		if v.field != []string{"6700", "6800"}[i] || v.sha256 != sha256.Sum256(c.members[v.member]) {
			t.Fatal("image identity/field")
		}
		meta := c.members[got.member]
		n := int(binary.LittleEndian.Uint32(meta[v.offset+2:]))
		if !bytes.Equal(meta[v.offset:v.offset+2], []byte{0x2a, 0x23}) || string(meta[v.offset+6:v.offset+6+n]) != v.member {
			t.Fatal("exact reference position")
		}
		refs = append(refs, v.vffImageReference)
	}
	if decoded, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err != nil || len(decoded) != 2 {
		t.Fatalf("actual images: %#v %v", decoded, err)
	}
	for _, v := range got.opaque {
		b := c.members[v.member]
		if sha256.Sum256(b[v.offset+6:v.offset+6+v.bytes]) != v.sha256 {
			t.Fatal("opaque source position")
		}
	}
	if got.opaque[0].member != "model.dat" || got.opaque[1].member != got.member {
		t.Fatal("opaque member provenance")
	}
	// Literal %20 must remain literal, even with a same-directory decoded decoy.
	c.members["images/model view.png"] = c.members[refs[0].member]
	delete(c.members, refs[0].member)
	if partial, err := readVFFMetaReferences(context.Background(), c, vffMetaTestLimits()); err == nil || partial != nil {
		t.Fatal("missing literal member repaired")
	}
}

func TestVFFMetaEmptyAndSharedImage(t *testing.T) {
	c := vffMetaControl(func(p map[string][]byte, _ *vffContainer) {
		p["front"] = vffRecord("6700", nil)
		p["preview"] = vffRecord("6800", nil)
	})
	if got, err := readVFFMetaReferences(context.Background(), c, vffMetaTestLimits()); err != nil || len(got.images) != 0 {
		t.Fatalf("explicit empty: %#v %v", got, err)
	}
	c = vffMetaControl(func(p map[string][]byte, _ *vffContainer) {
		p["preview"] = vffRecord("6800", vffMetaImageValue("images/model%20view.png"))
	})
	limit := vffMetaTestLimits()
	limit.bytes = len(c.members["model.dat"]) + len(c.members["custom/meta.dat"]) + len(c.members["images/model%20view.png"])
	if got, err := readVFFMetaReferences(context.Background(), c, limit); err != nil || len(got.images) != 2 || got.images[0].member != got.images[1].member {
		t.Fatalf("shared image: %#v %v", got, err)
	}
	// Reference facts cannot stand in for actual image validation.
	c.members["images/model%20view.png"] = []byte("not an image")
	got, err := readVFFMetaReferences(context.Background(), c, vffMetaTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = readVFFImages(context.Background(), c, []vffImageReference{got.images[0].vffImageReference}, vffImageTestLimits()); err == nil {
		t.Fatal("reference became body proof")
	}
}

func TestVFFMetaRejectsAmbiguousOrUnexplainedReferenceShapes(t *testing.T) {
	for name, change := range map[string]func(map[string][]byte, *vffContainer){
		"missing-entry":   func(p map[string][]byte, _ *vffContainer) { p["entry"] = nil },
		"duplicate-entry": func(p map[string][]byte, _ *vffContainer) { p["entry"] = append(p["entry"], p["entry"]...) },
		"absolute-entry":  func(p map[string][]byte, _ *vffContainer) { p["entry"] = vffRecord("0A02", []byte("/custom/meta.dat")) },
		"missing-field":   func(p map[string][]byte, _ *vffContainer) { p["preview"] = nil },
		"duplicate-field": func(p map[string][]byte, _ *vffContainer) { p["front"] = append(p["front"], p["front"]...) },
		"wrong-wrapper":   func(p map[string][]byte, _ *vffContainer) { p["front"] = vffRecord("6700", vffRecord("2824", nil)) },
		"two-wrappers": func(p map[string][]byte, _ *vffContainer) {
			v := vffMetaImageValue("images/model%20view.png")
			p["front"] = vffRecord("6700", v, v)
		},
		"unknown-value": func(p map[string][]byte, _ *vffContainer) {
			p["front"] = vffRecord("6700", vffRecord("2823", vffRecord("2923", []byte{4, 0, 0, 0}), vffRecord("2A23", []byte("images/model%20view.png")), vffRecord("2B23", nil)))
		},
		"duplicate-value": func(p map[string][]byte, _ *vffContainer) {
			p["front"] = vffRecord("6700", vffRecord("2823", vffRecord("2923", []byte{4, 0, 0, 0}), vffRecord("2A23", []byte("images/preview.png")), vffRecord("2A23", []byte("images/model%20view.png"))))
		},
		"wrong-type": func(p map[string][]byte, _ *vffContainer) {
			p["front"] = vffRecord("6700", vffRecord("2823", vffRecord("2923", []byte{5, 0, 0, 0}), vffRecord("2A23", []byte("images/preview.png"))))
		},
		"wrong-type-size": func(p map[string][]byte, _ *vffContainer) {
			p["front"] = vffRecord("6700", vffRecord("2823", vffRecord("2923", []byte{4}), vffRecord("2A23", []byte("images/preview.png"))))
		},
		"missing-type": func(p map[string][]byte, _ *vffContainer) {
			p["front"] = vffRecord("6700", vffRecord("2823", vffRecord("2A23", []byte("images/preview.png"))))
		},
		"missing-path": func(p map[string][]byte, _ *vffContainer) {
			p["front"] = vffRecord("6700", vffRecord("2823", vffRecord("2923", []byte{4, 0, 0, 0})))
		},
		"missing-target": func(_ map[string][]byte, c *vffContainer) { delete(c.members, "images/preview.png") },
		"binary-alias": func(p map[string][]byte, _ *vffContainer) {
			p["front"] = vffRecord("6700", vffMetaImageValue("custom/meta.dat"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := vffMetaControl(change)
			if got, err := readVFFMetaReferences(context.Background(), c, vffMetaTestLimits()); err == nil || got != nil {
				t.Fatal("invalid partial facts")
			}
		})
	}
	for _, path := range []string{"", "../images/preview.png", "./images/preview.png", "/images/preview.png", "https://example.invalid/a.png", "images\\preview.png", "images//preview.png", "images/a\x00.png", string([]byte{0xff})} {
		t.Run(path, func(t *testing.T) {
			c := vffMetaControl(func(p map[string][]byte, _ *vffContainer) { p["front"] = vffRecord("6700", vffMetaImageValue(path)) })
			if got, err := readVFFMetaReferences(context.Background(), c, vffMetaTestLimits()); err == nil || got != nil {
				t.Fatal("noncanonical reference")
			}
		})
	}
}

func TestVFFMetaBudgetFramingSourceChangesAndCancellation(t *testing.T) {
	for _, member := range []string{"model.dat", "custom/meta.dat"} {
		for _, mode := range []string{"tail", "truncated", "two-roots", "wrong-root", "missing"} {
			t.Run(member+mode, func(t *testing.T) {
				c := vffMetaControl(nil)
				raw := c.members[member]
				switch mode {
				case "tail":
					c.members[member] = append(raw, 1)
				case "truncated":
					c.members[member] = raw[:len(raw)-1]
				case "two-roots":
					c.members[member] = append(raw, raw...)
				case "wrong-root":
					raw[0] ^= 1
				case "missing":
					delete(c.members, member)
				}
				if got, err := readVFFMetaReferences(context.Background(), c, vffMetaTestLimits()); err == nil || got != nil {
					t.Fatal("bad binary source")
				}
			})
		}
	}
	c := vffMetaControl(nil)
	total := len(c.members["model.dat"]) + len(c.members["custom/meta.dat"]) + len(c.members["images/model%20view.png"]) + len(c.members["images/preview.png"])
	for name, limit := range map[string]vffMetaLimits{
		"unspecified": {}, "model-bytes": {bytes: 1, records: 1000, depth: 8},
		"meta-bytes":  {bytes: len(c.members["model.dat"]), records: 1000, depth: 8},
		"image-bytes": {bytes: total - 1, records: 1000, depth: 8},
		"records":     {bytes: 1 << 20, records: 8, depth: 8}, "depth": {bytes: 1 << 20, records: 1000, depth: 2},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := readVFFMetaReferences(context.Background(), c, limit); err == nil || got != nil {
				t.Fatal("budget")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readVFFMetaReferences(ctx, c, vffMetaTestLimits()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	checks := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 8}
	if got, err := readVFFMetaReferences(checks, c, vffMetaTestLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("in-flight cancel ignored")
	}
	before, err := readVFFMetaReferences(context.Background(), c, vffMetaTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	c.members["images/preview.png"] = []byte("changed")
	after, err := readVFFMetaReferences(context.Background(), c, vffMetaTestLimits())
	if err != nil || before.images[1].sha256 == after.images[1].sha256 {
		t.Fatal("current image identity")
	}
	if _, err := readVFFImages(context.Background(), c, []vffImageReference{before.images[1].vffImageReference}, vffImageTestLimits()); err == nil {
		t.Fatal("old image fact reused")
	}
}
