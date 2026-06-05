package scanflow

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestInheritedTargetsRemovesIndependentSchedules(t *testing.T) {
	t.Parallel()

	parent := models.JSONMap{
		"type":          "catalog_path",
		"catalog_paths": []interface{}{"public", "gis", "bucket/a", "bucket/b"},
	}
	independent := []models.JSONMap{
		{"type": "catalog_path", "catalog_paths": []interface{}{"bucket/b"}},
	}

	got := InheritedTargets(parent, independent)
	if !reflect.DeepEqual(got.CatalogPaths, []string{"public", "gis", "bucket/a"}) {
		t.Fatalf("catalog paths = %#v", got.CatalogPaths)
	}
}

func TestTargetsFromScopeReadsCatalogPaths(t *testing.T) {
	t.Parallel()

	got := TargetsFromScope(models.JSONMap{
		"type":          "catalog_path",
		"catalog_paths": []interface{}{"catalog/path"},
	})
	if got.ScopeType != "catalog_path" {
		t.Fatalf("scope type = %q", got.ScopeType)
	}
	if !reflect.DeepEqual(got.CatalogPaths, []string{"catalog/path"}) {
		t.Fatalf("catalog paths = %#v", got.CatalogPaths)
	}
}

func TestTargetsFromScopeKeepsEngineScope(t *testing.T) {
	t.Parallel()

	got := TargetsFromScope(models.JSONMap{
		"type":      "engine",
		"engine_id": 7,
	})
	if got.ScopeType != "engine" || len(got.CatalogPaths) != 0 {
		t.Fatalf("engine target = %#v", got)
	}
}
