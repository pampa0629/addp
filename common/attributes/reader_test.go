package attributes

import "testing"

func TestStringPrefersSectionThenFlat(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"physical_path": "legacy.parquet",
		"storage": map[string]interface{}{
			"physical_path": "standard.parquet",
		},
	}

	if got := String(attrs, "storage", "physical_path"); got != "standard.parquet" {
		t.Fatalf("String() = %q, want standard.parquet", got)
	}
}

func TestInt64AndFloat64Slice(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"object_count": float64(1),
		"extent":       []interface{}{1, "2.5", int64(3), float32(4)},
		"storage": map[string]interface{}{
			"object_count": "7",
		},
	}

	if got := Int64(attrs, "storage", "object_count"); got != 7 {
		t.Fatalf("Int64() = %d, want 7", got)
	}
	extent := Float64Slice(attrs, "extensions.spatial", "extent")
	if len(extent) != 4 || extent[1] != 2.5 {
		t.Fatalf("Float64Slice() = %#v, want parsed extent", extent)
	}
}

func TestValueFromSectionsSupportsNestedSpatialMetadata(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"geometry_column": "legacy_geom",
		"extensions": map[string]interface{}{
			"spatial": map[string]interface{}{
				"spatial_metadata": map[string]interface{}{
					"geometry_column": "shape",
				},
			},
		},
	}

	got := StringFromSections(attrs, "geometry_column", "extensions.spatial", "extensions.spatial.spatial_metadata")
	if got != "shape" {
		t.Fatalf("geometry_column = %q, want shape", got)
	}
}

func TestSectionSupportsNamedStringMapTypes(t *testing.T) {
	t.Parallel()

	type jsonMap map[string]interface{}
	attrs := map[string]interface{}{
		"storage": jsonMap{
			"physical_path": "standard.parquet",
		},
	}

	if got := String(attrs, "storage", "physical_path"); got != "standard.parquet" {
		t.Fatalf("String() = %q, want standard.parquet", got)
	}
}
