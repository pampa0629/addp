package plugin

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestCatalogFactsTableInfoReturnsClone(t *testing.T) {
	facts := &CatalogFacts{
		Table: &datatype.TableInfo{
			Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
		},
	}

	info := CatalogFactsTableInfo(facts)
	if info == nil || len(info.Fields) != 1 || info.Fields[0].Name != "id" {
		t.Fatalf("CatalogFactsTableInfo() = %#v", info)
	}
	info.Fields[0].Name = "changed"
	if facts.Table.Fields[0].Name != "id" {
		t.Fatalf("CatalogFactsTableInfo returned mutable table fields")
	}
}

func TestCatalogEntryTableInfoOmitsDetailFields(t *testing.T) {
	facts := &CatalogFacts{
		Table: &datatype.TableInfo{
			Name:       "orders",
			Fields:     []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
			PrimaryKey: []string{"id"},
			Native:     map[string]interface{}{"engine": "MergeTree"},
		},
	}

	info := CatalogEntryTableInfo(facts)
	if info == nil || info.Name != "orders" || info.Native["engine"] != "MergeTree" {
		t.Fatalf("CatalogEntryTableInfo() = %#v", info)
	}
	if len(info.Fields) != 0 {
		t.Fatalf("entry table fields = %#v, want empty", info.Fields)
	}
	if len(info.PrimaryKey) != 0 {
		t.Fatalf("entry table primary key = %#v, want empty", info.PrimaryKey)
	}

	info.Native["engine"] = "Log"
	if facts.Table.Native["engine"] != "MergeTree" {
		t.Fatalf("CatalogEntryTableInfo returned mutable native map")
	}
}

func TestCatalogEntryStorageInfoOmitsDetailFields(t *testing.T) {
	size := int64(128)
	facts := &CatalogFacts{
		Storage: &CatalogStorageFacts{
			Name:        "orders.csv",
			Path:        "datasets/orders.csv",
			ContentType: "text/csv",
			ETag:        "etag-1",
			Extension:   ".csv",
			SizeBytes:   &size,
		},
	}

	info := CatalogEntryStorageInfo(facts)
	if info == nil || info.Path != "datasets/orders.csv" || info.ContentType != "text/csv" || info.ETag != "etag-1" {
		t.Fatalf("CatalogEntryStorageInfo() = %#v", info)
	}
	if info.Name != "" {
		t.Fatalf("entry storage name = %q, want empty", info.Name)
	}
	if info.Extension != "" {
		t.Fatalf("entry storage extension = %q, want empty", info.Extension)
	}
	if info.SizeBytes == nil || *info.SizeBytes != 128 {
		t.Fatalf("entry storage size = %#v, want 128", info.SizeBytes)
	}

	*info.SizeBytes = 256
	if *facts.Storage.SizeBytes != 128 {
		t.Fatalf("CatalogEntryStorageInfo returned mutable size pointer")
	}
}

func TestCatalogFactsGraphInfoReturnsClone(t *testing.T) {
	count := int64(3)
	facts := &CatalogFacts{
		Kind: "graph",
		Graph: &datatype.GraphInfo{
			NodeShapes: []datatype.GraphNodeShapeInfo{{
				Name:  "Person",
				Count: &count,
			}},
		},
	}

	info := CatalogFactsGraphInfo(facts)
	if info == nil || len(info.NodeShapes) != 1 || info.NodeShapes[0].Name != "Person" {
		t.Fatalf("CatalogFactsGraphInfo() = %#v", info)
	}
	info.NodeShapes[0].Name = "Changed"
	if facts.Graph.NodeShapes[0].Name != "Person" {
		t.Fatalf("CatalogFactsGraphInfo returned mutable graph info")
	}
}
