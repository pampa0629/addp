package scantask

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestInheritedTargetsRemovesIndependentSchedules(t *testing.T) {
	t.Parallel()

	parent := models.JSONMap{
		"namespaces":   []interface{}{"public", "gis"},
		"object_paths": []interface{}{"bucket/a", "bucket/b"},
	}
	independent := []models.JSONMap{
		{"namespaces": []interface{}{"gis"}},
		{"object_paths": []interface{}{"bucket/b"}},
	}

	got := InheritedTargets(parent, independent)
	if !reflect.DeepEqual(got.Namespaces, []string{"public"}) {
		t.Fatalf("namespaces = %#v", got.Namespaces)
	}
	if !reflect.DeepEqual(got.ObjectPaths, []string{"bucket/a"}) {
		t.Fatalf("object paths = %#v", got.ObjectPaths)
	}
}
