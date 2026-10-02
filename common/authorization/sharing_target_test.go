package authorization

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestSharingTargetExactIdentityAndBounds(t *testing.T) {
	path := plugin.TabularItemPath(math.MaxInt64, "schema", " 业务.域/一 ", " 表/名.甲 ")
	encoded, err := EncodeSharingTarget(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := json.Marshal(path)
	if err != nil || !bytes.Equal(encoded, original) {
		t.Fatalf("encoding changed structural identity: %s %v", encoded, err)
	}
	var decoded plugin.EngineCatalogPath
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.EngineID != path.EngineID ||
		decoded.Segments[1].Name != path.Segments[1].Name || decoded.Segments[2].Name != path.Segments[2].Name {
		t.Fatalf("round trip lost exact identity: %+v %v", decoded, err)
	}
	path.Segments = append(path.Segments[:1:1], append(make([]plugin.EngineCatalogSegment, 62), path.Segments[2])...)
	for i := 1; i < len(path.Segments)-1; i++ {
		path.Segments[i] = plugin.EngineCatalogSegment{Term: "directory", Kind: "directory", Name: "node"}
	}
	if _, err := EncodeSharingTarget(path); err != nil {
		t.Fatalf("64-segment boundary rejected: %v", err)
	}
	path.Segments = append(path.Segments[:len(path.Segments)-1], plugin.EngineCatalogSegment{Term: "directory", Kind: "directory", Name: "extra"}, path.Segments[len(path.Segments)-1])
	if _, err := EncodeSharingTarget(path); !errors.Is(err, ErrInvalidSharingTarget) {
		t.Fatalf("65 segments accepted: %v", err)
	}

	path = plugin.TabularItemPath(12, "schema", "public", "x")
	base, _ := json.Marshal(path)
	path.Segments[2].Name = strings.Repeat("x", 16*1024-len(base)+1)
	boundary, _ := json.Marshal(path)
	if len(boundary) != 16*1024 {
		t.Fatalf("bad boundary fixture: %d", len(boundary))
	}
	if _, err := EncodeSharingTarget(path); err != nil {
		t.Fatalf("16 KiB boundary rejected: %v", err)
	}
	path.Segments[2].Name += "x"
	if _, err := EncodeSharingTarget(path); !errors.Is(err, ErrInvalidSharingTarget) {
		t.Fatalf("oversized encoded target accepted: %v", err)
	}
}

func TestSharingTargetRejectsInvalidOrLossyIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*plugin.EngineCatalogPath){
		"zero engine":           func(p *plugin.EngineCatalogPath) { p.EngineID = 0 },
		"overflow engine":       func(p *plugin.EngineCatalogPath) { p.EngineID = math.MaxInt64 + 1 },
		"unknown version":       func(p *plugin.EngineCatalogPath) { p.Version = "unknown" },
		"no structural root":    func(p *plugin.EngineCatalogPath) { p.Segments = p.Segments[1:] },
		"non-leaf":              func(p *plugin.EngineCatalogPath) { p.Segments = p.Segments[:2] },
		"empty name":            func(p *plugin.EngineCatalogPath) { p.Segments[2].Name = " " },
		"lossy name":            func(p *plugin.EngineCatalogPath) { p.Segments[2].Name = "invalid\xff" },
		"lossy kind":            func(p *plugin.EngineCatalogPath) { p.Segments[2].Kind = "invalid\xff" },
		"lossy term":            func(p *plugin.EngineCatalogPath) { p.Segments[2].Term = "invalid\xff" },
		"encoded utf8 bytes":    func(p *plugin.EngineCatalogPath) { p.Segments[2].Name = strings.Repeat("甲", 6000) },
		"encoded escaped bytes": func(p *plugin.EngineCatalogPath) { p.Segments[2].Name = strings.Repeat("<", 3000) },
	} {
		t.Run(name, func(t *testing.T) {
			path := plugin.TabularItemPath(12, "schema", "public", "orders")
			mutate(&path)
			if _, err := EncodeSharingTarget(path); !errors.Is(err, ErrInvalidSharingTarget) {
				t.Fatalf("invalid target accepted: %v", err)
			}
		})
	}
}
