package metaattr

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
)

func TestNormalizeGeometryType(t *testing.T) {
	t.Parallel()

	if got := NormalizeGeometryType("ST_MultiPolygon"); got != "MultiPolygon" {
		t.Fatalf("NormalizeGeometryType() = %q", got)
	}
	if got := NormalizeGeometryType("geometrycollection"); got != "GeometryCollection" {
		t.Fatalf("NormalizeGeometryType() = %q", got)
	}
	if got := NormalizeGeometryType("not-a-geometry"); got != "Geometry" {
		t.Fatalf("NormalizeGeometryType() = %q", got)
	}
}

func TestSetTableFieldsWritesDatatypeFieldFacts(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	SetTableFields(attrs, []datatype.FieldInfo{{
		Name:              "id",
		Type:              datatype.FieldTypeInt,
		NativeType:        "int4",
		Nullable:          false,
		PrimaryKey:        true,
		Comment:           "identifier",
		OrdinalPosition:   1,
		DefaultExpression: "0",
	}})

	table := attrs["type_info"].(map[string]interface{})["table"].(map[string]interface{})
	fields := table["fields"].([]interface{})
	if len(fields) != 1 {
		t.Fatalf("fields = %#v, want one field", fields)
	}
	field := fields[0].(map[string]interface{})
	if field["name"] != "id" || field["type"] != "int" || field["native_type"] != "int4" {
		t.Fatalf("field identity = %#v", field)
	}
	if field["primary_key"] != true || field["nullable"] != false || field["comment"] != "identifier" {
		t.Fatalf("field facts = %#v", field)
	}
	if field["ordinal_position"] != 1 || field["default_expression"] != "0" {
		t.Fatalf("field extended facts = %#v", field)
	}
	if field["is_primary_key"] != nil || field["is_nullable"] != nil || field["is_spatial"] != nil || field["geometry_type"] != nil {
		t.Fatalf("legacy or cross-cutting field attrs should not be written: %#v", field)
	}
}

