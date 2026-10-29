package authorizationfacts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestWholeScopeBindingAndStrictWire(t *testing.T) {
	root := plugin.FileDirectoryPath(14, "exports/public_test.gdb")
	f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: 14, ItemType: "file", FullName: root.StringPath(),
		ItemFingerprint: models.GenerateItemFingerprint(14, root.StringPath()), ObservedAt: time.Now(), BasisState: AuthorizationBasisComplete,
		Boundary: &AuthorizationBoundary{Layout: "whole", Scope: &ScopeBoundary{Format: "filegdb", RuleVersion: 1, Root: root}}}
	hash, err := f.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	f.BoundaryHash = &hash
	b, err := f.ProvePrimaryTarget(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip Binding
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.Scope == nil || roundtrip.BoundaryHash != hash || strings.Contains(string(raw), "native_target") {
		t.Fatal(string(raw))
	}
	fraw, _ := json.Marshal(f)
	var decoded AuthorizationFacts
	if err := json.Unmarshal(fraw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, mutated := range []string{
		strings.Replace(string(raw), `"rule_version":1`, `"rule_version":2`, 1),
		strings.Replace(string(raw), `"format":"filegdb"`, `"format":"sqlite"`, 1),
		strings.Replace(string(raw), "public_test.gdb", "other.gdb", 1),
		strings.Replace(string(raw), `"scope":{`, `"scope":{"refs":[],`, 1),
		strings.Replace(string(raw), `"scope":`, `"native_target":{},"scope":`, 1),
	} {
		if json.Unmarshal([]byte(mutated), &roundtrip) == nil {
			t.Fatalf("accepted changed/ambiguous scope: %s", mutated)
		}
	}
	if _, err := f.ProvePrimaryTarget(plugin.FileItemPath(14, root.StringPath()+"/a00000001.gdbtable")); err == nil {
		t.Fatal("physical fragment inherited the Grant")
	}
	if _, err := f.ProveNativeTarget(root); err == nil {
		t.Fatal("whole scope treated as native table")
	}
	for _, path := range []plugin.EngineCatalogPath{plugin.FileDirectoryPath(14, "exports"), plugin.FileDirectoryPath(14, "exports/other"), plugin.FileItemPath(14, root.StringPath())} {
		if ValidWholeScopeRoot(path) {
			t.Fatalf("accepted non-container scope: %#v", path)
		}
	}
}
