package authorizationfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func modelFileRefs() []FileCompositionRef {
	return []FileCompositionRef{
		{Target: plugin.FileItemPath(33, "models/scene.3DS"), Role: "model", Required: true, Primary: true},
		{Target: plugin.FileItemPath(33, "models/TEST.PNG"), Role: "texture", Required: true},
	}
}

func fileCompositionFacts(t *testing.T) AuthorizationFacts {
	t.Helper()
	composition, err := NewFileCompositionBoundary("3ds", modelFileRefs())
	if err != nil {
		t.Fatal(err)
	}
	f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: 33, ItemType: "file",
		FullName: "models/scene.3DS", BasisState: AuthorizationBasisComplete, ObservedAt: time.Now().UTC(),
		Boundary: &AuthorizationBoundary{Layout: "single", FileComposition: composition}}
	f.ItemFingerprint = models.GenerateItemFingerprint(f.EngineID, f.FullName)
	hash, err := f.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	f.BoundaryHash = &hash
	return f
}

func TestFileCompositionCanonicalSetAndIsolation(t *testing.T) {
	refs := modelFileRefs()
	refs = append(refs, refs[0])
	composition, err := NewFileCompositionBoundary("3ds", refs)
	if err != nil || composition.Validate(33) != nil || len(composition.Refs) != 2 {
		t.Fatalf("composition=%+v error=%v", composition, err)
	}
	reordered, err := NewFileCompositionBoundary("3ds", []FileCompositionRef{refs[1], refs[0]})
	if err != nil || !reflect.DeepEqual(composition, reordered) {
		t.Fatal("canonical composition depends on enumeration order", err)
	}
	primary, err := composition.PrimaryTarget()
	if err != nil || !reflect.DeepEqual(primary, plugin.FileItemPath(33, "models/scene.3DS")) {
		t.Fatal("wrong primary", err)
	}
	refs[0].Target.Segments[2].Name = "changed"
	primary.Segments[2].Name = "changed"
	clone := composition.Clone()
	clone.Refs[0].Target.Segments[2].Name = "changed"
	targets, err := composition.Targets()
	if err != nil || len(targets) != 2 {
		t.Fatal("exact targets missing", err)
	}
	targets[0].Segments[2].Name = "changed"
	if composition.Validate(33) != nil || reflect.DeepEqual(composition, clone) {
		t.Fatal("mutable input/output aliases canonical scope")
	}
}

func TestFileCompositionRejectsAmbiguousOrWiderBoundaries(t *testing.T) {
	cases := map[string]func([]FileCompositionRef) []FileCompositionRef{
		"no primary":       func(r []FileCompositionRef) []FileCompositionRef { r[0].Primary = false; return r },
		"two primary":      func(r []FileCompositionRef) []FileCompositionRef { r[1].Primary = true; return r },
		"optional primary": func(r []FileCompositionRef) []FileCompositionRef { r[0].Required = false; return r },
		"other engine":     func(r []FileCompositionRef) []FileCompositionRef { r[1].Target.EngineID++; return r },
		"directory": func(r []FileCompositionRef) []FileCompositionRef {
			r[1].Target = plugin.FileDirectoryPath(33, "models")
			return r
		},
		"sibling prefix": func(r []FileCompositionRef) []FileCompositionRef {
			r[1].Target = plugin.FileItemPath(33, "models-other/TEST.PNG")
			return r
		},
		"outside": func(r []FileCompositionRef) []FileCompositionRef {
			r[1].Target = plugin.FileItemPath(33, "elsewhere/TEST.PNG")
			return r
		},
		"traversal": func(r []FileCompositionRef) []FileCompositionRef {
			r[1].Target = plugin.FileItemPath(33, "models/../TEST.PNG")
			return r
		},
		"dot": func(r []FileCompositionRef) []FileCompositionRef {
			r[1].Target.Segments = append(r[1].Target.Segments[:2],
				plugin.EngineCatalogSegment{Term: "directory", Kind: "directory", Name: "."},
				plugin.EngineCatalogSegment{Term: "file", Kind: "file", Name: "TEST.PNG"})
			return r
		},
		"noncanonical root": func(r []FileCompositionRef) []FileCompositionRef { r[1].Target.Segments[0].Name = "/"; return r },
		"separator": func(r []FileCompositionRef) []FileCompositionRef {
			r[1].Target.Segments[1].Name = "models/elsewhere"
			return r
		},
		"nul": func(r []FileCompositionRef) []FileCompositionRef {
			r[1].Target.Segments[2].Name = "a\x00.png"
			return r
		},
		"uri": func(r []FileCompositionRef) []FileCompositionRef {
			r[1].Target = plugin.FileItemPath(33, "https://host/TEST.PNG")
			return r
		},
		"mixed model": func(r []FileCompositionRef) []FileCompositionRef {
			r[1].Target = plugin.ObjectItemPath(33, "models", "TEST.PNG")
			return r
		},
		"conflicting duplicate": func(r []FileCompositionRef) []FileCompositionRef {
			duplicate := r[1]
			duplicate.Required = false
			return append(r, duplicate)
		},
		"empty role": func(r []FileCompositionRef) []FileCompositionRef { r[1].Role = ""; return r },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewFileCompositionBoundary("3ds", change(modelFileRefs())); err == nil {
				t.Fatal("invalid composition accepted")
			}
		})
	}
	if _, err := NewFileCompositionBoundary("3ds", nil); err == nil {
		t.Fatal("unknown empty set accepted")
	}
	if _, err := NewFileCompositionBoundary("3ds", make([]FileCompositionRef, 201)); err == nil {
		t.Fatal("unbounded file set accepted")
	}
	if _, err := NewFileCompositionBoundary("3DS", modelFileRefs()); err == nil {
		t.Fatal("noncanonical format accepted")
	}
}

