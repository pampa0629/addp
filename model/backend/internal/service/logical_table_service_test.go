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
