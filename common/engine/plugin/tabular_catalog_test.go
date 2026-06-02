package plugin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"gorm.io/gorm"
)

func TestTabularCatalogEntryCarriesTableInfo(t *testing.T) {
	updatedAt := time.Unix(200, 0)
	table := datatype.TableInfo{
		Name:      "orders",
		Comment:   "order facts",
		UpdatedAt: &updatedAt,
		Fields:    []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
		PrimaryKey: []string{
			"id",
		},
		Native: map[string]interface{}{
			"engine": "MergeTree",
		},
	}

	node := tabularCatalogEntryFromFacts(CatalogPath{Version: CatalogPathVersion}, "orders", "table", &CatalogFacts{
		Kind:      CatalogKindTable,
		Table:     table.Clone(),
		UpdatedAt: &updatedAt,
	})
	if node.Table == nil {
		t.Fatalf("node = %#v, want table info", node)
	}
	table.Native["engine"] = "Log"

	if node.Table.Name != "orders" {
		t.Fatalf("Table.Name = %q, want orders", node.Table.Name)
	}
	if node.Table.Comment != "order facts" {
		t.Fatalf("Table.Comment = %#v, want order facts", node.Table.Comment)
	}
	if len(node.Table.Fields) != 0 {
		t.Fatalf("entry Table.Fields = %#v, want empty", node.Table.Fields)
	}
	if len(node.Table.PrimaryKey) != 0 {
		t.Fatalf("entry Table.PrimaryKey = %#v, want empty", node.Table.PrimaryKey)
	}
	if node.UpdatedAt == nil || !node.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("UpdatedAt = %#v, want %v", node.UpdatedAt, updatedAt)
	}
	if node.Table.Native["engine"] != "MergeTree" {
		t.Fatalf("Table.Native = %#v, want copied engine", node.Table.Native)
	}
}

func TestTabularNamespaceCatalogEntryUsesLeafCount(t *testing.T) {
	root := CatalogRootPath(TabularCatalogModel(CatalogTermDatabase), 7)

	node := TabularNamespaceCatalogEntry(root, CatalogTermDatabase, "analytics", 3)

	if node.LeafCount == nil || *node.LeafCount != 3 {
		t.Fatalf("LeafCount = %#v, want 3", node.LeafCount)
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["leaf_count"]; !ok {
		t.Fatalf("payload = %s, want leaf_count", data)
	}
	if _, ok := payload["table_count"]; ok {
		t.Fatalf("payload = %s, must not contain table_count", data)
	}
}

func TestBuildTabularCatalogFactsCarriesTableInfo(t *testing.T) {
	rowCount := int64(42)
	updatedAt := time.Unix(300, 0)
	item := buildTabularCatalogFacts(CatalogPath{
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
	}, true, CatalogKindTable, nil)
	if item.Table == nil {
		t.Fatal("CatalogFacts.Table is nil")
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

func TestTabularCatalogEntryFromFactsCarriesTableInfo(t *testing.T) {
	rowCount := int64(10)
	facts := &CatalogFacts{
		Table: &datatype.TableInfo{
			Name:     "orders",
			Kind:     "view",
			Comment:  "orders",
			RowCount: &rowCount,
			Native:   map[string]interface{}{"engine": "MergeTree"},
		},
	}

	node := tabularCatalogEntryFromFacts(CatalogPath{Version: CatalogPathVersion}, "orders", "view", facts)

	if node.Kind != "view" || node.Role != CatalogRoleLeaf {
		t.Fatalf("node kind/role = %q/%q", node.Kind, node.Role)
	}
	if node.Table == nil || node.Table.RowCount == nil || *node.Table.RowCount != int64(10) {
		t.Fatalf("Table.RowCount = %#v", node.Table)
	}
	if node.Table == nil || node.Table.Comment != "orders" || node.Table.Native["engine"] != "MergeTree" {
		t.Fatalf("Table = %#v", node.Table)
	}
	if len(node.Table.Fields) != 0 {
		t.Fatalf("entry Table.Fields = %#v, want empty", node.Table.Fields)
	}
	if len(node.Table.PrimaryKey) != 0 {
		t.Fatalf("entry Table.PrimaryKey = %#v, want empty", node.Table.PrimaryKey)
	}
}

func TestDescribeTabularItemOnlyRunsRowCountWhenStatisticsRequested(t *testing.T) {
	rowCountCalls := 0
	callbacks := TabularCatalogCallbacks{
		ListNamespaces: func(context.Context, *gorm.DB, CatalogPath) ([]CatalogEntry, error) {
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
			{Term: CatalogTermServer, Kind: CatalogTermServer},
			{Term: CatalogTermDatabase, Kind: CatalogKindNamespace, Name: "analytics"},
			{Term: CatalogTermTable, Kind: CatalogKindTable, Name: "orders"},
		},
	}

	Register(&tabularCatalogTestPlugin{})
	t.Cleanup(func() {
		Unregister("tabular_catalog_test")
		ClosePool(engine.ID)
	})

	item, err := DescribeTabularCatalogFacts(context.Background(), callbacks, engine, path, CatalogFactsOptions{})
	if err != nil {
		t.Fatalf("DescribeTabularCatalogFacts() error = %v", err)
	}
	if rowCountCalls != 0 {
		t.Fatalf("row count calls = %d, want 0 without IncludeStatistics", rowCountCalls)
	}
	if item.Table.RowCount != nil {
		t.Fatalf("Table.RowCount = %#v, want nil without IncludeStatistics", item.Table.RowCount)
	}

	item, err = DescribeTabularCatalogFacts(context.Background(), callbacks, engine, path, CatalogFactsOptions{IncludeStatistics: true})
	if err != nil {
		t.Fatalf("DescribeTabularCatalogFacts(IncludeStatistics) error = %v", err)
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
