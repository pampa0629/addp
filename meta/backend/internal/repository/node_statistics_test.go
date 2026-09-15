package repository

import (
	"testing"
	"time"

	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func TestNodeStatisticsReflectCurrentCatalog(t *testing.T) {
	assertNodeStatisticsLifecycle(t, metatest.OpenMetadataDB(t))
}

// Shared with the PostgreSQL gate: SQLite is not the only SQL verification.
func assertNodeStatisticsLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	node := func(id, tenant, engine, parent uint) {
		n := models.MetaNode{ID: id, TenantID: tenant, EngineID: engine, NodeType: "prefix", Name: "same-name", FullName: "irrelevant-path", Depth: 1}
		if parent != 0 {
			n.ParentNodeID = &parent
		}
		if err := db.Create(&n).Error; err != nil {
			t.Fatal(err)
		}
	}
	node(1, 7, 9, 0)
	node(2, 7, 9, 1)
	node(3, 7, 9, 2)
	node(4, 7, 9, 1) // empty sibling
	node(5, 7, 9, 1) // deleted branch, with an active descendant
	node(6, 7, 9, 5)
	node(7, 8, 9, 1)  // malformed cross-tenant edge must not leak
	node(8, 7, 10, 1) // malformed cross-engine edge must not leak
	if err := db.Delete(&models.MetaNode{}, 5).Error; err != nil {
		t.Fatal(err)
	}
	item := func(id, tenant, engine, parent uint, size *int64) {
		i := models.MetaItem{ID: id, TenantID: tenant, EngineID: engine, NodeID: parent, Name: "item", ItemType: "object", Fingerprint: time.Unix(int64(id), 0).Format(time.RFC3339), SizeBytes: size}
		if err := db.Create(&i).Error; err != nil {
			t.Fatal(err)
		}
	}
	size := int64(10)
	item(1, 7, 9, 2, &size)
	item(2, 7, 9, 3, nil) // unknown size still counts as one logical item
	item(3, 7, 9, 6, &size)
	item(4, 8, 9, 7, &size)
	item(5, 7, 10, 8, &size)
	item(6, 8, 9, 2, &size) // malformed item ownership must not leak
	item(7, 7, 10, 2, &size)
	item(8, 7, 9, 999, &size) // orphan
	check := func(id uint, count int, bytes int64, children bool) {
		t.Helper()
		stats, err := QueryNodeStatistics(db, 7, 9, []uint{1, 2, 3, 4, 7, 8, 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(stats) != 4 {
			t.Fatalf("duplicate or unauthorized roots: %#v", stats)
		}
		for _, stat := range stats {
			if stat.NodeID == id {
				if stat.ItemCount != count || stat.TotalSizeBytes != bytes || stat.HasChildren != children {
					t.Fatalf("node %d: %#v, want count=%d size=%d children=%v", id, stat, count, bytes, children)
				}
				return
			}
		}
		t.Fatalf("missing node %d", id)
	}
	check(1, 2, 10, true)
	check(2, 2, 10, true)
	check(3, 1, 0, true)
	check(4, 0, 0, false)

	// Upload commits a new item; no count-maintenance call follows.
	item(9, 7, 9, 2, &size)
	check(1, 3, 20, true)
	check(2, 3, 20, true)
	// Overwrite changes size, never item cardinality.
	if err := db.Model(&models.MetaItem{}).Where("id = ?", 9).Update("size_bytes", 30).Error; err != nil {
		t.Fatal(err)
	}
	check(2, 3, 40, true)
	// Reparent: old subtree shrinks, sibling grows, common ancestor is unchanged.
	if err := db.Model(&models.MetaItem{}).Where("id = ?", 9).Update("node_id", 4).Error; err != nil {
		t.Fatal(err)
	}
	check(2, 2, 10, true)
	check(4, 1, 30, true)
	check(1, 3, 40, true)
	if err := db.Delete(&models.MetaItem{}, 9).Error; err != nil {
		t.Fatal(err)
	}
	check(1, 2, 10, true)
	check(4, 0, 0, false)
	if err := db.Unscoped().Model(&models.MetaItem{}).Where("id = ?", 9).Update("deleted_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	check(1, 3, 40, true)
	// Moving a whole node follows parent edges, even with stale/identical path text.
	if err := db.Model(&models.MetaNode{}).Where("id = ?", 3).Update("parent_node_id", 4).Error; err != nil {
		t.Fatal(err)
	}
	check(2, 1, 10, true)
	check(4, 2, 30, true)
	check(1, 3, 40, true)
	if stats, err := QueryNodeStatistics(db, 7, 9, nil); err != nil || len(stats) != 0 {
		t.Fatalf("empty query: %#v, %v", stats, err)
	}
}
