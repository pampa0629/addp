package repository

import (
	"testing"

	"github.com/addp/meta/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpsertNodeUsesExplicitFullNameAsSemanticKey(t *testing.T) {
	db := openScanRepositoryTestDB(t)
	repo := NewScanRepository(db)

	root, err := repo.UpsertNode(1, 26, nil, "root", "", strPtr(""), models.JSONMap{"path": "/"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	renamedRoot, err := repo.UpsertNode(1, 26, nil, "root", ".", strPtr(""), models.JSONMap{"path": "/"})
	if err != nil {
		t.Fatalf("upsert renamed root: %v", err)
	}
	if renamedRoot.ID != root.ID {
		t.Fatalf("root id = %d, want %d", renamedRoot.ID, root.ID)
	}
	if renamedRoot.Name != "." {
		t.Fatalf("root name = %q, want .", renamedRoot.Name)
	}

	lake, err := repo.UpsertNode(1, 26, root, "dir", "lake", strPtr("lake"), models.JSONMap{"path": "/lake/"})
	if err != nil {
		t.Fatalf("create lake: %v", err)
	}
	renamedLake, err := repo.UpsertNode(1, 26, renamedRoot, "dir", "lake", strPtr("lake"), models.JSONMap{"path": "/lake/"})
	if err != nil {
		t.Fatalf("upsert lake with renamed parent: %v", err)
	}
	if renamedLake.ID != lake.ID {
		t.Fatalf("lake id = %d, want %d", renamedLake.ID, lake.ID)
	}
	if renamedLake.ParentNodeID == nil || *renamedLake.ParentNodeID != renamedRoot.ID {
		t.Fatalf("lake parent = %v, want %d", renamedLake.ParentNodeID, renamedRoot.ID)
	}

	var count int64
	if err := db.Model(&models.MetaNode{}).Where("engine_id = ?", 26).Count(&count).Error; err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	if count != 2 {
		t.Fatalf("node count = %d, want 2", count)
	}
}

func TestHardDeleteInvalidEngineGraphRemovesNodeItemConflicts(t *testing.T) {
	db := openScanRepositoryTestDB(t)
	repo := NewScanRepository(db)

	root, err := repo.UpsertNode(1, 26, nil, "root", "Business NFS", strPtr(""), models.JSONMap{})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	staleNode, err := repo.UpsertNode(1, 26, root, "dir", "README.md", strPtr("README.md"), models.JSONMap{})
	if err != nil {
		t.Fatalf("create stale node: %v", err)
	}
	_, err = repo.UpsertNode(1, 26, staleNode, "dir", "child", strPtr("README.md/child"), models.JSONMap{})
	if err != nil {
		t.Fatalf("create stale child: %v", err)
	}
	_, err = repo.UpsertItemWithDepth(1, 26, root, "file", "README.md", "README.md", models.JSONMap{}, nil, nil, nil, models.ScannedDepthBasic)
	if err != nil {
		t.Fatalf("create file item: %v", err)
	}

	if err := repo.HardDeleteInvalidEngineGraph(1, 26); err != nil {
		t.Fatalf("HardDeleteInvalidEngineGraph() error = %v", err)
	}

	var staleCount int64
	if err := db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND engine_id = ? AND full_name LIKE ?", 1, 26, "README.md%").
		Count(&staleCount).Error; err != nil {
		t.Fatalf("count stale nodes: %v", err)
	}
	if staleCount != 0 {
		t.Fatalf("stale node count = %d, want 0", staleCount)
	}

	item, ok, err := repo.FindItemByFullName(1, 26, "README.md")
	if err != nil {
		t.Fatalf("FindItemByFullName() error = %v", err)
	}
	if !ok || item.NodeID != root.ID {
		t.Fatalf("README item = %#v, found=%v, want item under root %d", item, ok, root.ID)
	}
}

func openScanRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS metadata").Error; err != nil {
		t.Fatalf("attach metadata schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE metadata.meta_node (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			engine_id INTEGER NOT NULL,
			parent_node_id INTEGER,
			node_type TEXT NOT NULL,
			name TEXT NOT NULL,
			depth INTEGER NOT NULL,
			path TEXT,
			full_name TEXT,
			scan_status TEXT,
			scanned_depth TEXT,
			scanned_at DATETIME,
			scan_error TEXT,
			item_count INTEGER,
			total_size_bytes INTEGER,
			attributes JSON,
			created_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create meta_node table: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE metadata.meta_item (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			engine_id INTEGER NOT NULL,
			node_id INTEGER NOT NULL,
			item_type TEXT NOT NULL,
			name TEXT NOT NULL,
			full_name TEXT,
			fingerprint TEXT NOT NULL,
			row_count INTEGER,
			size_bytes INTEGER,
			data_updated_at DATETIME,
			scanned_at DATETIME,
			scanned_depth TEXT,
			attributes JSON,
			created_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create meta_item table: %v", err)
	}
	return db
}

func strPtr(s string) *string {
	return &s
}
