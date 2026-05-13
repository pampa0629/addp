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
	return db
}

func strPtr(s string) *string {
	return &s
}
