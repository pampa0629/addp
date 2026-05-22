package metaattr

import (
	"github.com/addp/common/datatype"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/models"
)

func TestSpatialMetadataWritesMinimalCapabilitiesSpatial(t *testing.T) {
	t.Parallel()

	values := SpatialCapabilityFromMetadata(&models.SpatialMetadata{
		GeometryColumn:  "geom",
		SRID:            4326,
		ExtentSRID:      4326,
		Extent:          []float64{1, 2, 3, 4},
		GeometryTypes:   []string{"ST_MultiPolygon"},
		HasSpatialIndex: true,
	})

	if values["spatial_metadata"] != nil || values["geometry_types"] != nil {
		t.Fatalf("legacy spatial metadata should not be written: %#v", values)
	}
	if values["primary_geometry_column"] != "geom" || values["has_spatial_index"] != true {
		t.Fatalf("capabilities.spatial = %#v", values)
	}
	columns := values["geometry_columns"].([]map[string]interface{})
	if len(columns) != 1 {
		t.Fatalf("geometry_columns = %#v", columns)
	}
	if columns[0]["name"] != "geom" || columns[0]["geometry_type"] != "MultiPolygon" || columns[0]["srid"] != 4326 {
		t.Fatalf("geometry column = %#v", columns[0])
	}
}

func TestFieldAttributesFromFormatWritesSpatialFieldFacts(t *testing.T) {
	t.Parallel()

	fields := FieldAttributesFromFormat([]format.FieldInfo{{
		Name:     "SmGeometry",
		Type:     datatype.FieldTypeGeometry,
		Nullable: true,
	}})

	if len(fields) != 1 {
		t.Fatalf("fields = %#v, want one field", fields)
	}
	field := fields[0]
	if field["type"] != "geometry" || field["is_spatial"] != true || field["geometry_type"] != "Geometry" {
		t.Fatalf("spatial field attributes = %#v", field)
	}
}

func TestFieldAttributesFromDatatypeWritesStandardFieldFacts(t *testing.T) {
	t.Parallel()

	fields := FieldAttributesFromDatatype([]datatype.FieldInfo{{
		Name:       "id",
		Type:       datatype.FieldTypeInt,
		NativeType: "int4",
		Nullable:   false,
		PrimaryKey: true,
		Comment:    "identifier",
	}})

	if len(fields) != 1 {
		t.Fatalf("fields = %#v, want one field", fields)
	}
	field := fields[0]
	if field["name"] != "id" || field["type"] != "int" || field["native_type"] != "int4" {
		t.Fatalf("field identity = %#v", field)
	}
	if field["is_primary_key"] != true || field["nullable"] != false || field["comment"] != "identifier" {
		t.Fatalf("field facts = %#v", field)
	}
}

func TestBuildDocumentCollectionAttributesWritesTypeInfoTableSection(t *testing.T) {
	t.Parallel()

	attrs := BuildDocumentCollectionAttributes(&plugin.ItemMetadata{
		Fields: []datatype.FieldInfo{{
			Name: "name",
			Type: datatype.FieldTypeString,
		}},
		Indexes: []plugin.IndexInfo{{
			Name:      "name_idx",
			Fields:    []string{"name"},
			IndexType: "btree",
		}},
		Stats: map[string]interface{}{
			"document_count": int64(12),
			"index_count":    int64(1),
			"avg_doc_size":   int64(256),
		},
		Attributes: map[string]interface{}{
			"database":        "db1",
			"collection":      "people",
			"is_sampled":      true,
			"sample_size":     10,
			"schema_type":     "dynamic",
			"total_documents": int64(12),
		},
	})

	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	capabilities := attrs["capabilities"].(map[string]interface{})
	statistics := capabilities["statistics"].(map[string]interface{})
	if _, ok := table["fields"]; !ok {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
	if _, ok := table["indexes"]; !ok {
		t.Fatalf("type_info.table.indexes missing: %#v", table)
	}
	if table["is_sampled"] != true || table["schema_type"] != "dynamic" {
		t.Fatalf("type_info.table sampling attrs missing: %#v", table)
	}
	if table["row_count"] != int64(12) {
		t.Fatalf("type_info.table.row_count missing: %#v", table)
	}
	if statistics["sample_size"] != 10 || statistics["index_count"] != int64(1) || statistics["avg_doc_size"] != int64(256) {
		t.Fatalf("capabilities.statistics missing: %#v", statistics)
	}
	if attrs["fields"] != nil || attrs["indexes"] != nil || attrs["schema"] != nil || attrs["database"] != nil || attrs["collection"] != nil {
		t.Fatalf("legacy flat/schema fields should not be written: %#v", attrs)
	}
}

func TestApplyNamespaceItemAttributesDoesNotWriteEngineFormat(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	ApplyNamespaceItemAttributes(attrs, "collection")

	item := attrs["item"].(map[string]interface{})
	if item["layout"] != "single" || item["data_type"] != "table" {
		t.Fatalf("item attrs = %#v", item)
	}
	if item["format"] != nil {
		t.Fatalf("native namespace item should not write item.format: %#v", item)
	}
}

func TestApplyDocumentCollectionStatisticsWritesStandardSections(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	ApplyDocumentCollectionStatistics(attrs, 42, 2048)

	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	storage := attrs["storage"].(map[string]interface{})
	if table["row_count"] != int64(42) {
		t.Fatalf("document collection counts not standardized: %#v", attrs)
	}
	if typeInfo["document"] != nil {
		t.Fatalf("document collection count should not be duplicated in type_info.document: %#v", typeInfo)
	}
	if storage["total_size"] != int64(2048) {
		t.Fatalf("storage.total_size missing: %#v", storage)
	}
}

func TestApplyGraphItemAttributesWritesTypeInfoGraph(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	ApplyGraphItemAttributes(attrs, "relationship", 7, map[string]interface{}{
		"from_labels": []string{"Person"},
		"to_labels":   []interface{}{"Company"},
	})

	graph := attrs["type_info"].(map[string]interface{})["graph"].(map[string]interface{})
	if graph["edge_count"] != int64(7) || graph["relationship"] != true {
		t.Fatalf("type_info.graph relationship attrs missing: %#v", graph)
	}
	fromLabels := graph["from_labels"].([]string)
	toLabels := graph["to_labels"].([]string)
	if len(fromLabels) != 1 || fromLabels[0] != "Person" || len(toLabels) != 1 || toLabels[0] != "Company" {
		t.Fatalf("graph labels not preserved: %#v", graph)
	}
	if attrs["count"] != nil || attrs["from_labels"] != nil || attrs["to_labels"] != nil {
		t.Fatalf("legacy graph attrs should not be written: %#v", attrs)
	}
}
