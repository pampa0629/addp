package plugin

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestCatalogFactsTableInfoReturnsClone(t *testing.T) {
	facts := &EngineCatalogFacts{
		Table: &datatype.TableInfo{
			Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
		},
	}

	info := EngineCatalogFactsTableInfo(facts)
	if info == nil || len(info.Fields) != 1 || info.Fields[0].Name != "id" {
		t.Fatalf("EngineCatalogFactsTableInfo() = %#v", info)
	}
	info.Fields[0].Name = "changed"
	if facts.Table.Fields[0].Name != "id" {
		t.Fatalf("EngineCatalogFactsTableInfo returned mutable table fields")
	}
}

func TestCatalogEntryTableInfoOmitsDetailFields(t *testing.T) {
	facts := &EngineCatalogFacts{
		Table: &datatype.TableInfo{
			Name:       "orders",
			Fields:     []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
			PrimaryKey: []string{"id"},
			Native:     map[string]interface{}{"engine": "MergeTree"},
		},
	}

	info := EngineCatalogEntryTableInfo(facts)
	if info == nil || info.Name != "orders" || info.Native["engine"] != "MergeTree" {
		t.Fatalf("EngineCatalogEntryTableInfo() = %#v", info)
	}
	if len(info.Fields) != 0 {
		t.Fatalf("entry table fields = %#v, want empty", info.Fields)
	}
	if len(info.PrimaryKey) != 0 {
		t.Fatalf("entry table primary key = %#v, want empty", info.PrimaryKey)
	}

	info.Native["engine"] = "Log"
	if facts.Table.Native["engine"] != "MergeTree" {
		t.Fatalf("EngineCatalogEntryTableInfo returned mutable native map")
	}
}

func TestCatalogEntryStorageInfoOmitsDetailFields(t *testing.T) {
	size := int64(128)
	facts := &EngineCatalogFacts{
		Storage: &EngineCatalogStorageFacts{
			Name:        "orders.csv",
			Path:        "datasets/orders.csv",
			ContentType: "text/csv",
			ETag:        "etag-1",
			Extension:   ".csv",
			SizeBytes:   &size,
		},
	}

	info := EngineCatalogEntryStorageInfo(facts)
	if info == nil || info.Path != "datasets/orders.csv" || info.ContentType != "text/csv" || info.ETag != "etag-1" {
		t.Fatalf("EngineCatalogEntryStorageInfo() = %#v", info)
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
		t.Fatalf("EngineCatalogEntryStorageInfo returned mutable size pointer")
	}
}

func TestCatalogFactsGraphInfoReturnsClone(t *testing.T) {
	count := int64(3)
	facts := &EngineCatalogFacts{
		Kind: "graph",
		Graph: &datatype.GraphInfo{
			NodeShapes: []datatype.GraphNodeShapeInfo{{
				Name:  "Person",
				Count: &count,
			}},
		},
	}

	info := EngineCatalogFactsGraphInfo(facts)
	if info == nil || len(info.NodeShapes) != 1 || info.NodeShapes[0].Name != "Person" {
		t.Fatalf("EngineCatalogFactsGraphInfo() = %#v", info)
	}
	info.NodeShapes[0].Name = "Changed"
	if facts.Graph.NodeShapes[0].Name != "Person" {
		t.Fatalf("EngineCatalogFactsGraphInfo returned mutable graph info")
	}
}

func TestCatalogFactsSpatialInfoReturnsClone(t *testing.T) {
	srid := 4326
	facts := &EngineCatalogFacts{
		Spatial: &datatype.SpatialInfo{
			GeometryColumns: []datatype.GeometryColumnInfo{{
				Name: "geom",
				SRID: &srid,
			}},
			PrimaryGeometryColumn: "geom",
		},
	}

	info := EngineCatalogFactsSpatialInfo(facts)
	if info == nil || info.PrimaryGeometryColumn != "geom" || len(info.GeometryColumns) != 1 {
		t.Fatalf("EngineCatalogFactsSpatialInfo() = %#v", info)
	}
	*info.GeometryColumns[0].SRID = 3857
	if *facts.Spatial.GeometryColumns[0].SRID != 4326 {
		t.Fatalf("EngineCatalogFactsSpatialInfo returned mutable spatial info")
	}
}
