package authorizationfacts

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestGeoPackageBoundaryIsOneContainerDataItem(t *testing.T) {
	primary := plugin.FileItemPath(14, "sources/ 原名 %.gpkg")
	refs := []FileCompositionRef{{Target: primary, Role: "container", Required: true, Primary: true}}
	b, err := NewFileCompositionBoundary("geopackage", refs)
	if err != nil || b.DataType() != "container" || b.Layout() != "single" || b.PrimaryRole() != "container" {
		t.Fatalf("GeoPackage identity = %#v, error = %v", b, err)
	}
	facts := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: 14, ItemFingerprint: models.GenerateItemFingerprint(14, primary.StringPath()), ItemType: "file", FullName: primary.StringPath(), ObservedAt: time.Now(), BasisState: AuthorizationBasisComplete, Boundary: &AuthorizationBoundary{Layout: "single", FileComposition: b}}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	facts.BoundaryHash = &hash
	binding, err := facts.ApprovedBinding()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Binding
	if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(binding, decoded) {
		t.Fatalf("GeoPackage binding roundtrip: %v", err)
	}
	if target, err := decoded.PrimaryTarget(); err != nil || !reflect.DeepEqual(target, primary) {
		t.Fatal("container did not retain the DataItem target", target, err)
	}
	for name, mutate := range map[string]func([]FileCompositionRef) []FileCompositionRef{
		"model role": func(r []FileCompositionRef) []FileCompositionRef { r[0].Role = "model"; return r },
		"main role":  func(r []FileCompositionRef) []FileCompositionRef { r[0].Role = "main"; return r },
		"optional":   func(r []FileCompositionRef) []FileCompositionRef { r[0].Required = false; return r },
		"no primary": func(r []FileCompositionRef) []FileCompositionRef { r[0].Primary = false; return r },
		"additional WAL": func(r []FileCompositionRef) []FileCompositionRef {
			return append(r, FileCompositionRef{Target: plugin.FileItemPath(14, "sources/ 原名 %.gpkg-wal"), Role: "journal", Required: true})
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewFileCompositionBoundary("geopackage", mutate(append([]FileCompositionRef{}, refs...))); err == nil {
				t.Fatal("non-single GeoPackage boundary accepted")
			}
		})
	}
}
