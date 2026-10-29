package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
)

func vffWatermarkTestLimits() vffWatermarkLimits {
	return vffWatermarkLimits{bytes: 1 << 20, records: 1000, depth: 10}
}
func vffWatermarkControl(change func(map[string][]byte, *vffContainer)) *vffContainer {
	c := &vffContainer{members: map[string][]byte{"watermarks/literal%20image.png": vffAuthoredPNG(8, []byte{11, 22, 33, 0, 44, 55, 66, 128}, 2)}}
	p := map[string][]byte{
		"image":       vffRecord("E62E", vffMetaImageValue("watermarks/literal%20image.png")),
		"other-entry": vffRecord("E12E", []byte("https://unread.invalid/history.png")),
		"model-other": vffRecord("0902", vffRecord("E62E", vffMetaImageValue("not-a-reference.png"))),
		"list-other":  vffRecord("EE2C", []byte{0, 0, 0, 0}),
	}
	if change != nil {
		change(p, c)
	}
	entry := vffRecord("E02E", p["image"], p["other-entry"])
	if v, ok := p["entry"]; ok {
		entry = v
	}
	list := vffRecord("ED2C", entry)
	if v, ok := p["list"]; ok {
		list = v
	}
	manager := vffRecord("EC2C", list, p["list-other"])
	if v, ok := p["manager"]; ok {
		manager = v
	}
	group := vffRecord("0302", manager)
	if v, ok := p["group"]; ok {
		group = v
	}
	c.members["model.dat"] = vffRecord("F401", group, p["model-other"])
	return c
}

func TestVFFWatermarkExactReferencesPixelsAndOpaqueHistory(t *testing.T) {
	c := vffWatermarkControl(nil)
	got, err := readVFFWatermarkReferences(context.Background(), c, vffWatermarkTestLimits())
	if err != nil || len(got.images) != 1 || len(got.opaque) != 3 {
		t.Fatalf("facts: %#v %v", got, err)
	}
	if got.modelSHA256 != sha256.Sum256(c.members["model.dat"]) {
		t.Fatal("model identity")
	}
	ref := got.images[0]
	model := c.members["model.dat"]
	if !bytes.Equal(model[ref.entryOffset:ref.entryOffset+2], []byte{0xe0, 0x2e}) || !bytes.Equal(model[ref.offset:ref.offset+2], []byte{0x2a, 0x23}) {
		t.Fatal("entry/reference positions")
	}
	n := int(binary.LittleEndian.Uint32(model[ref.offset+2:]))
	if string(model[ref.offset+6:ref.offset+6+n]) != ref.member || ref.sha256 != sha256.Sum256(c.members[ref.member]) {
		t.Fatal("literal referenced bytes")
	}
	pixels, err := readVFFImages(context.Background(), c, []vffImageReference{ref.vffImageReference}, vffImageTestLimits())
	if err != nil || len(pixels) != 1 || pixels[0].minAlpha != 0 || pixels[0].maxAlpha != 32896 {
		t.Fatalf("pixels: %#v %v", pixels, err)
	}
	for _, o := range got.opaque {
		if o.member != "model.dat" || sha256.Sum256(model[o.offset+6:o.offset+6+o.bytes]) != o.sha256 {
			t.Fatal("opaque provenance")
		}
	}
	// This decoy must not rescue the missing literal member; neither an import
	// string nor another member's basename/decoded spelling is a read plan.
	c.members["watermarks/literal image.png"] = c.members[ref.member]
	delete(c.members, ref.member)
	if facts, err := readVFFWatermarkReferences(context.Background(), c, vffWatermarkTestLimits()); err == nil || facts != nil {
		t.Fatal("missing literal reference repaired")
	}
}

func TestVFFWatermarkExplicitEmptyAndRepeatedImage(t *testing.T) {
	c := vffWatermarkControl(func(p map[string][]byte, _ *vffContainer) { p["list"] = vffRecord("ED2C", nil) })
	if got, err := readVFFWatermarkReferences(context.Background(), c, vffWatermarkTestLimits()); err != nil || len(got.images) != 0 {
		t.Fatalf("empty list: %#v %v", got, err)
	}
	c = vffWatermarkControl(func(p map[string][]byte, _ *vffContainer) {
		entry := vffRecord("E02E", p["image"], p["other-entry"])
		p["entry"] = append(entry, entry...)
	})
	limit := vffWatermarkTestLimits()
	limit.bytes = len(c.members["model.dat"]) + len(c.members["watermarks/literal%20image.png"])
	got, err := readVFFWatermarkReferences(context.Background(), c, limit)
	if err != nil || len(got.images) != 2 || got.images[0].member != got.images[1].member || got.images[0].entryOffset == got.images[1].entryOffset || got.images[0].offset == got.images[1].offset {
		t.Fatalf("distinct occurrences: %#v %v", got, err)
	}
	var refs []vffImageReference
	for _, v := range got.images {
		refs = append(refs, v.vffImageReference)
	}
	if decoded, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err != nil || len(decoded) != 1 {
		t.Fatalf("one actual image: %#v %v", decoded, err)
	}
}

