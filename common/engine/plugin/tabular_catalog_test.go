package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"gorm.io/gorm"
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

func TestDescribeTabularItemOnlyRunsRowCountWhenStatisticsRequested(t *testing.T) {
	rowCountCalls := 0
	callbacks := TabularCatalogCallbacks{
		ListNamespaces: func(context.Context, *gorm.DB) ([]NamespaceInfo, error) {
			return nil, nil
		},
		ListTables: func(context.Context, *gorm.DB, string) ([]datatype.TableInfo, error) {
			return []datatype.TableInfo{{Name: "orders", Kind: CatalogKindTable}}, nil
		},
		ListColumns: func(context.Context, *gorm.DB, string, string) ([]datatype.FieldInfo, error) {
			return []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}}, nil
		},
		RowCount: func(context.Context, *gorm.DB, string, string) (int64, error) {
			rowCountCalls++
			return 42, nil
		},
	}
	engine := &Engine{ID: 7001, EngineType: "tabular_catalog_test"}
	path := CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: engine.ID,
		Segments: []CatalogSegment{
			{Term: CatalogTermDatabase, Kind: CatalogKindNamespace, Name: "analytics"},
			{Term: CatalogTermTable, Kind: CatalogKindTable, Name: "orders"},
		},
	}

	Register(&tabularCatalogTestPlugin{})
	t.Cleanup(func() {
		Unregister("tabular_catalog_test")
		ClosePool(engine.ID)
	})

	item, err := DescribeTabularItem(context.Background(), callbacks, engine, path, MetadataOptions{})
	if err != nil {
		t.Fatalf("DescribeTabularItem() error = %v", err)
	}
	if rowCountCalls != 0 {
		t.Fatalf("row count calls = %d, want 0 without IncludeStatistics", rowCountCalls)
	}
	if item.Table.RowCount != nil {
		t.Fatalf("Table.RowCount = %#v, want nil without IncludeStatistics", item.Table.RowCount)
	}

	item, err = DescribeTabularItem(context.Background(), callbacks, engine, path, MetadataOptions{IncludeStatistics: true})
	if err != nil {
		t.Fatalf("DescribeTabularItem(IncludeStatistics) error = %v", err)
	}
	if rowCountCalls != 1 {
		t.Fatalf("row count calls = %d, want 1 with IncludeStatistics", rowCountCalls)
	}
	if item.Table.RowCount == nil || *item.Table.RowCount != 42 {
		t.Fatalf("Table.RowCount = %#v, want 42", item.Table.RowCount)
	}
}

type tabularCatalogTestPlugin struct{}

func (*tabularCatalogTestPlugin) Type() string                                         { return "tabular_catalog_test" }
func (*tabularCatalogTestPlugin) DisplayName() string                                  { return "Tabular Catalog Test" }
func (*tabularCatalogTestPlugin) EngineOrigin() string                                 { return "general" }
func (*tabularCatalogTestPlugin) DefaultPort() int                                     { return 0 }
func (*tabularCatalogTestPlugin) RequiredFields() []string                             { return nil }
func (*tabularCatalogTestPlugin) SensitiveFields() []string                            { return nil }
func (*tabularCatalogTestPlugin) ValidateConnectionInfo(ConnectionInfo) error          { return nil }
func (*tabularCatalogTestPlugin) TestConnection(context.Context, ConnectionInfo) error { return nil }
func (*tabularCatalogTestPlugin) Capabilities() EngineCapabilities                     { return EngineCapabilities{} }
func (*tabularCatalogTestPlugin) GetDialect() string                                   { return "test" }
func (*tabularCatalogTestPlugin) CreateConnectionPool(ConnectionInfo, *PoolConfig) (*gorm.DB, error) {
	return &gorm.DB{}, nil
}
