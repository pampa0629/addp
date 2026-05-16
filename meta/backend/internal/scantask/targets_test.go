package scantask

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestInheritedTargetsRemovesIndependentSchedules(t *testing.T) {
	t.Parallel()

	parent := models.JSONMap{
		"catalog_paths": []interface{}{"public", "gis", "bucket/a", "bucket/b"},
	}
	independent := []models.JSONMap{
		{"catalog_paths": []interface{}{"bucket/b"}},
	}

	got := InheritedTargets(parent, independent)
	if !reflect.DeepEqual(got.CatalogPaths, []string{"public", "gis", "bucket/a"}) {
		t.Fatalf("catalog paths = %#v", got.CatalogPaths)
	}
}

func TestTargetsFromParametersReadsCatalogPaths(t *testing.T) {
	t.Parallel()

	got := TargetsFromParameters(models.JSONMap{
		"catalog_paths": []interface{}{"catalog/path"},
	})
	if !reflect.DeepEqual(got.CatalogPaths, []string{"catalog/path"}) {
		t.Fatalf("catalog paths = %#v", got.CatalogPaths)
	}
}
