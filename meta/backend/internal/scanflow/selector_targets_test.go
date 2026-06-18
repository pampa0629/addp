package scanflow

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestTargetPathsFromRootNodeScansWholeEngine(t *testing.T) {
	t.Parallel()

	got := TargetPathsFromNode(models.MetaNode{
		NodeType:     "server",
		Name:         "Business PostgreSQL",
		FullName:     "",
		ParentNodeID: nil,
	})

	if len(got) != 0 {
		t.Fatalf("TargetPathsFromNode(root) = %#v, want no explicit target", got)
	}
}

func TestTargetPathsFromNodeUsesFullName(t *testing.T) {
	t.Parallel()

	parentID := uint(1)
	got := TargetPathsFromNode(models.MetaNode{
		NodeType:     "schema",
		Name:         "public",
		FullName:     "/public/",
		ParentNodeID: &parentID,
	})
	want := []string{"public"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TargetPathsFromNode() = %#v, want %#v", got, want)
	}
}

func TestTargetPathsFromItemDoesNotExpandMultiRefs(t *testing.T) {
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

	got := TargetPathsFromItem(item)
	want := []string{"lake/roads.shp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TargetPathsFromItem() = %#v, want %#v", got, want)
	}
}

func TestTargetPathsFromItemFallsBackToPhysicalPath(t *testing.T) {
	t.Parallel()

	item := models.MetaItem{
		Attributes: models.JSONMap{
			"storage": map[string]interface{}{
				"physical_path": "/bucket/docs/readme.md",
			},
		},
		FullName: "legacy/path",
	}

	got := TargetPathsFromItem(item)
	want := []string{"bucket/docs/readme.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TargetPathsFromItem() = %#v, want %#v", got, want)
	}
}

func TestTargetPathsFromLocatorUsesTopCatalogForTable(t *testing.T) {
	t.Parallel()

	got := TargetPathsFromLocator("addp://engine/9/path/public/roads?type=table")
	if !reflect.DeepEqual(got, []string{"public"}) {
		t.Fatalf("TargetPathsFromLocator() = %#v", got)
	}
}

func TestTargetPathsFromLocatorUsesSharedParserDecoding(t *testing.T) {
	t.Parallel()

	got := TargetPathsFromLocator("addp://engine/9/path/bucket/folder%20name/report.pdf?type=object")
	if !reflect.DeepEqual(got, []string{"bucket/folder name/report.pdf"}) {
		t.Fatalf("TargetPathsFromLocator() = %#v", got)
	}
}

func TestEngineIDFromLocator(t *testing.T) {
	t.Parallel()

	got, ok := EngineIDFromLocator("addp://engine/42/path/public?type=schema")
	if !ok || got != 42 {
		t.Fatalf("EngineIDFromLocator() = %d, %v", got, ok)
	}
}
