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
			"schema_name": "analytics", "table_name": "fact_order",
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

func TestPreviewMaterializationDoesNotMutateStoredTable(t *testing.T) {
	stored := &models.LogicalTable{
		Code:            "fact_order",
		Materialization: models.JSONB{"schema_name": "public", "table_name": "stored_order"},
	}
	preview := previewLogicalTableWithMaterialization(stored, map[string]interface{}{
		"schema_name": "analytics", "table_name": "preview_order",
	})

	if preview.Materialization["table_name"] != "preview_order" {
		t.Fatalf("preview materialization = %#v", preview.Materialization)
	}
	if stored.Materialization["table_name"] != "stored_order" {
		t.Fatalf("stored materialization mutated: %#v", stored.Materialization)
	}
}
