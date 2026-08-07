package postgresql

import (
	"reflect"
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

func TestFilterPostgreSQLSystemTablesRequiresDetectedSuperMapSDX(t *testing.T) {
	tables := []datatype.TableInfo{
		{Name: "roads"},
		{Name: "smdatasourceinfo"},
		{Name: "SMFIELDINFO"},
		{Name: "smdynamicindex"},
		{Name: "SMTILEINDEX"},
		{Name: "sm_business_table"},
	}

	withoutSDX := filterPostgreSQLSystemTables(tables, false)
	if len(withoutSDX) != len(tables) {
		t.Fatalf("filterPostgreSQLSystemTables without SDX returned %d tables, want %d", len(withoutSDX), len(tables))
	}

	withSDX := filterPostgreSQLSystemTables(tables, true)
	if len(withSDX) != 2 || withSDX[0].Name != "roads" || withSDX[1].Name != "sm_business_table" {
		t.Fatalf("filterPostgreSQLSystemTables with SDX = %#v, want roads and sm_business_table", withSDX)
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
