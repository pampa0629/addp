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
			node: plugin.CatalogNode{Kind: plugin.CatalogKindCollection},
			want: "collection",
		},
		{
			name: "graph label",
			node: plugin.CatalogNode{Kind: plugin.CatalogKindLabel},
			want: "label",
		},
		{
			name: "graph relationship",
			node: plugin.CatalogNode{Kind: plugin.CatalogKindRelationship},
			want: "relationship",
		},
		{
			name: "unsupported kind",
			node: plugin.CatalogNode{Kind: plugin.CatalogKindNamespace},
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

func TestBuildDocCollectionAttributesWritesTypeInfoTableSection(t *testing.T) {
	t.Parallel()

	attrs := buildDocCollectionAttributesFromMetadata(&plugin.ItemMetadata{
		Fields: []plugin.FieldInfo{{
			Name: "name",
			Type: "string",
		}},
		Indexes: []plugin.IndexInfo{{
			Name:      "name_idx",
			Fields:    []string{"name"},
			IndexType: "btree",
		}},
	})

	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	if _, ok := table["fields"]; !ok {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
	if _, ok := table["indexes"]; !ok {
		t.Fatalf("type_info.table.indexes missing: %#v", table)
	}
	if attrs["fields"] != nil || attrs["indexes"] != nil || attrs["schema"] != nil {
		t.Fatalf("legacy flat/schema fields should not be written: %#v", attrs)
	}
}

func TestApplyNoSQLDataItemAttributesDoesNotWriteEngineFormat(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	applyNoSQLDataItemAttributes(attrs, "collection")

	item := attrs["item"].(map[string]interface{})
	if item["organization"] != "single" || item["data_type"] != "table" {
		t.Fatalf("item attrs = %#v", item)
	}
	if item["format"] != nil {
		t.Fatalf("native NoSQL item should not write item.format: %#v", item)
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
