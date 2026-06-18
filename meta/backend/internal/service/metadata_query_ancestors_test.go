package service

import (
	"strings"
	"testing"

	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func TestGetNodeAncestorsReturnsRootToTarget(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := NewMetadataQueryService(db)

	root := createAncestorNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "MinIO", FullName: "", Depth: 0})
	bucket := createAncestorNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 1})
	prefix := createAncestorNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &bucket.ID, NodeType: "prefix", Name: "reports", FullName: "addp/reports", Depth: 2})

	got, err := svc.GetNodeAncestors(7, prefix.ID)
	if err != nil {
		t.Fatalf("GetNodeAncestors() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %#v", len(got), got)
	}
	if got[0].ID != root.ID || got[1].ID != bucket.ID || got[2].ID != prefix.ID {
		t.Fatalf("chain ids = [%d %d %d], want [%d %d %d]", got[0].ID, got[1].ID, got[2].ID, root.ID, bucket.ID, prefix.ID)
	}
}

func TestGetItemAncestorsReturnsItemAndParentChain(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := NewMetadataQueryService(db)

	root := createAncestorNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "MinIO", FullName: "", Depth: 0})
	bucket := createAncestorNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 1})
	item := createAncestorItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: bucket.ID,
		ItemType: "object", Name: "report.pdf", FullName: "addp/report.pdf",
		Fingerprint: "fp",
	})

	got, err := svc.GetItemAncestors(7, item.ID)
	if err != nil {
		t.Fatalf("GetItemAncestors() error = %v", err)
	}
	if got.Item.ID != item.ID {
		t.Fatalf("item id = %d, want %d", got.Item.ID, item.ID)
	}
	if len(got.Ancestors) != 2 {
		t.Fatalf("ancestor len = %d, want 2: %#v", len(got.Ancestors), got.Ancestors)
	}
	if got.Ancestors[0].ID != root.ID || got.Ancestors[1].ID != bucket.ID {
		t.Fatalf("ancestor ids = [%d %d], want [%d %d]", got.Ancestors[0].ID, got.Ancestors[1].ID, root.ID, bucket.ID)
	}
}

func TestGetNodeAncestorsFailsWhenParentMissing(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := NewMetadataQueryService(db)

	missingParentID := uint(404)
	node := createAncestorNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &missingParentID, NodeType: "prefix", Name: "reports", FullName: "addp/reports", Depth: 2})

	_, err := svc.GetNodeAncestors(7, node.ID)
	if err == nil || !strings.Contains(err.Error(), "ancestor missing") {
		t.Fatalf("err = %v, want ancestor missing", err)
	}
}

func createAncestorNode(t *testing.T, db *gorm.DB, node models.MetaNode) models.MetaNode {
	t.Helper()
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	return node
}

func createAncestorItem(t *testing.T, db *gorm.DB, item models.MetaItem) models.MetaItem {
	t.Helper()
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}
	return item
}
