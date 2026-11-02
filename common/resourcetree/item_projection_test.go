package resourcetree

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/models"
)

func assertDataItemProjection(t *testing.T, node *TreeNode, itemID uint) {
	t.Helper()
	loc, err := ParseURI(node.Locator)
	if err != nil || loc.ItemID == nil || *loc.ItemID != itemID || loc.NodeID != nil || node.ID != node.Locator {
		t.Fatalf("invalid item identity: %+v (%v)", node, err)
	}
	encoded, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"item_count", "node_id", "is_meta_item", "total_size_bytes"} {
		if _, ok := wire.Metadata[key]; ok {
			t.Fatalf("DataItem leaked %s: %s", key, encoded)
		}
	}
	if node.HasChildren || len(node.Children) != 0 || node.Metadata["has_children"] != false {
		t.Fatalf("DataItem is expandable: %s", encoded)
	}
}

func TestMetaItemProjectionKeepsIdentityAndCountsSeparate(t *testing.T) {
	engine := &models.Engine{ID: 4, EngineType: "nfs"}
	rows, size := int64(25), int64(512)
	for _, itemType := range []string{"table", "file", "directory", "prefix"} {
		t.Run(itemType, func(t *testing.T) {
			item := models.MetaItem{ID: 7, NodeID: 7, ItemType: itemType, Name: "dataset", FullName: "lake/dataset", RowCount: &rows, SizeBytes: &size,
				Attributes: map[string]interface{}{
					"item_count": 999, "node_id": 100007, "item_id": 999, "has_children": true,
					"storage":   map[string]interface{}{"object_count": 10},
					"type_info": map[string]interface{}{"container": map[string]interface{}{"child_count": 3}},
				}}
			tree := NewTreeBuilder().ConvertMetaItems(engine, []models.MetaItem{item})[0]
			assertDataItemProjection(t, tree, item.ID)
			if tree.Metadata["row_count"] != rows || tree.Metadata["size_bytes"] != size {
				t.Fatalf("item facts lost: %+v", tree.Metadata)
			}
			if item.Attributes["has_children"] != true || len(item.Attributes) != 6 {
				t.Fatal("source attributes mutated")
			}
			if _, exists := item.Attributes["type_info"].(map[string]interface{})["table"]; exists {
				t.Fatal("row count projection mutated source type_info")
			}
		})
	}
}

func TestWholeItemHidesItsNativeDirectoryContents(t *testing.T) {
	rootID, dirID, innerID := uint(1), uint(2), uint(3)
	tree, err := NewTreeBuilder().BuildFromMeta(&models.Engine{ID: 4, EngineType: "nfs"}, []*models.MetaNode{
		{ID: rootID, NodeType: "root"},
		{ID: innerID, ParentNodeID: &dirID, NodeType: "dir", FullName: "tiles/internal"},
		{ID: dirID, ParentNodeID: &rootID, NodeType: "dir", FullName: "tiles"},
	}, []models.MetaItem{
		{ID: 2, NodeID: dirID, ItemType: "file", Name: "tiles", FullName: "tiles", Attributes: map[string]interface{}{"item": map[string]interface{}{"layout": "whole"}}},
		{ID: 3, NodeID: innerID, ItemType: "file", Name: "part", FullName: "tiles/internal/part"},
	})
	if err != nil || len(tree.Children) != 1 || tree.Children[0].Label != "tiles" {
		t.Fatalf("whole item exposed native content: %+v, %v", tree, err)
	}
	assertDataItemProjection(t, tree.Children[0], 2)
}

func TestResourceTreeSeparatesNodeAndItemIDSpaces(t *testing.T) {
	rootID, branchID := uint(1), uint(100007)
	tree, err := NewTreeBuilder().BuildFromMeta(&models.Engine{ID: 4, EngineType: "postgresql"}, []*models.MetaNode{
		{ID: rootID, NodeType: "server"},
		{ID: branchID, ParentNodeID: &rootID, NodeType: "schema", Name: "public", FullName: "public", ItemCount: 2},
	}, []models.MetaItem{
		{ID: 7, NodeID: branchID, ItemType: "table", Name: "orders", FullName: "public.orders"},
		{ID: branchID, NodeID: branchID, ItemType: "table", Name: "lines", FullName: "public.lines"},
	})
	if err != nil || len(tree.Children) != 1 || len(tree.Children[0].Children) != 2 {
		t.Fatalf("ID collision lost a resource: %+v, %v", tree, err)
	}
	branch := tree.Children[0]
	if branch.Metadata["node_id"] != branchID || branch.Metadata["item_count"] != 2 || !branch.HasChildren || branch.Metadata["has_children"] != true {
		t.Fatalf("branch facts lost: %+v", branch)
	}
	assertDataItemProjection(t, branch.Children[0], 7)
	assertDataItemProjection(t, branch.Children[1], branchID)
}

func TestNodeExpansionUsesExplicitFactsOnly(t *testing.T) {
	builder := NewTreeBuilder()
	for _, tc := range []struct {
		count       int
		hasChildren bool
	}{{10, false}, {0, true}} {
		node := builder.ConvertMetaNodes(&models.Engine{ID: 4, EngineType: "nfs"}, []*models.MetaNode{{ID: 1, NodeType: "dir", ItemCount: tc.count, HasChildren: tc.hasChildren,
			Attributes: map[string]interface{}{"has_children": !tc.hasChildren, "item_count": 999}}})[0]
		if node.HasChildren != tc.hasChildren || node.Metadata["has_children"] != tc.hasChildren || node.Metadata["item_count"] != tc.count {
			t.Fatalf("node statistics changed expansion: %+v", node)
		}
	}
}
