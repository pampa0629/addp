package search

import (
	"reflect"
	"testing"
)

func TestBuildAssetSearchFiltersPreservesUncategorizedSemantics(t *testing.T) {
	uncategorized := int64(-1)
	got := buildAssetSearchFilters(7, "dataset", &uncategorized)
	want := []string{
		"tenant_id = 7",
		`status = "published"`,
		`type_code = "dataset"`,
		"category_id IS NULL",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildAssetSearchFilters() = %#v, want %#v", got, want)
	}

	categoryID := int64(12)
	got = buildAssetSearchFilters(7, "", &categoryID)
	want = []string{"tenant_id = 7", `status = "published"`, "category_id = 12"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildAssetSearchFilters() = %#v, want %#v", got, want)
	}
}