func TestVFFWatermarkRefusalsAreAtomic(t *testing.T) {
	for name, change := range map[string]func(map[string][]byte, *vffContainer){
		"missing-group":     func(p map[string][]byte, _ *vffContainer) { p["group"] = nil },
		"duplicate-group":   func(p map[string][]byte, _ *vffContainer) { g := vffRecord("0302", nil); p["group"] = append(g, g...) },
		"unknown-container": func(p map[string][]byte, _ *vffContainer) { p["manager"] = vffRecord("EC2D", nil) },
		"duplicate-container": func(p map[string][]byte, _ *vffContainer) {
			v := vffRecord("EC2C", nil)
			p["manager"] = append(v, v...)
		},
		"missing-list":       func(p map[string][]byte, _ *vffContainer) { p["list"] = nil },
		"duplicate-list":     func(p map[string][]byte, _ *vffContainer) { v := vffRecord("ED2C", nil); p["list"] = append(v, v...) },
		"unknown-list-field": func(p map[string][]byte, _ *vffContainer) { p["list-other"] = vffRecord("EF2C", nil) },
		"duplicate-list-field": func(p map[string][]byte, _ *vffContainer) {
			p["list-other"] = append(p["list-other"], p["list-other"]...)
		},
		"missing-list-field": func(p map[string][]byte, _ *vffContainer) { p["list-other"] = nil },
		"unknown-entry":      func(p map[string][]byte, _ *vffContainer) { p["entry"] = vffRecord("E12E", p["image"]) },
		"missing-image":      func(p map[string][]byte, _ *vffContainer) { p["image"] = nil },
		"empty-image":        func(p map[string][]byte, _ *vffContainer) { p["image"] = vffRecord("E62E", nil) },
		"duplicate-image":    func(p map[string][]byte, _ *vffContainer) { p["image"] = append(p["image"], p["image"]...) },
		"wrong-value-type": func(p map[string][]byte, _ *vffContainer) {
			p["image"] = vffRecord("E62E", vffRecord("2823", vffRecord("2923", []byte{5, 0, 0, 0}), vffRecord("2A23", []byte("watermarks/literal%20image.png"))))
		},
		"unknown-value": func(p map[string][]byte, _ *vffContainer) {
			p["image"] = vffRecord("E62E", vffRecord("2823", vffRecord("2923", []byte{4, 0, 0, 0}), vffRecord("2B23", []byte("watermarks/literal%20image.png"))))
		},
		"missing-target": func(_ map[string][]byte, c *vffContainer) { delete(c.members, "watermarks/literal%20image.png") },
		"model-alias": func(p map[string][]byte, _ *vffContainer) {
			p["image"] = vffRecord("E62E", vffMetaImageValue("model.dat"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := vffWatermarkControl(change)
			if got, err := readVFFWatermarkReferences(context.Background(), c, vffWatermarkTestLimits()); err == nil || got != nil {
				t.Fatal("partial or ambiguous facts")
			}
		})
	}
	for _, name := range []string{"", "../image.png", "./watermarks/literal%20image.png", "/image.png", "https://unread.invalid/image.png", "watermarks\\image.png", "watermarks//image.png", string([]byte{0xff})} {
		t.Run(name, func(t *testing.T) {
			c := vffWatermarkControl(func(p map[string][]byte, _ *vffContainer) { p["image"] = vffRecord("E62E", vffMetaImageValue(name)) })
			if got, err := readVFFWatermarkReferences(context.Background(), c, vffWatermarkTestLimits()); err == nil || got != nil {
				t.Fatal("noncanonical path")
			}
		})
	}
}

func TestVFFWatermarkBudgetsCancellationAndSourceChange(t *testing.T) {
	c := vffWatermarkControl(nil)
	total := len(c.members["model.dat"]) + len(c.members["watermarks/literal%20image.png"])
	for name, limit := range map[string]vffWatermarkLimits{"unspecified": {}, "model-bytes": {bytes: 1, records: 1000, depth: 10}, "image-bytes": {bytes: total - 1, records: 1000, depth: 10}, "records": {bytes: 1 << 20, records: 8, depth: 10}, "depth": {bytes: 1 << 20, records: 1000, depth: 5}} {
		t.Run(name, func(t *testing.T) {
			if got, err := readVFFWatermarkReferences(context.Background(), c, limit); err == nil || got != nil {
				t.Fatal("budget")
			}
		})
	}
	for _, change := range []func([]byte) []byte{func(b []byte) []byte { return b[:len(b)-1] }, func(b []byte) []byte { return append(b, 1) }, func(b []byte) []byte { return append(b, b...) }} {
		broken := vffWatermarkControl(nil)
		broken.members["model.dat"] = change(broken.members["model.dat"])
		if got, err := readVFFWatermarkReferences(context.Background(), broken, vffWatermarkTestLimits()); err == nil || got != nil {
			t.Fatal("truncated or unconsumed source")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readVFFWatermarkReferences(ctx, c, vffWatermarkTestLimits()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	checks := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 10}
	if got, err := readVFFWatermarkReferences(checks, c, vffWatermarkTestLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("in-flight cancel")
	}
	got, err := readVFFWatermarkReferences(context.Background(), c, vffWatermarkTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	old := got.images[0].vffImageReference
	c.members[old.member] = []byte("not an image")
	if _, err := readVFFImages(context.Background(), c, []vffImageReference{old}, vffImageTestLimits()); err == nil {
		t.Fatal("stale source image fact")
	}
	got, err = readVFFWatermarkReferences(context.Background(), c, vffWatermarkTestLimits())
	if err != nil || got.images[0].sha256 == old.sha256 {
		t.Fatal("current digest")
	}
	if _, err := readVFFImages(context.Background(), c, []vffImageReference{got.images[0].vffImageReference}, vffImageTestLimits()); err == nil {
		t.Fatal("reference fact became image validity")
	}
}
