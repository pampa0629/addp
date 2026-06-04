package service

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestResolveScanScopeRefGroups(t *testing.T) {
	t.Parallel()

	scanSvc := &ScanService{}
	scope, err := scanSvc.ResolveScanScope(3, ScanOptions{
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
		t.Fatalf("ResolveScanScope() error = %v", err)
	}

	if scope.EngineID != 7 || scope.Mode != ScanScopeModeRefGroups || scope.ScanDepth != "deep" || !scope.Force || scope.Source != "transfer" {
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

func TestResolveScanScopeDefaultsToEngineMode(t *testing.T) {
	t.Parallel()

	scanSvc := &ScanService{}
	scope, err := scanSvc.ResolveScanScope(3, ScanOptions{EngineID: 7})
	if err != nil {
		t.Fatalf("ResolveScanScope() error = %v", err)
	}
	if scope.Mode != ScanScopeModeEngine || scope.ScanDepth != "basic" || scope.Source != "meta_frontend" {
		t.Fatalf("scope defaults = %#v", scope)
	}
}
