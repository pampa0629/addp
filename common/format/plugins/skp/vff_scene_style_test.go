package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
)

func vffSceneStyleTestLimits() vffSceneStyleLimits {
	return vffSceneStyleLimits{bytes: 1 << 20, records: 1000, depth: 12}
}
func vffSceneStyleControl(change func(map[string][]byte, *vffContainer)) (*vffContainer, *vffStyleFacts) {
	c := &vffContainer{members: map[string][]byte{"styles/Default/style.xml": []byte("default XML identity only"), "styles/Scene/style.xml": []byte("distinct scene XML identity only")}}
	p := map[string][]byte{"id": vffEntity(10), "name": vffRecord("556F", []byte("Scene / 名称")), "flags": vffRecord("4971", []byte{2, 0, 0, 0}), "reference": vffRecord("4C71", []byte{2}), "render": vffRecord("4D71", vffRecord("3C73", vffRecord("3D73", []byte{1}))), "root-opaque": vffRecord("0902", vffRecord("4C71", []byte{99})), "head-opaque": vffRecord("566F", []byte("unproved description"))}
	if change != nil {
		change(p, c)
	}
	head := vffRecord("546F", p["id"], p["name"], p["head-opaque"])
	if x, ok := p["head"]; ok {
		head = x
	}
	scene := vffRecord("4871", head, p["flags"], p["reference"], p["render"], p["thumbnail"])
	if x, ok := p["scene"]; ok {
		scene = x
	}
	list := vffRecord("616D", scene)
	if x, ok := p["list"]; ok {
		list = x
	}
	group := vffRecord("0702", vffRecord("606D", list, vffRecord("626D", []byte{10})))
	if x, ok := p["group"]; ok {
		group = x
	}
	c.members["model.dat"] = vffRecord("F401", p["root-opaque"], group)
	catalog := &vffStyleFacts{modelSHA256: sha256.Sum256(c.members["model.dat"]), reference: 1, entries: []vffStyleEntry{{branch: "7969", id: 1, folder: "Default", member: "styles/Default/style.xml", xmlSHA256: sha256.Sum256(c.members["styles/Default/style.xml"])}, {branch: "7969", id: 2, folder: "Scene", member: "styles/Scene/style.xml", xmlSHA256: sha256.Sum256(c.members["styles/Scene/style.xml"])}}}
	return c, catalog
}

func TestVFFSceneStyleCandidateJoinsExactIDWithoutDefaultInheritance(t *testing.T) {
	c, catalog := vffSceneStyleControl(nil)
	got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits())
	if err != nil || len(got.scenes) != 1 {
		t.Fatalf("facts: %#v %v", got, err)
	}
	s := got.scenes[0]
	model := c.members["model.dat"]
	if s.id != 10 || s.name != "Scene / 名称" || s.flags != 2 || s.candidateID != 2 || s.candidateMember != "styles/Scene/style.xml" || s.candidateXMLSHA256 != sha256.Sum256(c.members[s.candidateMember]) {
		t.Fatalf("synthetic candidate join: %#v", s)
	}
	if !bytes.Equal(model[s.offset:s.offset+2], []byte{0x48, 0x71}) || !bytes.Equal(model[s.candidateOffset:s.candidateOffset+2], []byte{0x4c, 0x71}) || model[s.candidateOffset+6] != 2 {
		t.Fatal("exact source positions")
	}
	for _, o := range got.opaque {
		if o.member != "model.dat" || sha256.Sum256(model[o.offset+6:o.offset+6+o.bytes]) != o.sha256 {
			t.Fatal("opaque field identity")
		}
	}
	// Even with a set raw bit or identical XML bytes, an absent candidate is
	// absent, never model-current inheritance or a proved resource-free scene.
	for _, flags := range []byte{0, 1, 2, 255} {
		c, catalog := vffSceneStyleControl(func(p map[string][]byte, c *vffContainer) {
			p["reference"] = nil
			p["flags"] = vffRecord("4971", []byte{flags, 0, 0, 0})
			c.members["styles/Scene/style.xml"] = c.members["styles/Default/style.xml"]
		})
		got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits())
		if err != nil || len(got.scenes) != 1 || got.scenes[0].flags != uint32(flags) || got.scenes[0].candidateID != 0 || got.scenes[0].candidateMember != "" {
			t.Fatalf("fabricated inheritance: %#v %v", got, err)
		}
	}
}

