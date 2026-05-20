package service

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestScanTargetFromItemDoesNotExpandMultiRefs(t *testing.T) {
	t.Parallel()

	item := models.MetaItem{
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout": "multi",
				"refs": []map[string]interface{}{
					{"path": "roads.shp"},
					{"path": "roads.dbf"},
					{"path": "roads.shp"},
				},
			},
			"storage": map[string]interface{}{
				"physical_path": "/lake/roads.shp",
			},
		},
		ItemType: "table",
		FullName: "public.roads",
	}

	got := scanTargetFromItem(item)
	want := []string{"lake/roads.shp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanTargetFromItem() = %#v, want %#v", got, want)
	}
}

func TestScanTargetFromItemFallsBackToPhysicalPath(t *testing.T) {
	t.Parallel()

	item := models.MetaItem{
		Attributes: models.JSONMap{
			"storage": map[string]interface{}{
				"physical_path": "/bucket/docs/readme.md",
			},
		},
		FullName: "legacy/path",
	}

	got := scanTargetFromItem(item)
	want := []string{"bucket/docs/readme.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanTargetFromItem() = %#v, want %#v", got, want)
	}
}
