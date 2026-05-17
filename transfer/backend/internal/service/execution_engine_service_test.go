package service

import (
	"reflect"
	"testing"

	"github.com/addp/transfer/internal/planner"
)

func TestTargetCatalogPathsUsesObjectParentPrefix(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Resource: planner.ResourceSpec{
			Kind: "object",
			Path: map[string]interface{}{"bucket": "addp", "path": "gis/abc.shp"},
		},
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"addp/gis"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesBucketForTopLevelObject(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Resource: planner.ResourceSpec{
			Kind: "object",
			Path: map[string]interface{}{"bucket": "addp", "path": "abc.csv"},
		},
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"addp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesFileParentDirectory(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Resource: planner.ResourceSpec{
			Kind: "file",
			Path: map[string]interface{}{"path": "exports/abc.shp"},
		},
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"exports"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesFilesystemRootForTopLevelFile(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Resource: planner.ResourceSpec{
			Kind: "file",
			Path: map[string]interface{}{"path": "abc.csv"},
		},
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesNativeTableNamespace(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Resource: planner.ResourceSpec{
			Kind: "native_table",
			Path: map[string]interface{}{"schema": "public", "table": "roads"},
		},
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"public"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}
