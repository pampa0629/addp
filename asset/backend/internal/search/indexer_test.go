package search

import (
	"reflect"
	"testing"
)

func TestBuildAssetSearchFiltersPreservesUncategorizedSemantics(t *testing.T) {
	got := buildAssetSearchFilters(7, "dataset", []int64{-1})
	want := []string{
		"tenant_id = 7",
		`status = "published"`,
		`type_code = "dataset"`,
		"category_id IS NULL",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildAssetSearchFilters() = %#v, want %#v", got, want)
	}

	got = buildAssetSearchFilters(7, "", []int64{12, 15, 19})
	want = []string{"tenant_id = 7", `status = "published"`, "(category_id = 12 OR category_id = 15 OR category_id = 19)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildAssetSearchFilters() = %#v, want %#v", got, want)
	}
}
