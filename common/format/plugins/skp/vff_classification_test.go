package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
)

func vffClassificationTestLimits() vffClassificationLimits {
	return vffClassificationLimits{bytes: 1 << 20, records: 100, depth: 6}
}
func vffClassificationControl(change func(map[string][]byte, *vffContainer)) *vffContainer {
	c := &vffContainer{members: map[string][]byte{"classifications/literal%20schema.skc": []byte("nested SKC bytes are not interpreted by this stage")}}
	p := map[string][]byte{
		"path":      vffRecord("8767", []byte("classifications/literal%20schema.skc")),
		"directory": vffRecord("8567", []byte("https://unread.invalid/import")),
		"name":      vffRecord("8667", []byte("different display name")),
		"other":     vffRecord("0902", vffRecord("8767", []byte("not-a-reference.skc"))),
	}
	if change != nil {
		change(p, c)
	}
	entry := vffRecord("8467", p["directory"], p["name"], p["path"], p["extra"])
	if v, ok := p["entries"]; ok {
		entry = v
	}
	group := vffRecord("0502", entry)
	if v, ok := p["group"]; ok {
		group = v
	}
	c.members["model.dat"] = vffRecord("F401", group, p["other"])
	return c
}

func TestVFFClassificationExactReferencesAndOpaqueFacts(t *testing.T) {
	c := vffClassificationControl(nil)
	model := bytes.Clone(c.members["model.dat"])
	got, err := readVFFClassificationReferences(context.Background(), c, vffClassificationTestLimits())
	if err != nil || len(got.entries) != 1 || len(got.opaque) != 3 {
		t.Fatalf("facts %#v: %v", got, err)
	}
	ref := got.entries[0]
	if got.modelSHA256 != sha256.Sum256(model) || ref.member != "classifications/literal%20schema.skc" || ref.sha256 != sha256.Sum256(c.members[ref.member]) {
		t.Fatal("exact source identity")
	}
	if !bytes.Equal(model[ref.entryOffset:ref.entryOffset+2], []byte{0x84, 0x67}) || !bytes.Equal(model[ref.pathOffset:ref.pathOffset+2], []byte{0x87, 0x67}) {
		t.Fatal("source record positions")
	}
	n := int(binary.LittleEndian.Uint32(model[ref.pathOffset+2:]))
	if string(model[ref.pathOffset+6:ref.pathOffset+6+n]) != ref.member {
		t.Fatal("literal member path")
	}
	for _, o := range got.opaque {
		if o.member != "model.dat" || sha256.Sum256(model[o.offset+6:o.offset+6+o.bytes]) != o.sha256 {
			t.Fatal("opaque provenance")
		}
	}
	if !bytes.Equal(model, c.members["model.dat"]) {
		t.Fatal("source modified")
	}
	// No display-name, historical location or URI-decoded fallback.
	c.members["classifications/literal schema.skc"] = c.members[ref.member]
	c.members["classifications/different display name.skc"] = c.members[ref.member]
	delete(c.members, ref.member)
	if v, err := readVFFClassificationReferences(context.Background(), c, vffClassificationTestLimits()); err == nil || v != nil {
		t.Fatal("missing exact member rescued by decoy")
	}
}

func TestVFFClassificationEmptyListAndSharedMemberBudget(t *testing.T) {
	c := vffClassificationControl(func(p map[string][]byte, _ *vffContainer) { p["entries"] = nil })
	got, err := readVFFClassificationReferences(context.Background(), c, vffClassificationTestLimits())
	if err != nil || len(got.entries) != 0 || len(got.opaque) != 1 {
		t.Fatalf("explicit empty list: %#v %v", got, err)
	}
	c = vffClassificationControl(func(p map[string][]byte, _ *vffContainer) {
		entry := vffRecord("8467", p["directory"], p["name"], p["path"])
		p["entries"] = append(bytes.Clone(entry), entry...)
	})
	limits := vffClassificationTestLimits()
	limits.bytes = len(c.members["model.dat"]) + len(c.members["classifications/literal%20schema.skc"])
	got, err = readVFFClassificationReferences(context.Background(), c, limits)
	if err != nil || len(got.entries) != 2 || got.entries[0].entryOffset == got.entries[1].entryOffset || got.entries[0].pathOffset == got.entries[1].pathOffset || got.entries[0].sha256 != got.entries[1].sha256 {
		t.Fatalf("shared member: %#v %v", got, err)
	}
	limits.bytes--
	if v, err := readVFFClassificationReferences(context.Background(), c, limits); err == nil || v != nil {
		t.Fatal("target not charged")
	}
	// Same bytes under a different member retain distinct identity and cost.
	c = vffClassificationControl(func(p map[string][]byte, c *vffContainer) {
		c.members["classifications/second.skc"] = bytes.Clone(c.members["classifications/literal%20schema.skc"])
		p["entries"] = vffRecord("8467", p["directory"], p["name"], p["path"])
		p["entries"] = append(p["entries"], vffRecord("8467", p["directory"], p["name"], vffRecord("8767", []byte("classifications/second.skc")))...)
	})
	limits.bytes = len(c.members["model.dat"]) + len(c.members["classifications/literal%20schema.skc"])
	if v, err := readVFFClassificationReferences(context.Background(), c, limits); err == nil || v != nil {
		t.Fatal("distinct equal-content member not charged")
	}
	limits.bytes += len(c.members["classifications/second.skc"])
	got, err = readVFFClassificationReferences(context.Background(), c, limits)
	if err != nil || len(got.entries) != 2 || got.entries[0].member == got.entries[1].member {
		t.Fatalf("distinct identities: %#v %v", got, err)
	}
}

