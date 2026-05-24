package jsonmap

import "testing"

func TestStringReadsNestedSection(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"physical_path": "top-level.parquet",
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
		"capabilities": map[string]interface{}{
			"spatial": map[string]interface{}{
				"extent": []interface{}{1, "2.5", int64(3), float32(4)},
			},
		},
	}

	if got := Int64(attrs, "storage", "object_count"); got != 7 {
		t.Fatalf("Int64() = %d, want 7", got)
	}
	extent := Float64Slice(attrs, "capabilities.spatial", "extent")
	if len(extent) != 4 || extent[1] != 2.5 {
		t.Fatalf("Float64Slice() = %#v, want parsed extent", extent)
	}
}

func TestValueFromSectionsSupportsNestedSections(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"primary_geometry_column": "top_level_geom",
		"capabilities": map[string]interface{}{
			"spatial": map[string]interface{}{
				"primary_geometry_column": "shape",
			},
		},
	}

	got := StringFromSections(attrs, "primary_geometry_column", "capabilities.spatial")
	if got != "shape" {
		t.Fatalf("primary_geometry_column = %q, want shape", got)
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

func TestInterfaceMapAndSliceSupportNamedTypes(t *testing.T) {
	t.Parallel()

	type jsonMap map[string]interface{}
	type jsonSlice []jsonMap

	mapped := InterfaceMap(jsonMap{"name": "orders"})
	if mapped["name"] != "orders" {
		t.Fatalf("InterfaceMap() = %#v, want named map converted", mapped)
	}

	items := InterfaceSlice(jsonSlice{{"name": "id"}, {"name": "name"}})
	if len(items) != 2 {
		t.Fatalf("InterfaceSlice() len = %d, want 2", len(items))
	}
	first := InterfaceMap(items[0])
	if first["name"] != "id" {
		t.Fatalf("InterfaceSlice()[0] = %#v, want first map", items[0])
	}
}
