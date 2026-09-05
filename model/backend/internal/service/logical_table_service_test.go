package service

import (
	"strings"
	"testing"

	"github.com/addp/model/internal/models"
)

func TestGeneratePostgreSQLDDLQuotesIdentifiers(t *testing.T) {
	service := &LogicalTableService{}
	table := &models.LogicalTable{
		Code: "fact_order",
		Materialization: models.JSONB{
			"target_parent_locator": "addp://engine/2/path/analytics?type=schema",
			"target_name":           "fact_order",
		},
	}
	fields := []models.LogicalField{{ColumnName: "order_id", DataType: "bigint", IsPK: true, Nullable: false}}
	if err := validateMaterialization(table, fields); err != nil {
		t.Fatalf("validate materialization: %v", err)
	}
	ddl := service.generatePostgreSQLDDL(table, fields)
	if !strings.Contains(ddl, `CREATE TABLE "analytics"."fact_order"`) || !strings.Contains(ddl, `"order_id" BIGINT`) {
		t.Fatalf("identifiers were not quoted: %s", ddl)
	}
}

func TestGeneratePostgreSQLDDLWithoutTargetUsesUnqualifiedLogicalCode(t *testing.T) {
	service := &LogicalTableService{}
	table := &models.LogicalTable{Code: "fact_order", Materialization: models.JSONB{}}
	fields := []models.LogicalField{{ColumnName: "order_id", DataType: "bigint"}}

	if err := validateMaterialization(table, fields); err != nil {
		t.Fatalf("validate materialization: %v", err)
	}
	ddl := service.generatePostgreSQLDDL(table, fields)
	if !strings.HasPrefix(ddl, `CREATE TABLE "fact_order" (`) {
		t.Fatalf("expected unqualified logical table name: %s", ddl)
	}
}

func TestValidateMaterializationRejectsArbitrarySQLField(t *testing.T) {
	table := &models.LogicalTable{Code: "fact_order", Materialization: models.JSONB{"extra_options": "DROP TABLE users"}}
	fields := []models.LogicalField{{ColumnName: "order_id", DataType: "bigint"}}
	if err := validateMaterialization(table, fields); err == nil {
		t.Fatal("expected arbitrary materialization field error")
	}
}

func TestValidateMaterializationRejectsUnknownPartitionField(t *testing.T) {
	table := &models.LogicalTable{Code: "fact_order", Materialization: models.JSONB{"partition_by": "missing", "partition_type": "range"}}
	fields := []models.LogicalField{{ColumnName: "order_id", DataType: "bigint"}}
	if err := validateMaterialization(table, fields); err == nil {
		t.Fatal("expected unknown partition field error")
	}
}

func TestValidateMaterializationRejectsLegacyTargetFields(t *testing.T) {
	table := &models.LogicalTable{Code: "fact_order", Materialization: models.JSONB{
		"schema_name": "analytics", "table_name": "fact_order",
	}}
	fields := []models.LogicalField{{ColumnName: "order_id", DataType: "bigint"}}
	if err := validateMaterialization(table, fields); err == nil {
		t.Fatal("expected legacy materialization fields error")
	}
}

func TestValidateMaterializationRejectsNonSchemaTargetParent(t *testing.T) {
	table := &models.LogicalTable{Code: "fact_order", Materialization: models.JSONB{
		"target_parent_locator": "addp://engine/2/path/analytics/orders?type=table",
		"target_name":           "fact_order",
	}}
	fields := []models.LogicalField{{ColumnName: "order_id", DataType: "bigint"}}
	if err := validateMaterialization(table, fields); err == nil {
		t.Fatal("expected non-schema target parent error")
	}
}

func TestValidateMaterializationRequiresCompleteTarget(t *testing.T) {
	fields := []models.LogicalField{{ColumnName: "order_id", DataType: "bigint"}}
	for _, config := range []models.JSONB{
		{"target_parent_locator": "addp://engine/2/path/analytics?type=schema"},
		{"target_name": "fact_order"},
	} {
		table := &models.LogicalTable{Code: "fact_order", Materialization: config}
		if err := validateMaterialization(table, fields); err == nil {
			t.Fatalf("expected incomplete target error for %#v", config)
		}
	}
}

func TestPreviewMaterializationDoesNotMutateStoredTable(t *testing.T) {
	stored := &models.LogicalTable{
		Code: "fact_order",
		Materialization: models.JSONB{
			"target_parent_locator": "addp://engine/2/path/public?type=schema",
			"target_name":           "stored_order",
		},
	}
	preview := previewLogicalTableWithMaterialization(stored, map[string]interface{}{
		"target_parent_locator": "addp://engine/2/path/analytics?type=schema",
		"target_name":           "preview_order",
	})

	if preview.Materialization["target_name"] != "preview_order" {
		t.Fatalf("preview materialization = %#v", preview.Materialization)
	}
	if stored.Materialization["target_name"] != "stored_order" {
		t.Fatalf("stored materialization mutated: %#v", stored.Materialization)
	}
}

func TestNormalizeMaterializationOmitsEmptyPartitionDesign(t *testing.T) {
	input := map[string]interface{}{
		"target_parent_locator": " addp://engine/2/path/public?type=schema ",
		"target_name":           " fact_order ",
		"partition_by":          " ",
		"partition_type":        "range",
	}
	normalized := normalizeMaterialization(input)
	if _, exists := normalized["partition_by"]; exists {
		t.Fatalf("partition_by must be omitted: %#v", normalized)
	}
	if _, exists := normalized["partition_type"]; exists {
		t.Fatalf("partition_type must be omitted: %#v", normalized)
	}
	if normalized["target_parent_locator"] != "addp://engine/2/path/public?type=schema" || normalized["target_name"] != "fact_order" {
		t.Fatalf("target fields were not normalized: %#v", normalized)
	}
	if input["partition_by"] != " " {
		t.Fatalf("input was mutated: %#v", input)
	}
}

func TestNormalizeMaterializationCollapsesEmptyTargetToEmptyObject(t *testing.T) {
	normalized := normalizeMaterialization(map[string]interface{}{
		"target_parent_locator": "   ",
		"target_name":           "",
		"partition_by":          "",
		"partition_type":        "range",
	})
	if len(normalized) != 0 {
		t.Fatalf("empty materialization must be canonicalized to an empty object: %#v", normalized)
	}
}

func TestNormalizeMaterializationKeepsExplicitPartitionDesign(t *testing.T) {
	normalized := normalizeMaterialization(map[string]interface{}{
		"partition_by": " occurred_at ", "partition_type": "RANGE",
	})
	if normalized["partition_by"] != "occurred_at" || normalized["partition_type"] != "range" {
		t.Fatalf("partition design = %#v", normalized)
	}
}