func TestVFFSceneStyleExplicitEmptyAndNoCamera(t *testing.T) {
	c, catalog := vffSceneStyleControl(func(p map[string][]byte, _ *vffContainer) { p["list"] = vffRecord("616D", nil) })
	if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err != nil || len(got.scenes) != 0 {
		t.Fatalf("explicit empty: %#v %v", got, err)
	}
	c, catalog = vffSceneStyleControl(func(p map[string][]byte, _ *vffContainer) { p["reference"] = nil; p["render"] = nil })
	if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err != nil || len(got.scenes) != 1 {
		t.Fatalf("no camera is still a record: %#v %v", got, err)
	}
}

func TestVFFSceneStylesKeepPerSceneLinksAndChargeEachTargetOnce(t *testing.T) {
	c, catalog := vffSceneStyleControl(func(p map[string][]byte, _ *vffContainer) {
		var list []byte
		for i, target := range []byte{2, 1, 2} {
			list = append(list, vffRecord("4871", vffRecord("546F", vffEntity(byte(10+i)), p["name"]), p["flags"], vffRecord("4C71", []byte{target}))...)
		}
		p["list"] = vffRecord("616D", list)
	})
	limits := vffSceneStyleTestLimits()
	limits.bytes = len(c.members["model.dat"]) + len(c.members["styles/Default/style.xml"]) + len(c.members["styles/Scene/style.xml"])
	got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, limits)
	if err != nil || len(got.scenes) != 3 {
		t.Fatalf("shared target budget: %#v %v", got, err)
	}
	for i, target := range []uint64{2, 1, 2} {
		if got.scenes[i].id != uint64(10+i) || got.scenes[i].candidateID != target {
			t.Fatal("per-scene reference replaced with a common style")
		}
	}
	limits.bytes--
	if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, limits); err == nil || got != nil {
		t.Fatal("distinct target bytes must all fit before any facts are returned")
	}
}

func TestVFFSceneStyleRefusalsAreAtomic(t *testing.T) {
	for name, change := range map[string]func(map[string][]byte, *vffContainer){
		"missing-group":   func(p map[string][]byte, _ *vffContainer) { p["group"] = nil },
		"duplicate-group": func(p map[string][]byte, _ *vffContainer) { v := vffRecord("0702", nil); p["group"] = append(v, v...) },
		"unknown-wrapper": func(p map[string][]byte, _ *vffContainer) { p["group"] = vffRecord("0702", vffRecord("606E", nil)) },
		"missing-list": func(p map[string][]byte, _ *vffContainer) {
			p["group"] = vffRecord("0702", vffRecord("606D", vffRecord("626D", []byte{1})))
		},
		"unknown-entry": func(p map[string][]byte, _ *vffContainer) { p["scene"] = vffRecord("4971", nil) },
		"duplicate-scene-id": func(p map[string][]byte, _ *vffContainer) {
			s := vffRecord("4871", vffRecord("546F", p["id"], p["name"]), p["flags"], p["reference"])
			p["scene"] = append(s, s...)
		},
		"duplicate-head": func(p map[string][]byte, _ *vffContainer) {
			h := vffRecord("546F", p["id"], p["name"])
			p["head"] = append(h, h...)
		},
		"duplicate-id": func(p map[string][]byte, _ *vffContainer) { p["id"] = append(p["id"], p["id"]...) },
		"duplicate-id-value": func(p map[string][]byte, _ *vffContainer) {
			p["id"] = vffRecord("DC05", vffRecord("DE05", []byte{10}), vffRecord("DE05", []byte{11}))
		},
		"missing-id":          func(p map[string][]byte, _ *vffContainer) { p["id"] = nil },
		"missing-name":        func(p map[string][]byte, _ *vffContainer) { p["name"] = nil },
		"bad-name":            func(p map[string][]byte, _ *vffContainer) { p["name"] = vffRecord("556F", []byte{255}) },
		"missing-flags":       func(p map[string][]byte, _ *vffContainer) { p["flags"] = nil },
		"flag-width":          func(p map[string][]byte, _ *vffContainer) { p["flags"] = vffRecord("4971", []byte{1}) },
		"duplicate-flags":     func(p map[string][]byte, _ *vffContainer) { p["flags"] = append(p["flags"], p["flags"]...) },
		"dangling-candidate":  func(p map[string][]byte, _ *vffContainer) { p["reference"] = vffRecord("4C71", []byte{99}) },
		"zero-candidate":      func(p map[string][]byte, _ *vffContainer) { p["reference"] = vffRecord("4C71", []byte{0}) },
		"long-candidate":      func(p map[string][]byte, _ *vffContainer) { p["reference"] = vffRecord("4C71", make([]byte, 9)) },
		"duplicate-candidate": func(p map[string][]byte, _ *vffContainer) { p["reference"] = append(p["reference"], p["reference"]...) },
		"missing-XML":         func(_ map[string][]byte, c *vffContainer) { delete(c.members, "styles/Scene/style.xml") },
	} {
		t.Run(name, func(t *testing.T) {
			c, catalog := vffSceneStyleControl(change)
			if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err == nil || got != nil {
				t.Fatal("invalid partial facts")
			}
		})
	}
	c, catalog := vffSceneStyleControl(nil)
	catalog.entries = append(catalog.entries, catalog.entries[1])
	if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err == nil || got != nil {
		t.Fatal("duplicate directory IDs")
	}
	c, catalog = vffSceneStyleControl(nil)
	catalog.entries[1].branch = "7B69"
	if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err == nil || got != nil {
		t.Fatal("joined independent working record")
	}
}

