package scanresolver

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func TestResolveScopeRefGroups(t *testing.T) {
	t.Parallel()

	scope, err := New(nil).ResolveScope(3, scanflow.Options{
		EngineID:     7,
		CatalogPaths: []string{" bucket/path "},
		RefGroups: []models.ScanRefGroup{
			{
				Primary: " bucket/path/roads.shp ",
				Refs: []models.ScanRef{
					{Path: " bucket/path/roads.shp ", Role: " main ", Required: true},
					{Path: " ", Role: "sidecar", Required: true},
				},
			},
		},
		ScanDepth: "DEEP",
		Force:     true,
		Source:    " transfer ",
	})
	if err != nil {
		t.Fatalf("ResolveScope() error = %v", err)
	}

	if scope.EngineID != 7 || scope.Mode != scanflow.ModeRefGroups || scope.ScanDepth != "deep" || !scope.Force || scope.Source != "transfer" {
		t.Fatalf("scope scalar fields = %#v", scope)
	}
	if !reflect.DeepEqual(scope.CatalogPaths, []string{"bucket/path"}) {
		t.Fatalf("catalog paths = %#v", scope.CatalogPaths)
	}
	if len(scope.RefGroups) != 1 || scope.RefGroups[0].Primary != "bucket/path/roads.shp" {
		t.Fatalf("ref groups = %#v", scope.RefGroups)
	}
	if got := scope.RefGroups[0].Refs; len(got) != 1 || got[0].Path != "bucket/path/roads.shp" || got[0].Role != "main" || !got[0].Required {
		t.Fatalf("refs = %#v", got)
	}
}

func TestResolveScopeDefaultsToEngineMode(t *testing.T) {
	t.Parallel()

	scope, err := New(nil).ResolveScope(3, scanflow.Options{EngineID: 7})
	if err != nil {
		t.Fatalf("ResolveScope() error = %v", err)
	}
	if scope.Mode != scanflow.ModeEngine || scope.ScanDepth != "basic" || scope.Source != "meta" {
		t.Fatalf("scope defaults = %#v", scope)
	}
}
