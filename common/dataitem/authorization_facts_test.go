package dataitem

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func authorizationTestFacts(t *testing.T) AuthorizationFacts {
	t.Helper()
	f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: 2, ItemType: "table", FullName: "Out.door. active.table ", BasisState: AuthorizationBasisComplete, ObservedAt: time.Now().UTC(),
		Boundary: &AuthorizationBoundary{Layout: "single", NativeTarget: plugin.TabularItemPath(2, "schema", "Out.door", " active.table ")}}
	f.ItemFingerprint = models.GenerateItemFingerprint(f.EngineID, f.FullName)
	hash, err := f.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	f.BoundaryHash = &hash
	return f
}

func TestAuthorizationFactsBoundaryAndIdentity(t *testing.T) {
	f := authorizationTestFacts(t)
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	before := *f.BoundaryHash
	f.ObservedAt = f.ObservedAt.Add(time.Hour)
	after, err := f.ComputeBoundaryHash()
	if err != nil || before != after {
		t.Fatal("observation time changed boundary", err)
	}
	f.Boundary.NativeTarget.Segments[2].Name = "another"
	if f.Validate() == nil {
		t.Fatal("different table reused approved identity")
	}
	f = authorizationTestFacts(t)
	f.EngineID++
	if f.Validate() == nil {
		t.Fatal("different engine reused identity")
	}
}

func TestAuthorizationFactsStrictWire(t *testing.T) {
	valid, err := json.Marshal(authorizationTestFacts(t))
	if err != nil {
		t.Fatal(err)
	}
	var decoded AuthorizationFacts
	if err := json.Unmarshal(valid, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{
		strings.Replace(string(valid), `"layout":"single"`, `"layout":"single","layout":"single"`, 1),
		strings.Replace(string(valid), `"layout":"single"`, `"Layout":"single"`, 1),
		strings.Replace(string(valid), `"layout":"single"`, `"layout":"single","credential":"secret"`, 1),
		strings.Replace(string(valid), `"schema_version":"`+AuthorizationFactsSchemaV1+`"`, `"schema_version":"unsupported"`, 1),
		strings.Replace(string(valid), `"engine_id":"2"`, `"engine_id":"02"`, 1),
		string(valid) + `{}`,
	} {
		if json.Unmarshal([]byte(encoded), &decoded) == nil {
			t.Fatalf("accepted malformed facts: %s", encoded)
		}
	}
	for _, state := range []AuthorizationBasisState{AuthorizationBasisIncomplete, AuthorizationBasisNeedsRefresh, AuthorizationBasisUnsupported} {
		f := authorizationTestFacts(t)
		f.BasisState, f.Boundary, f.BoundaryHash = state, nil, nil
		if f.Validate() != nil {
			t.Fatal("unresolved state rejected")
		}
		encoded, _ := json.Marshal(f)
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
	}
}
