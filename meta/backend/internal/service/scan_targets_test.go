package service

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestScanTargetFromRootNodeScansWholeEngine(t *testing.T) {
	t.Parallel()

	got := scanTargetFromNode(models.MetaNode{
		NodeType:     "server",
		Name:         "Business PostgreSQL",
		FullName:     "",
		ParentNodeID: nil,
	})

	if len(got) != 0 {
		t.Fatalf("scanTargetFromNode(root) = %#v, want no explicit target", got)
	}
}

func TestScanTargetFromNodeUsesFullName(t *testing.T) {
	t.Parallel()

	parentID := uint(1)
	got := scanTargetFromNode(models.MetaNode{
		NodeType:     "schema",
		Name:         "public",
		FullName:     "/public/",
		ParentNodeID: &parentID,
	})
	want := []string{"public"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanTargetFromNode() = %#v, want %#v", got, want)
	}
}

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
