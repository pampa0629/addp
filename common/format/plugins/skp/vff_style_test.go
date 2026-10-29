package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
)

func vffStyleTestLimits() vffStyleLimits {
	return vffStyleLimits{bytes: 1 << 20, records: 1000, depth: 10}
}
func vffStyleEntryData(id byte, name string, token []byte) []byte {
	var entity []byte
	if id != 0 {
		entity = vffRecord("DC05", vffRecord("DE05", []byte{id}))
	}
	return vffRecord("6C6B", entity, vffRecord("6D6B", token), vffRecord("6F6B", []byte(name)), vffRecord("6E6B", []byte("https://unread.invalid/import.style")))
}
func vffStyleControl(change func(map[string][]byte, *vffContainer)) *vffContainer {
	token := bytes.Repeat([]byte{11}, 16)
	c := &vffContainer{members: map[string][]byte{"styles/Saved%20Style/style.xml": []byte("opaque style XML semantics"), "styles/Working/style.xml": []byte("opaque style XML semantics")}}
	p := map[string][]byte{"catalog": vffStyleEntryData(1, "Saved%20Style", token), "copy": vffStyleEntryData(0, "Working", token), "reference": vffRecord("7A69", []byte{1}), "state": vffRecord("7C69", []byte{0}), "unknown": vffRecord("0902", vffStyleEntryData(90, "not-a-catalog-entry", token))}
	if change != nil {
		change(p, c)
	}
	list := vffRecord("7969", p["catalog"])
	if x, ok := p["list"]; ok {
		list = x
	}
	independent := vffRecord("7B69", p["copy"])
	if x, ok := p["independent"]; ok {
		independent = x
	}
	fields := vffRecord("7869", list, p["reference"], independent, p["state"])
	if x, ok := p["fields"]; ok {
		fields = x
	}
	group := vffRecord("0602", fields)
	if x, ok := p["group"]; ok {
		group = x
	}
	c.members["model.dat"] = vffRecord("F401", p["unknown"], group)
	return c
}

func TestVFFStyleCatalogReferencesAndDistinctIdentities(t *testing.T) {
	c := vffStyleControl(nil)
	got, err := readVFFStyleCatalog(context.Background(), c, vffStyleTestLimits())
	if err != nil || len(got.entries) != 2 || got.reference != 1 || got.state != 0 || len(got.opaque) != 3 {
		t.Fatalf("facts: %#v %v", got, err)
	}
	if got.modelSHA256 != sha256.Sum256(c.members["model.dat"]) {
		t.Fatal("source identity")
	}
	model := c.members["model.dat"]
	if !bytes.Equal(model[got.referenceOffset:got.referenceOffset+2], []byte{0x7a, 0x69}) || model[got.referenceOffset+6] != 1 {
		t.Fatal("reference position")
	}
	for i, e := range got.entries {
		if e.branch != []string{"7969", "7B69"}[i] || e.id != []uint64{1, 0}[i] || e.xmlSHA256 != sha256.Sum256(c.members[e.member]) || !bytes.Equal(model[e.offset:e.offset+2], []byte{0x6c, 0x6b}) {
			t.Fatal("entry exact identity")
		}
	}
	if got.entries[0].token != got.entries[1].token || got.entries[0].xmlSHA256 != got.entries[1].xmlSHA256 || got.entries[0].member == got.entries[1].member || got.entries[0].offset == got.entries[1].offset {
		t.Fatal("equal contents merged independent entries")
	}
	for _, o := range got.opaque {
		if o.member != "model.dat" || sha256.Sum256(model[o.offset+6:o.offset+6+o.bytes]) != o.sha256 {
			t.Fatal("unproved source ranges")
		}
	}
	// The matching XML byte hash is deliberately not an XML semantics proof.
	if string(c.members[got.entries[0].member]) != "opaque style XML semantics" {
		t.Fatal("control")
	}
	c.members["styles/Saved Style/style.xml"] = c.members[got.entries[0].member]
	delete(c.members, got.entries[0].member)
	if got, err := readVFFStyleCatalog(context.Background(), c, vffStyleTestLimits()); err == nil || got != nil {
		t.Fatal("literal %20 folder repaired")
	}
}

