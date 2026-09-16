package postgresql

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestPostgreSQLPartitionedChangeApplyDeclaresUpsertDeleteAndSkip(t *testing.T) {
	capability := (&PostgreSQLPlugin{}).Capabilities().Storage.Store.PartitionedTableChangeApply
	want := []string{plugin.TableChangeOperationUpsert, plugin.TableChangeOperationDelete, plugin.TableChangeOperationSkip}
	if capability == nil || !reflect.DeepEqual(capability.Operations, want) {
		t.Fatalf("partitioned change apply operations = %#v, want %v", capability, want)
	}
}

func TestPostgreSQLDeclaresRelationQueryParameters(t *testing.T) {
	t.Parallel()

	types := (&PostgreSQLPlugin{}).Capabilities().Compute.Query.Parameters.Types
	if !plugin.Contains(types, "relation") {
		t.Fatalf("query parameter types = %v, want relation", types)
	}
}

func TestPostgreSQLPrepareQueryAcceptsBoundPositionalArguments(t *testing.T) {
	prepared, err := (&PostgreSQLPlugin{}).PrepareQuery(t.Context(), plugin.ConnectionInfo{}, plugin.QueryRequest{
		Language: "sql",
		Query:    `SELECT * FROM public.items WHERE city = $1`,
		Options: plugin.QueryOptions{
			ReadOnly: true,
			Args:     []interface{}{"长沙市"},
		},
	})
	if err != nil {
		t.Fatalf("PrepareQuery() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareQuery() returned nil plan")
	}
}

func TestPostgreSQLIsSystemSchema(t *testing.T) {
	plugin := &PostgreSQLPlugin{}

	for _, name := range []string{"pg_catalog", "information_schema", "pg_toast", "PG_TEMP_12", "pg_toast_temp_7"} {
		if !plugin.isSystemSchema(name) {
			t.Fatalf("isSystemSchema(%q) = false, want true", name)
		}
	}

	if plugin.isSystemSchema("public") {
		t.Fatal("isSystemSchema(\"public\") = true, want false")
	}
}

func TestProtocolCompatibleCatalogFiltersAdditionalSystemSchemasBeforeLeafCounts(t *testing.T) {
	t.Parallel()

	protocol := NewProtocolCompatiblePlugin(ProtocolIdentity{
		EngineType:              "opengauss",
		DisplayName:             "openGauss",
		AdditionalSystemSchemas: []string{" Coverage ", "DBE_PERF", "coverage"},
		AdditionalSystemTables:  []string{" SYS_STAT_STATEMENTS_ALL ", "sys_stat_statements", "sys_stat_statements"},
	})

	if !protocol.isSystemSchema("coverage") || !protocol.isSystemSchema("DBE_PERF") {
		t.Fatal("protocol-specific system schemas must be filtered")
	}
	if protocol.isSystemSchema("public") {
		t.Fatal("business schema public must remain visible")
	}

	query, args := protocol.listNamespacesQuery(false)
	if !strings.Contains(query, "WHERE lower(schema_name) NOT IN") {
		t.Fatalf("namespace query must filter schemas before evaluating leaf counts: %s", query)
	}
	if !strings.Contains(query, "AND lower(table_name) NOT IN (?, ?)") {
		t.Fatalf("namespace query must filter protocol-specific system tables from leaf counts: %s", query)
	}
	if !strings.Contains(query, "has_schema_privilege(s.schema_name, 'USAGE')") {
		t.Fatalf("namespace query must filter schemas by the current connection identity: %s", query)
	}
	wantArgs := []interface{}{"sys_stat_statements", "sys_stat_statements_all", "coverage", "dbe_perf", "information_schema", "pg_catalog", "pg_toast"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("namespace query args = %#v, want %#v", args, wantArgs)
	}
}

func TestFilterPostgreSQLSystemTablesRequiresDetectedSuperMapSDX(t *testing.T) {
	tables := []datatype.TableInfo{
		{Name: "roads"},
		{Name: "smdatasourceinfo"},
		{Name: "SMFIELDINFO"},
		{Name: "smdynamicindex"},
		{Name: "SMTILEINDEX"},
		{Name: "sm_business_table"},
	}

	postgres := &PostgreSQLPlugin{}
	withoutSDX := postgres.filterSystemTables(tables, false)
	if len(withoutSDX) != len(tables) {
		t.Fatalf("filterSystemTables without SDX returned %d tables, want %d", len(withoutSDX), len(tables))
	}

	withSDX := postgres.filterSystemTables(tables, true)
	if len(withSDX) != 2 || withSDX[0].Name != "roads" || withSDX[1].Name != "sm_business_table" {
		t.Fatalf("filterSystemTables with SDX = %#v, want roads and sm_business_table", withSDX)
	}
}

func TestProtocolCompatibleCatalogFiltersAndRejectsAdditionalSystemTables(t *testing.T) {
	t.Parallel()

	protocol := NewProtocolCompatiblePlugin(ProtocolIdentity{
		EngineType:             "kingbase",
		DisplayName:            "KingbaseES",
		AdditionalSystemTables: []string{"sys_stat_statements", "sys_stat_statements_all"},
	})
	tables := []datatype.TableInfo{{Name: "customers"}, {Name: "SYS_STAT_STATEMENTS"}, {Name: "sys_stat_statements_all"}}
	filtered := protocol.filterSystemTables(tables, false)
	if len(filtered) != 1 || filtered[0].Name != "customers" {
		t.Fatalf("filterSystemTables() = %#v, want customers", filtered)
	}

	hiddenPath := plugin.TabularItemPath(26, plugin.EngineCatalogTermSchema, "public", "sys_stat_statements")
	if _, err := protocol.ResolvePath(t.Context(), nil, hiddenPath); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("ResolvePath(hidden table) error = %v, want not_found", err)
	}
	if _, err := protocol.DescribeEngineCatalogFacts(t.Context(), nil, hiddenPath, plugin.EngineCatalogFactsOptions{}); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("DescribeEngineCatalogFacts(hidden table) error = %v, want not_found", err)
	}
}

func TestPostgresTableNativeKeepsSourceFactsOnly(t *testing.T) {
	native := postgresTableNative(" BASE TABLE ", " r ")

	if native["table_type"] != "BASE TABLE" || native["relkind"] != "r" {
		t.Fatalf("postgresTableNative() = %#v, want table_type and relkind", native)
	}
	if native["kind"] != nil {
		t.Fatalf("postgresTableNative() should not include platform kind: %#v", native)
	}
}

func TestPostgresTableNativeReturnsNilForEmptyFacts(t *testing.T) {
	if got := postgresTableNative("", " "); got != nil {
		t.Fatalf("postgresTableNative() = %#v, want nil", got)
	}
}