func TestBuildDynamicSchemaAttributesWritesTypeInfoTableSection(t *testing.T) {
	t.Parallel()

	attrs := BuildDynamicSchemaAttributes(DynamicSchemaAttributesInput{
		Fields: []datatype.FieldInfo{{
			Name: "name",
			Type: datatype.FieldTypeString,
		}},
		Indexes: []IndexAttributesInput{{
			Name:      "name_idx",
			Fields:    []string{"name"},
			IndexType: "btree",
		}},
		Attributes: map[string]interface{}{
			"database":        "db1",
			"collection":      "people",
			"is_sampled":      true,
			"sample_size":     10,
			"schema_type":     "dynamic",
			"total_documents": int64(12),
			"index_count":     int64(1),
			"avg_record_size": int64(256),
		},
	})

	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	capabilities := attrs["capabilities"].(map[string]interface{})
	statistics := capabilities["statistics"].(map[string]interface{})
	indexing := capabilities["indexing"].(map[string]interface{})
	if typeInfo["document"] != nil || typeInfo["collection"] != nil {
		t.Fatalf("dynamic schema item must only write table type_info: %#v", typeInfo)
	}
	if _, ok := table["fields"]; !ok {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
	if table["indexes"] != nil || table["is_sampled"] != nil || table["schema_type"] != nil {
		t.Fatalf("dynamic schema sampling/indexing facts should not be in type_info.table: %#v", table)
	}
	if _, ok := indexing["indexes"]; !ok {
		t.Fatalf("capabilities.indexing.indexes missing: %#v", indexing)
	}
	if table["row_count"] != int64(12) {
		t.Fatalf("type_info.table.row_count missing: %#v", table)
	}
	if statistics["sample_size"] != 10 || statistics["index_count"] != int64(1) || statistics["avg_record_size"] != int64(256) ||
		statistics["is_sampled"] != true || statistics["schema_type"] != "dynamic" {
		t.Fatalf("capabilities.statistics missing: %#v", statistics)
	}
	if attrs["fields"] != nil || attrs["indexes"] != nil || attrs["schema"] != nil || attrs["database"] != nil || attrs["collection"] != nil {
		t.Fatalf("legacy flat/schema fields should not be written: %#v", attrs)
	}
}

func TestUpsertTableNativeWritesTypeInfoTableNative(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{
		"type_info": map[string]interface{}{
			"table": datatype.TableInfoPayload(&datatype.TableInfo{
				Kind:    "table",
				Comment: "roads",
			}),
		},
	}
	native := map[string]interface{}{"engine": "MergeTree"}

	UpsertTableNative(attrs, native)
	native["engine"] = "Log"

	table := attrs["type_info"].(map[string]interface{})["table"].(map[string]interface{})
	got := table["native"].(map[string]interface{})
	if got["engine"] != "MergeTree" {
		t.Fatalf("type_info.table.native = %#v", got)
	}
	if table["kind"] != "table" || table["comment"] != "roads" {
		t.Fatalf("type_info.table standard attrs = %#v", table)
	}
}

func TestTableInfoPayloadWritesStandardTableFacts(t *testing.T) {
	t.Parallel()

	rowCount := int64(7)
	sizeBytes := int64(128)
	info := &datatype.TableInfo{
		Name:      "orders",
		Kind:      "view",
		Comment:   "order view",
		RowCount:  &rowCount,
		SizeBytes: &sizeBytes,
		Fields: []datatype.FieldInfo{{
			Name:       "id",
			Type:       datatype.FieldTypeInt,
			NativeType: "int4",
		}},
		PrimaryKey: []string{"id"},
		Native:     map[string]interface{}{"relkind": "v"},
	}

	attrs := datatype.TableInfoPayload(info)
	info.Native["relkind"] = "r"

	if attrs["name"] != "orders" || attrs["kind"] != "view" || attrs["comment"] != "order view" {
		t.Fatalf("table identity attrs = %#v", attrs)
	}
	if attrs["row_count"] != int64(7) || attrs["size_bytes"] != int64(128) {
		t.Fatalf("table count attrs = %#v", attrs)
	}
	native := attrs["native"].(map[string]interface{})
	if native["relkind"] != "v" {
		t.Fatalf("native = %#v, want cloned relkind", native)
	}
}

func TestApplyBranchLeafItemAttributesDoesNotWriteEngineFormat(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	ApplyBranchLeafItemAttributes(attrs, "collection")

	item := attrs["item"].(map[string]interface{})
	if item["layout"] != "single" || item["data_type"] != "table" {
		t.Fatalf("item attrs = %#v", item)
	}
	if item["format"] != nil {
		t.Fatalf("native branch leaf should not write item.format: %#v", item)
	}
}

func TestApplyDynamicSchemaStatisticsWritesStandardSections(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	ApplyDynamicSchemaStatistics(attrs, 42, 2048)

	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	storage := attrs["storage"].(map[string]interface{})
	if table["row_count"] != int64(42) {
		t.Fatalf("dynamic schema counts not standardized: %#v", attrs)
	}
	if typeInfo["document"] != nil || typeInfo["collection"] != nil {
		t.Fatalf("dynamic schema count should not be duplicated outside type_info.table: %#v", typeInfo)
	}
	if storage["total_size"] != int64(2048) {
		t.Fatalf("storage.total_size missing: %#v", storage)
	}
}

func TestApplyGraphItemAttributesWritesTypeInfoGraph(t *testing.T) {
	t.Parallel()

	nodeCount := int64(3)
	relationshipCount := int64(7)
	attrs := models.JSONMap{}
	ApplyGraphItemAttributes(attrs, &datatype.GraphInfo{
		Model:             datatype.GraphModelPropertyGraph,
		NodeCount:         &nodeCount,
		RelationshipCount: &relationshipCount,
		NodeShapes: []datatype.GraphNodeShapeInfo{{
			Name:   "Person",
			Kind:   datatype.GraphNodeShapeKindLabel,
			Labels: []string{"Person"},
			Count:  &nodeCount,
		}},
		RelationshipShapes: []datatype.GraphRelationshipShapeInfo{{
			Type: "WORKS_FOR",
			Patterns: []datatype.GraphRelationshipPatternInfo{{
				From:  datatype.GraphEndpointInfo{ShapeName: "Person", Labels: []string{"Person"}},
				To:    datatype.GraphEndpointInfo{ShapeName: "Company", Labels: []string{"Company"}},
				Count: &relationshipCount,
			}},
			Count: &relationshipCount,
		}},
	})

	graph := attrs["type_info"].(map[string]interface{})["graph"].(map[string]interface{})
	if graph["model"] != datatype.GraphModelPropertyGraph || graph["relationship_count"] != int64(7) {
		t.Fatalf("type_info.graph relationship attrs missing: %#v", graph)
	}
	if graph["edge_count"] != nil || graph["from_labels"] != nil || graph["to_labels"] != nil {
		t.Fatalf("legacy graph attrs should not be written: %#v", graph)
	}
	item := attrs["item"].(map[string]interface{})
	if item["data_type"] != "graph" || item["layout"] != "single" {
		t.Fatalf("graph item attrs missing: %#v", item)
	}
}
