package service

import (
	"io"
	"log/slog"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNoSQLItemType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node plugin.CatalogNode
		want string
	}{
		{
			name: "document collection",
			node: plugin.CatalogNode{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, IsItem: true},
			want: "collection",
		},
		{
			name: "graph label",
			node: plugin.CatalogNode{Term: plugin.CatalogTermLabel, Kind: plugin.CatalogKindLabel, IsItem: true},
			want: "label",
		},
		{
			name: "graph relationship",
			node: plugin.CatalogNode{Term: plugin.CatalogTermRelationship, Kind: plugin.CatalogKindRelationship, IsItem: true},
			want: "relationship",
		},
		{
			name: "container is not item",
			node: plugin.CatalogNode{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, IsContainer: true},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := noSQLItemType(tt.node); got != tt.want {
				t.Fatalf("noSQLItemType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSoftDeleteLegacyGraphTableItems(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS metadata").Error; err != nil {
		t.Fatalf("attach metadata schema: %v", err)
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
			attributes JSON,
			created_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create meta_item table: %v", err)
	}

	items := []models.MetaItem{
		{TenantID: 1, EngineID: 25, NodeID: 10, ItemType: "table", Name: "Person", Fingerprint: "old-table", Attributes: models.JSONMap{}},
		{TenantID: 1, EngineID: 25, NodeID: 10, ItemType: "label", Name: "Person", Fingerprint: "label", Attributes: models.JSONMap{}},
		{TenantID: 1, EngineID: 25, NodeID: 11, ItemType: "table", Name: "OtherDB", Fingerprint: "other-node", Attributes: models.JSONMap{}},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatalf("seed items: %v", err)
	}

	svc := NewNoSQLScanService(db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil)
	svc.softDeleteLegacyGraphTableItems(1, 25, 10)

	var active []models.MetaItem
	if err := db.Where("deleted_at IS NULL").Order("fingerprint").Find(&active).Error; err != nil {
		t.Fatalf("query active items: %v", err)
	}
	got := make([]string, 0, len(active))
	for _, item := range active {
		got = append(got, item.Fingerprint)
	}
	want := []string{"label", "other-node"}
	if len(got) != len(want) {
		t.Fatalf("active fingerprints = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("active fingerprints = %v, want %v", got, want)
		}
	}
}