func TestVFFStyleMultipleCatalogEntriesAndRawState(t *testing.T) {
	c := vffStyleControl(func(p map[string][]byte, c *vffContainer) {
		p["catalog"] = append(p["catalog"], vffStyleEntryData(2, "Other", bytes.Repeat([]byte{11}, 16))...)
		p["reference"] = vffRecord("7A69", []byte{2})
		p["state"] = vffRecord("7C69", []byte{1})
		c.members["styles/Other/style.xml"] = []byte("other")
	})
	got, err := readVFFStyleCatalog(context.Background(), c, vffStyleTestLimits())
	if err != nil || len(got.entries) != 3 || got.reference != 2 || got.state != 1 {
		t.Fatalf("multiple style IDs: %#v %v", got, err)
	}
}

func TestVFFStyleCatalogRefusalsAreAtomic(t *testing.T) {
	token := bytes.Repeat([]byte{11}, 16)
	for name, change := range map[string]func(map[string][]byte, *vffContainer){
		"missing-group":   func(p map[string][]byte, _ *vffContainer) { p["group"] = nil },
		"duplicate-group": func(p map[string][]byte, _ *vffContainer) { v := vffRecord("0602", nil); p["group"] = append(v, v...) },
		"wrong-wrapper":   func(p map[string][]byte, _ *vffContainer) { p["fields"] = vffRecord("7868", nil) },
		"missing-list":    func(p map[string][]byte, _ *vffContainer) { p["list"] = nil },
		"empty-list":      func(p map[string][]byte, _ *vffContainer) { p["catalog"] = nil },
		"wrong-entry":     func(p map[string][]byte, _ *vffContainer) { p["catalog"] = vffRecord("6C6C", nil) },
		"duplicate-id": func(p map[string][]byte, c *vffContainer) {
			p["catalog"] = append(p["catalog"], vffStyleEntryData(1, "Other", token)...)
			c.members["styles/Other/style.xml"] = []byte("other")
		},
		"ambiguous-member":     func(p map[string][]byte, _ *vffContainer) { p["copy"] = vffStyleEntryData(0, "Saved%20Style", token) },
		"dangling-reference":   func(p map[string][]byte, _ *vffContainer) { p["reference"] = vffRecord("7A69", []byte{99}) },
		"zero-reference":       func(p map[string][]byte, _ *vffContainer) { p["reference"] = vffRecord("7A69", []byte{0}) },
		"long-reference":       func(p map[string][]byte, _ *vffContainer) { p["reference"] = vffRecord("7A69", make([]byte, 9)) },
		"missing-reference":    func(p map[string][]byte, _ *vffContainer) { p["reference"] = nil },
		"duplicate-reference":  func(p map[string][]byte, _ *vffContainer) { p["reference"] = append(p["reference"], p["reference"]...) },
		"missing-independent":  func(p map[string][]byte, _ *vffContainer) { p["independent"] = nil },
		"multiple-independent": func(p map[string][]byte, _ *vffContainer) { p["copy"] = append(p["copy"], p["copy"]...) },
		"independent-id":       func(p map[string][]byte, _ *vffContainer) { p["copy"] = vffStyleEntryData(2, "Working", token) },
		"catalog-missing-id": func(p map[string][]byte, _ *vffContainer) {
			p["catalog"] = vffStyleEntryData(0, "Saved%20Style", token)
		},
		"duplicate-id-field": func(p map[string][]byte, _ *vffContainer) {
			p["catalog"] = vffRecord("6C6B", vffEntity(1), vffEntity(2), vffRecord("6D6B", token), vffRecord("6F6B", []byte("Saved%20Style")))
		},
		"duplicate-id-value": func(p map[string][]byte, _ *vffContainer) {
			p["catalog"] = vffRecord("6C6B", vffRecord("DC05", vffRecord("DE05", []byte{1}), vffRecord("DE05", []byte{2})), vffRecord("6D6B", token), vffRecord("6F6B", []byte("Saved%20Style")))
		},
		"duplicate-name": func(p map[string][]byte, _ *vffContainer) {
			p["copy"] = vffRecord("6C6B", vffRecord("6D6B", token), vffRecord("6F6B", []byte("Working")), vffRecord("6F6B", []byte("Other")))
		},
		"bad-token":    func(p map[string][]byte, _ *vffContainer) { p["copy"] = vffStyleEntryData(0, "Working", token[:15]) },
		"missing-name": func(p map[string][]byte, _ *vffContainer) { p["copy"] = vffRecord("6C6B", vffRecord("6D6B", token)) },
		"missing-token": func(p map[string][]byte, _ *vffContainer) {
			p["copy"] = vffRecord("6C6B", vffRecord("6F6B", []byte("Working")))
		},
		"unknown-state":         func(p map[string][]byte, _ *vffContainer) { p["state"] = vffRecord("7C69", []byte{2}) },
		"long-state":            func(p map[string][]byte, _ *vffContainer) { p["state"] = vffRecord("7C69", []byte{0, 0}) },
		"missing-state":         func(p map[string][]byte, _ *vffContainer) { p["state"] = nil },
		"unknown-catalog-field": func(p map[string][]byte, _ *vffContainer) { p["state"] = vffRecord("7D69", []byte{0}) },
		"missing-xml":           func(_ map[string][]byte, c *vffContainer) { delete(c.members, "styles/Working/style.xml") },
	} {
		t.Run(name, func(t *testing.T) {
			c := vffStyleControl(change)
			if got, err := readVFFStyleCatalog(context.Background(), c, vffStyleTestLimits()); err == nil || got != nil {
				t.Fatal("invalid partial facts")
			}
		})
	}
	for _, name := range []string{"", "..", "../outside", "a/b", "a\\b", "/absolute", "http:thing", string([]byte{0xff}), "nul\x00"} {
		t.Run(name, func(t *testing.T) {
			c := vffStyleControl(func(p map[string][]byte, _ *vffContainer) { p["copy"] = vffStyleEntryData(0, name, token) })
			if got, err := readVFFStyleCatalog(context.Background(), c, vffStyleTestLimits()); err == nil || got != nil {
				t.Fatal("bad folder")
			}
		})
	}
}

