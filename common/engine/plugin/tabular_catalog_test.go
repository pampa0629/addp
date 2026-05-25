package plugin

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
)

func TestTableAttributesCarriesTableNativeFacts(t *testing.T) {
	updatedAt := time.Unix(200, 0)
	table := datatype.TableInfo{
		Name:      "orders",
		Comment:   "order facts",
		UpdatedAt: &updatedAt,
		Native: map[string]interface{}{
			"engine": "MergeTree",
		},
	}

	attrs := tableAttributes("analytics", table)
	table.Native["engine"] = "Log"

	if attrs["namespace"] != "analytics" || attrs["table"] != "orders" {
		t.Fatalf("table identity attrs = %#v", attrs)
	}
	if attrs["comment"] != "order facts" {
		t.Fatalf("comment attr = %#v, want order facts", attrs["comment"])
	}
	if attrs["updated_at"] != &updatedAt {
		t.Fatalf("updated_at attr = %#v, want original pointer", attrs["updated_at"])
	}
	native, ok := attrs["native"].(map[string]interface{})
	if !ok || native["engine"] != "MergeTree" {
		t.Fatalf("native attrs = %#v, want copied engine", attrs["native"])
	}
}

func TestBuildTabularItemMetadataCarriesTableInfo(t *testing.T) {
	rowCount := int64(42)
	updatedAt := time.Unix(300, 0)
	item := buildTabularItemMetadata(CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: 7,
		Segments: []CatalogSegment{
			{Term: CatalogTermDatabase, Kind: CatalogKindNamespace, Name: "analytics"},
			{Term: CatalogTermTable, Kind: CatalogKindTable, Name: "orders"},
		},
	}, "analytics", "orders", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, NativeType: "bigint", PrimaryKey: true},
		{Name: "name", Type: datatype.FieldTypeString, NativeType: "text", Nullable: true},
	}, datatype.TableInfo{
		Name:      "orders",
		Kind:      "view",
		Comment:   "order facts",
		RowCount:  &rowCount,
		UpdatedAt: &updatedAt,
		Native: map[string]interface{}{
			"engine": "MergeTree",
		},
	}, true, CatalogKindTable, nil, nil, nil)
	if item.Table == nil {
		t.Fatal("ItemMetadata.Table is nil")
	}
	if item.Table.Name != "orders" || item.Table.Kind != "view" || item.Table.Comment != "order facts" {
		t.Fatalf("Table = %#v", item.Table)
	}
	if item.Table.RowCount == nil || *item.Table.RowCount != rowCount {
		t.Fatalf("Table.RowCount = %#v, want %d", item.Table.RowCount, rowCount)
	}
	if len(item.Table.Fields) != 2 || item.Table.Fields[0].Name != "id" || !item.Table.Fields[0].PrimaryKey {
		t.Fatalf("Table.Fields = %#v", item.Table.Fields)
	}
	if len(item.Table.PrimaryKey) != 1 || item.Table.PrimaryKey[0] != "id" {
		t.Fatalf("Table.PrimaryKey = %#v", item.Table.PrimaryKey)
	}
	if item.Table.Native["engine"] != "MergeTree" {
		t.Fatalf("Table.Native = %#v", item.Table.Native)
	}
}

func TestTabularCatalogNodeFromItemCarriesAttributes(t *testing.T) {
	rowCount := int64(10)
	item := &ItemMetadata{
		Table: &datatype.TableInfo{
			Name:     "orders",
			Kind:     "view",
			Comment:  "orders",
			RowCount: &rowCount,
			Native:   map[string]interface{}{"engine": "MergeTree"},
		},
	}

	node := tabularCatalogNodeFromItem(CatalogPath{Version: CatalogPathVersion}, "orders", "view", item)

	if node.Kind != "view" || !node.IsItem {
		t.Fatalf("node kind/is_item = %q/%v", node.Kind, node.IsItem)
	}
	if node.Stats["row_count"] != int64(10) {
		t.Fatalf("Stats = %#v", node.Stats)
	}
	native, ok := node.Attributes["native"].(map[string]interface{})
	if node.Attributes["comment"] != "orders" || !ok || native["engine"] != "MergeTree" {
		t.Fatalf("Attributes = %#v", node.Attributes)
	}
}