func TestVFFSceneStyleBudgetsFramingCancellationAndSourceVersion(t *testing.T) {
	c, catalog := vffSceneStyleControl(nil)
	for name, limit := range map[string]vffSceneStyleLimits{"unspecified": {}, "model-bytes": {bytes: 1, records: 1000, depth: 12}, "XML-bytes": {bytes: len(c.members["model.dat"]), records: 1000, depth: 12}, "records": {bytes: 1 << 20, records: 8, depth: 12}, "depth": {bytes: 1 << 20, records: 1000, depth: 3}} {
		t.Run(name, func(t *testing.T) {
			if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, limit); err == nil || got != nil {
				t.Fatal("budget")
			}
		})
	}
	for _, change := range []func([]byte) []byte{func(b []byte) []byte { return b[:len(b)-1] }, func(b []byte) []byte { return append(b, 1) }, func(b []byte) []byte { return append(b, b...) }} {
		broken, facts := vffSceneStyleControl(nil)
		broken.members["model.dat"] = change(broken.members["model.dat"])
		facts.modelSHA256 = sha256.Sum256(broken.members["model.dat"])
		if got, err := readVFFSceneStyleCandidates(context.Background(), broken, facts, vffSceneStyleTestLimits()); err == nil || got != nil {
			t.Fatal("bad framing")
		}
	}
	c.members["model.dat"][0] ^= 1
	if _, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err == nil {
		t.Fatal("stale model")
	}
	c, catalog = vffSceneStyleControl(nil)
	c.members["styles/Scene/style.xml"] = []byte("changed")
	if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err == nil || got != nil {
		t.Fatal("stale XML")
	}
	c, catalog = vffSceneStyleControl(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readVFFSceneStyleCandidates(ctx, c, catalog, vffSceneStyleTestLimits()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	checks := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 8}
	if got, err := readVFFSceneStyleCandidates(checks, c, catalog, vffSceneStyleTestLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("in-flight cancel")
	}
	// Keep reference width semantics shared with the existing ID reader.
	var id [8]byte
	binary.LittleEndian.PutUint64(id[:], 2)
	c, catalog = vffSceneStyleControl(func(p map[string][]byte, _ *vffContainer) { p["reference"] = vffRecord("4C71", id[:]) })
	if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err != nil || got.scenes[0].candidateID != 2 {
		t.Fatalf("bounded ID: %#v %v", got, err)
	}
}