func TestFileCompositionObjectPathsKeepBucketAndCase(t *testing.T) {
	refs := []FileCompositionRef{
		{Target: plugin.ObjectItemPath(8, "Models", "folder/模型.3ds"), Role: "model", Required: true, Primary: true},
		{Target: plugin.ObjectItemPath(8, "Models", "folder/textures/颜色.PNG"), Role: "texture", Required: true},
	}
	composition, err := NewFileCompositionBoundary("3ds", refs)
	if err != nil || composition.Validate(8) != nil {
		t.Fatal(err)
	}
	refs[1].Target = plugin.ObjectItemPath(8, "models", "folder/textures/颜色.PNG")
	if _, err := NewFileCompositionBoundary("3ds", refs); err == nil {
		t.Fatal("another bucket accepted")
	}
}

func TestFileCompositionPreservesExactNativeNames(t *testing.T) {
	refs := modelFileRefs()
	refs[0].Target.Segments[1].Name = " 模型目录 "
	refs[0].Target.Segments[2].Name = " 主模型 100%.3DS "
	refs[1].Target.Segments[1].Name = " 模型目录 "
	refs[1].Target.Segments[2].Name = " 颜色.PNG "
	composition, err := NewFileCompositionBoundary("3ds", refs)
	if err != nil {
		t.Fatal(err)
	}
	primary, err := composition.PrimaryTarget()
	if err != nil || primary.StringPath() != " 模型目录 / 主模型 100%.3DS " {
		t.Fatal("native spelling was trimmed or lowercased", err)
	}
}

func TestFileFactsHashAndIdentityBindCompositionNotContent(t *testing.T) {
	f := fileCompositionFacts(t)
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	original := *f.BoundaryHash
	f.ObservedAt = f.ObservedAt.Add(time.Hour)
	if hash, err := f.ComputeBoundaryHash(); err != nil || hash != original {
		t.Fatal("observation time changed composition", err)
	}
	for _, change := range []func(*FileCompositionBoundary){
		func(c *FileCompositionBoundary) { c.Refs = c.Refs[1:] },
		func(c *FileCompositionBoundary) { c.Format = "dae" },
		func(c *FileCompositionBoundary) { c.Refs[0].Role = "texture_other" },
		func(c *FileCompositionBoundary) { c.Refs[0].Required = false },
		func(c *FileCompositionBoundary) { c.Refs[0].Target.Segments[2].Name = "test.png" },
	} {
		changed := fileCompositionFacts(t)
		change(changed.Boundary.FileComposition)
		if changed.Validate() == nil {
			t.Fatal("changed composition reused the approved hash")
		}
	}
	for _, change := range []func(*AuthorizationFacts){
		func(f *AuthorizationFacts) { f.EngineID++ },
		func(f *AuthorizationFacts) { f.ItemType = "object" },
		func(f *AuthorizationFacts) {
			f.FullName = "models/other.3DS"
			f.ItemFingerprint = models.GenerateItemFingerprint(f.EngineID, f.FullName)
		},
		func(f *AuthorizationFacts) { f.Boundary.NativeTarget = plugin.TabularItemPath(33, "schema", "x", "y") },
	} {
		changed := fileCompositionFacts(t)
		change(&changed)
		if _, err := changed.ComputeBoundaryHash(); err == nil {
			t.Fatal("file boundary accepted an incompatible identity or second target")
		}
	}
}

