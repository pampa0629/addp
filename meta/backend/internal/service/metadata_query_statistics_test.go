package service

import (
	"testing"
	"time"

	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
)

func TestAllNodeQueriesShareCurrentSubtreeStatistics(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := NewMetadataQueryService(db)
	stamp := time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC)
	root := createAncestorNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "MinIO", FullName: "", ScannedAt: &stamp, ScanStatus: "completed"})
	dir := createAncestorNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "prefix", Name: "doc", FullName: "addp/doc", ScannedAt: &stamp, ScanStatus: "completed"})
	size := int64(42)
	item := createAncestorItem(t, db, models.MetaItem{TenantID: 7, EngineID: 9, NodeID: dir.ID, ItemType: "object", Name: "report.md", FullName: "addp/doc/report.md", Fingerprint: "report", SizeBytes: &size})
	check := func(nodes []models.MetaNodeLite, err error) {
		t.Helper()
		if err != nil || len(nodes) == 0 {
			t.Fatalf("query: %#v, %v", nodes, err)
		}
		for _, n := range nodes {
			if n.ItemCount != 1 || n.TotalSizeBytes != 42 || !n.HasChildren {
				t.Fatalf("statistics missing: %#v", n)
			}
			if n.ScannedAt == nil || *n.ScannedAt != stamp.Format(time.RFC3339) || n.ScanStatus != "completed" {
				t.Fatalf("query modified scan facts: %#v", n)
			}
		}
	}
	n, err := svc.GetMetaNodeByID(7, dir.ID)
	if err != nil {
		t.Fatal(err)
	}
	check([]models.MetaNodeLite{*n}, nil)
	n, err = svc.GetNodeByCatalogPath(7, 9, "addp/doc")
	if err != nil {
		t.Fatal(err)
	}
	check([]models.MetaNodeLite{*n}, nil)
	check(svc.GetNodeChildren(7, root.ID))
	check(svc.GetNodeAncestors(7, dir.ID))
	ancestors, err := svc.GetItemAncestors(7, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	check(ancestors.Ancestors, nil)
	tree, err := svc.GetMetadataTree(7, 9)
	if err != nil {
		t.Fatal(err)
	}
	check(tree.TopNodes, nil)
	check(tree.ChildNodes, nil)

	// Same service instance: no stale cache after deletion, and no false expand arrow.
	if err := db.Delete(&models.MetaItem{}, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	n, err = svc.GetMetaNodeByID(7, dir.ID)
	if err != nil || n.ItemCount != 0 || n.TotalSizeBytes != 0 || n.HasChildren {
		t.Fatalf("deleted item: %#v, %v", n, err)
	}
	if _, err := svc.GetMetaNodeByID(8, dir.ID); err == nil {
		t.Fatal("cross-tenant node query succeeded")
	}
}