func TestVFFStyleBudgetsFramingCancellationAndCurrentBytes(t *testing.T) {
	c := vffStyleControl(nil)
	total := len(c.members["model.dat"]) + len(c.members["styles/Saved%20Style/style.xml"]) + len(c.members["styles/Working/style.xml"])
	for name, limit := range map[string]vffStyleLimits{"unspecified": {}, "model-bytes": {bytes: 1, records: 1000, depth: 10}, "xml-bytes": {bytes: total - 1, records: 1000, depth: 10}, "records": {bytes: 1 << 20, records: 8, depth: 10}, "depth": {bytes: 1 << 20, records: 1000, depth: 3}} {
		t.Run(name, func(t *testing.T) {
			if got, err := readVFFStyleCatalog(context.Background(), c, limit); err == nil || got != nil {
				t.Fatal("budget")
			}
		})
	}
	for _, change := range []func([]byte) []byte{func(b []byte) []byte { return b[:len(b)-1] }, func(b []byte) []byte { return append(b, 1) }, func(b []byte) []byte { return append(b, b...) }} {
		broken := vffStyleControl(nil)
		broken.members["model.dat"] = change(broken.members["model.dat"])
		if got, err := readVFFStyleCatalog(context.Background(), broken, vffStyleTestLimits()); err == nil || got != nil {
			t.Fatal("unconsumed framing")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readVFFStyleCatalog(ctx, c, vffStyleTestLimits()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	checks := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 8}
	if got, err := readVFFStyleCatalog(checks, c, vffStyleTestLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("in-flight cancellation")
	}
	before, err := readVFFStyleCatalog(context.Background(), c, vffStyleTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	c.members["styles/Working/style.xml"] = []byte("changed XML semantics remain unproved")
	after, err := readVFFStyleCatalog(context.Background(), c, vffStyleTestLimits())
	if err != nil || before.entries[1].xmlSHA256 == after.entries[1].xmlSHA256 {
		t.Fatal("current XML bytes")
	}
}
