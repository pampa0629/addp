package metatest

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// OpenMetadataDB opens an in-memory sqlite database with the meta schema attached.
func OpenMetadataDB(t testing.TB, opts ...MetadataDBOption) *gorm.DB {
	t.Helper()
	cfg := metadataDBConfig{metaItem: true}
	for _, opt := range opts {
		opt(&cfg)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS meta").Error; err != nil {
		t.Fatalf("attach meta schema: %v", err)
	}
	createMetaNodeTable(t, db)
	if cfg.metaItem {
		createMetaItemTable(t, db)
	}
	return db
}

type MetadataDBOption func(*metadataDBConfig)

type metadataDBConfig struct {
	metaItem bool
}

func WithoutMetaItemTable() MetadataDBOption {
	return func(cfg *metadataDBConfig) {
		cfg.metaItem = false
	}
}

func createMetaNodeTable(t testing.TB, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`
		CREATE TABLE meta.meta_node (
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
}

func createMetaItemTable(t testing.TB, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`
		CREATE TABLE meta.meta_item (
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
}
