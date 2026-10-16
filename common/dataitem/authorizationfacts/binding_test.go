package authorizationfacts

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func bindingFacts(t *testing.T, path plugin.EngineCatalogPath) AuthorizationFacts {
	t.Helper()
	fingerprint, err := NativeCandidateFingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: path.EngineID, ItemFingerprint: fingerprint,
		ItemType: path.Segments[2].Term, FullName: path.Segments[1].Name + "." + path.Segments[2].Name,
		BasisState: AuthorizationBasisComplete, ObservedAt: time.Now().UTC(),
		Boundary: &AuthorizationBoundary{Layout: "single", NativeTarget: path}}
	hash, err := f.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	f.BoundaryHash = &hash
	return f
}

func TestApprovedBindingContainsOnlyImmutableCompleteBoundary(t *testing.T) {
	path := plugin.TabularItemPath(2, "schema", "户外. 数据", "表/人物")
	f := bindingFacts(t, path)
	b, err := f.ApprovedBinding()
	if err != nil || b.Validate() != nil || !reflect.DeepEqual(b.NativeTarget, path) {
		t.Fatalf("binding=%+v err=%v", b, err)
	}
	f.ObservedAt = f.ObservedAt.Add(time.Hour)
	later, err := f.ApprovedBinding()
	if err != nil || !reflect.DeepEqual(later, b) {
		t.Fatal("observation time changed approved boundary", err)
	}
	f.Boundary.NativeTarget.Segments[2].Name = "changed"
	if b.NativeTarget.Segments[2].Name != "表/人物" {
		t.Fatal("binding aliases mutable source segments")
	}
	if _, err := f.ApprovedBinding(); err == nil {
		t.Fatal("changed boundary accepted under original hash")
	}
	for _, state := range []AuthorizationBasisState{AuthorizationBasisIncomplete, AuthorizationBasisNeedsRefresh, AuthorizationBasisUnsupported} {
		f := bindingFacts(t, path)
		f.BasisState, f.Boundary, f.BoundaryHash = state, nil, nil
		if _, err := f.ApprovedBinding(); err == nil {
			t.Fatalf("unresolved state %q accepted", state)
		}
	}
}

func TestNativeCandidateNeverProvesOwnership(t *testing.T) {
	first := plugin.TabularItemPath(2, "schema", "a.b", "c")
	other := plugin.TabularItemPath(2, "schema", "a", "b.c")
	a, _ := NativeCandidateFingerprint(first)
	b, _ := NativeCandidateFingerprint(other)
	if a != b {
		t.Fatal("fixture must exercise the dotted-name identity collision")
	}
	f := bindingFacts(t, first)
	if _, err := f.ProveNativeTarget(first); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ProveNativeTarget(other); err == nil {
		t.Fatal("candidate fingerprint silently proved a different typed target")
	}
	for _, path := range []plugin.EngineCatalogPath{{}, plugin.TabularItemPath(0, "schema", "a", "b"), plugin.TabularItemPath(2, "schema", "a", "\x00b"), plugin.TabularItemPath(2, "bucket", "a", "b")} {
		if _, err := NativeCandidateFingerprint(path); err == nil {
			t.Fatalf("unsupported native target accepted: %+v", path)
		}
	}
}

func TestSingleRecordBoundariesKeepNamespaceAndLeafTypes(t *testing.T) {
	paths := []plugin.EngineCatalogPath{
		plugin.TabularItemPath(2, "schema", "a.b", " c "),
		plugin.TabularItemPath(2, "database", "a.b", " c "),
		plugin.EngineCatalogBranchLeafPath(plugin.DynamicSchemaCatalogModel(), 2, "database", "a.b", "collection", "collection", " c "),
	}
	hashes := map[string]bool{}
	for _, path := range paths {
		f := bindingFacts(t, path)
		b, err := f.ProveNativeTarget(path)
		if err != nil || b.Validate() != nil || hashes[b.BoundaryHash] {
			t.Fatalf("typed boundary collapsed: %+v %v", b, err)
		}
		hashes[b.BoundaryHash] = true
		encoded, err := json.Marshal(f)
		var decoded AuthorizationFacts
		if err != nil || json.Unmarshal(encoded, &decoded) != nil || decoded.Validate() != nil {
			t.Fatal("supported typed boundary failed strict wire round trip", err)
		}
		for _, other := range paths {
			if !reflect.DeepEqual(other, path) {
				if _, err := f.ProveNativeTarget(other); err == nil {
					t.Fatal("same dotted name granted another namespace or leaf type")
				}
			}
		}
		other := path
		other.EngineID++
		if _, err := f.ProveNativeTarget(other); err == nil {
			t.Fatal("cross-engine path accepted")
		}
		malformed := path
		malformed.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
		malformed.Segments[2].Kind = "view"
		if _, err := NativeCandidateFingerprint(malformed); err == nil {
			t.Fatal("mismatched leaf kind accepted")
		}
	}
	invalid := paths[2]
	invalid.Segments = append([]plugin.EngineCatalogSegment(nil), invalid.Segments...)
	invalid.Segments[1].Term = "schema"
	if _, err := NativeCandidateFingerprint(invalid); err == nil {
		t.Fatal("collection under schema accepted")
	}
}
