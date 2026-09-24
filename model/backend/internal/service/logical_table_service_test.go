package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/model/internal/models"
)

func testPhysicalTargetSystemClient(t *testing.T, engineType string) *commonClient.SystemServiceClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/runtime/engine-descriptors/2" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(commonModels.EngineRuntimeDescriptor{ID: 2, EngineType: engineType})
	}))
	t.Cleanup(server.Close)
	return commonClient.NewSystemServiceClient(server.URL, materializationTestTokens{}, server.Client())
}

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

func TestValidateMaterializationRejectsPartitionOptions(t *testing.T) {
	table := &models.LogicalTable{Code: "fact_order", Materialization: models.JSONB{"partition_by": "order_id", "partition_type": "range"}}
	fields := []models.LogicalField{{ColumnName: "order_id", DataType: "bigint"}}
	if err := validateMaterialization(table, fields); err == nil {
		t.Fatal("expected unsupported partition options error")
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

func TestValidateMaterializationRejectsNonNamespaceTargetParent(t *testing.T) {
	table := &models.LogicalTable{Code: "fact_order", Materialization: models.JSONB{
		"target_parent_locator": "addp://engine/2/path/analytics/orders?type=table",
		"target_name":           "fact_order",
	}}
	fields := []models.LogicalField{{ColumnName: "order_id", DataType: "bigint"}}
	if err := validateMaterialization(table, fields); err == nil {
		t.Fatal("expected non-namespace target parent error")
	}
}

func TestValidateMaterializationAcceptsDatabaseNamespaceAndRejectsNestedParent(t *testing.T) {
	table := &models.LogicalTable{Code: "fact_order", Materialization: models.JSONB{
		"target_parent_locator": "addp://engine/2/path/metric_fixture?type=database",
		"target_name":           "fact_order",
	}}
	if err := validateMaterialization(table, nil); err != nil {
		t.Fatalf("database namespace rejected: %v", err)
	}
	table.Materialization["target_parent_locator"] = "addp://engine/2/path/metric_fixture/nested?type=database"
	if err := validateMaterialization(table, nil); err == nil {
		t.Fatal("nested namespace accepted")
	}
}

func TestPhysicalTargetCatalogMatchesEngine(t *testing.T) {
	for _, tc := range []struct {
		engineType, parentType, namespace, tableName string
		valid                                        bool
	}{
		{"postgresql", "schema", "metric_fixture", "fact_order", true},
		{"postgresql", "schema", "SalesSchema", "OrderDetail", true},
		{"postgresql", "database", "metric_fixture", "fact_order", false},
		{"tidb", "database", "metric_fixture", "fact_order", true},
		{"tidb", "database", "SalesDB", "OrderDetail", true},
		{"tidb", "schema", "metric_fixture", "fact_order", false},
		{"mysql", "database", "metric_fixture", "fact_order", true},
	} {
		t.Run(tc.engineType+"/"+tc.parentType+"/"+tc.namespace, func(t *testing.T) {
			svc := &LogicalTableService{system: testPhysicalTargetSystemClient(t, tc.engineType)}
			config := models.JSONB{"target_parent_locator": "addp://engine/2/path/" + tc.namespace + "?type=" + tc.parentType, "target_name": tc.tableName}
			err := svc.validateMaterializationCatalog(1, config)
			if (err == nil) != tc.valid {
				t.Fatalf("validate catalog = %v, want valid %v", err, tc.valid)
			}
		})
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

func TestNormalizeMaterializationTrimsTargetFieldsWithoutDroppingUnknownKeys(t *testing.T) {
	input := map[string]interface{}{
		"target_parent_locator": " addp://engine/2/path/public?type=schema ",
		"target_name":           " fact_order ",
		"unsupported":           " preserved for validation ",
	}
	normalized := normalizeMaterialization(input)
	if normalized["target_parent_locator"] != "addp://engine/2/path/public?type=schema" || normalized["target_name"] != "fact_order" {
		t.Fatalf("target fields were not normalized: %#v", normalized)
	}
	if normalized["unsupported"] != " preserved for validation " {
		t.Fatalf("unknown key must remain available to validation: %#v", normalized)
	}
	if input["target_name"] != " fact_order " {
		t.Fatalf("input was mutated: %#v", input)
	}
}

func TestNormalizeMaterializationCollapsesEmptyTargetToEmptyObject(t *testing.T) {
	normalized := normalizeMaterialization(map[string]interface{}{
		"target_parent_locator": "   ",
		"target_name":           "",
	})
	if len(normalized) != 0 {
		t.Fatalf("empty materialization must be canonicalized to an empty object: %#v", normalized)
	}
}