func TestVFFClassificationRejectsIncompleteAndUnexplained(t *testing.T) {
	cases := map[string]func(map[string][]byte, *vffContainer){
		"missing list": func(p map[string][]byte, _ *vffContainer) { p["group"] = nil },
		"duplicate list": func(p map[string][]byte, _ *vffContainer) {
			p["group"] = vffRecord("0502", nil)
			p["group"] = append(p["group"], p["group"]...)
		},
		"wrong entry":         func(p map[string][]byte, _ *vffContainer) { p["entries"] = vffRecord("8567", nil) },
		"empty entry":         func(p map[string][]byte, _ *vffContainer) { p["entries"] = vffRecord("8467", nil) },
		"missing path":        func(p map[string][]byte, _ *vffContainer) { p["path"] = nil },
		"missing name":        func(p map[string][]byte, _ *vffContainer) { p["name"] = nil },
		"missing directory":   func(p map[string][]byte, _ *vffContainer) { p["directory"] = nil },
		"duplicate path":      func(p map[string][]byte, _ *vffContainer) { p["path"] = append(p["path"], p["path"]...) },
		"duplicate name":      func(p map[string][]byte, _ *vffContainer) { p["name"] = append(p["name"], p["name"]...) },
		"duplicate directory": func(p map[string][]byte, _ *vffContainer) { p["directory"] = append(p["directory"], p["directory"]...) },
		"unknown field":       func(p map[string][]byte, _ *vffContainer) { p["extra"] = vffRecord("8867", nil) },
		"truncated field":     func(p map[string][]byte, _ *vffContainer) { p["extra"] = []byte{1} },
		"missing target":      func(_ map[string][]byte, c *vffContainer) { delete(c.members, "classifications/literal%20schema.skc") },
		"empty target":        func(_ map[string][]byte, c *vffContainer) { c.members["classifications/literal%20schema.skc"] = nil },
	}
	for _, path := range []string{"", "model.dat", "../escape.skc", "/absolute.skc", "C:/absolute.skc", "https://invalid/schema.skc", "classifications/./schema.skc", "classifications//schema.skc", "classifications\\schema.skc", "a\x00.skc", string([]byte{0xff})} {
		path := path
		cases["path "+path] = func(p map[string][]byte, _ *vffContainer) { p["path"] = vffRecord("8767", []byte(path)) }
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := readVFFClassificationReferences(context.Background(), vffClassificationControl(mutate), vffClassificationTestLimits())
			if err == nil || got != nil {
				t.Fatalf("accepted invalid declaration: %#v %v", got, err)
			}
		})
	}
}

func TestVFFClassificationLimitsAndCancellation(t *testing.T) {
	c := vffClassificationControl(nil)
	for _, limits := range []vffClassificationLimits{{}, {1, 100, 6}, {1 << 20, 1, 6}, {1 << 20, 100, 2}} {
		if got, err := readVFFClassificationReferences(context.Background(), c, limits); err == nil || got != nil {
			t.Fatalf("invalid limits accepted %#v", limits)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readVFFClassificationReferences(ctx, c, vffClassificationTestLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("cancellation: %#v %v", got, err)
	}
	if got, err := readVFFClassificationReferences(context.Background(), nil, vffClassificationTestLimits()); err == nil || got != nil {
		t.Fatal("nil container accepted")
	}
}