func TestFileFactsStrictWireAndNoNativeFallback(t *testing.T) {
	f := fileCompositionFacts(t)
	encoded, err := json.Marshal(f)
	if err != nil || strings.Contains(string(encoded), "native_target") {
		t.Fatal("file facts exposed a second native target", err)
	}
	var decoded AuthorizationFacts
	if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded.Boundary, f.Boundary) {
		t.Fatal("file facts roundtrip failed", err)
	}
	for _, invalid := range []string{
		strings.Replace(string(encoded), `"required":true`, `"required":null`, 1),
		strings.Replace(string(encoded), `,"primary":false`, ``, 1),
		strings.Replace(string(encoded), `"required":true,`, ``, 1),
		strings.Replace(string(encoded), `"format":"3ds"`, `"format":"3ds","format":"3ds"`, 1),
		strings.Replace(string(encoded), `"format":"3ds"`, `"format":"3ds","allow":true`, 1),
		strings.Replace(string(encoded), `"layout":"single"`, `"layout":"single","native_target":null`, 1),
		strings.Replace(string(encoded), `"role":"texture"`, `"role":"texture","size":23700`, 1),
	} {
		if json.Unmarshal([]byte(invalid), &decoded) == nil {
			t.Fatal("malformed or ambiguous file facts accepted")
		}
	}
	// A file composition is not a single native target or an independently
	// approved texture. The native proof cannot silently return its primary.
	if binding, err := f.ApprovedBinding(); err != nil || binding.FileComposition == nil {
		t.Fatal("approved binding lost complete file composition", err)
	}
	if _, err := f.ProveNativeTarget(plugin.FileItemPath(33, "models/scene.3DS")); err == nil {
		t.Fatal("native proof dropped related files")
	}
	compositionWire, err := json.Marshal(f.Boundary.FileComposition)
	if err != nil {
		t.Fatal(err)
	}
	var composition FileCompositionBoundary
	if json.Unmarshal(compositionWire, &composition) != nil {
		t.Fatal("standalone file composition roundtrip failed")
	}
	for _, invalid := range []string{
		strings.Replace(string(compositionWire), `"format":"3ds"`, `"format":"3ds","allow":true`, 1),
		strings.Replace(string(compositionWire), `"required":true`, `"required":true,"required":true`, 1),
		strings.Replace(string(compositionWire), `,"primary":false`, ``, 1),
	} {
		if json.Unmarshal([]byte(invalid), &composition) == nil {
			t.Fatal("standalone composition accepted malformed wire")
		}
	}
}

func TestNativeFactsCanonicalHashUnaffectedByFileUnion(t *testing.T) {
	f := authorizationTestFacts(t)
	oldCanonical := struct {
		SchemaVersion   string `json:"schema_version"`
		ItemFingerprint string `json:"item_fingerprint"`
		EngineID        uint   `json:"engine_id,string"`
		ItemType        string `json:"item_type"`
		FullName        string `json:"full_name"`
		Boundary        struct {
			Layout       string                   `json:"layout"`
			NativeTarget plugin.EngineCatalogPath `json:"native_target"`
		} `json:"boundary"`
	}{SchemaVersion: f.SchemaVersion, ItemFingerprint: f.ItemFingerprint, EngineID: f.EngineID, ItemType: f.ItemType, FullName: f.FullName}
	oldCanonical.Boundary.Layout, oldCanonical.Boundary.NativeTarget = "single", f.Boundary.NativeTarget
	encoded, err := json.Marshal(oldCanonical)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded)
	if hex.EncodeToString(sum[:]) != *f.BoundaryHash {
		t.Fatal("adding file boundaries changed existing native Grant hashes")
	}
}
