package scantask

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestNormalizeStorageType(t *testing.T) {
	t.Parallel()

	if got := NormalizeStorageType("Object Storage"); got != "object_storage" {
		t.Fatalf("storage type = %q, want object_storage", got)
	}
	if got := NormalizeStorageType(" "); got != "unknown" {
		t.Fatalf("empty storage type = %q, want unknown", got)
	}
}

func TestJSONMapStringSlice(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{
		"namespaces": []interface{}{"public", "gis"},
	}
	if got := JSONMapStringSlice(attrs, "namespaces"); !reflect.DeepEqual(got, []string{"public", "gis"}) {
		t.Fatalf("namespaces = %#v", got)
	}
}

func TestStringSliceFromInterfaceSupportsStringSlice(t *testing.T) {
	t.Parallel()

	if got := StringSliceFromInterface([]string{"a", "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("slice = %#v", got)
	}
}
