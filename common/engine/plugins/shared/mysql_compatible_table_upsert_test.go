package shared

import (
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestMySQLCompatibleTableWriterValidatesUpsertKeys(t *testing.T) {
	writer := MySQLCompatibleTableWriter{EngineType: "oceanbase", EngineName: "OceanBase", DefaultPort: 2881}
	keys, err := writer.validateTableUpsertOptions(plugin.TableUpsertOptions{
		Fields: []datatype.FieldInfo{
			{Name: "tenant_id", Type: datatype.FieldTypeBigInt},
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "name", Type: datatype.FieldTypeString},
		},
		Keys: []string{"tenant_id", "id"},
	})
	if err != nil {
		t.Fatalf("validateTableUpsertOptions() error = %v", err)
	}
	if len(keys) != 2 || keys[0] != "tenant_id" || keys[1] != "id" {
		t.Fatalf("keys = %#v, want [tenant_id id]", keys)
	}

	invalid := []plugin.TableUpsertOptions{
		{Fields: []datatype.FieldInfo{{Name: "id"}}},
		{Fields: []datatype.FieldInfo{{Name: "id"}}, Keys: []string{"id", "id"}},
		{Fields: []datatype.FieldInfo{{Name: "id"}}, Keys: []string{"missing"}},
	}
	for _, opts := range invalid {
		if _, err := writer.validateTableUpsertOptions(opts); err == nil {
			t.Fatalf("validateTableUpsertOptions(%#v) succeeded, want error", opts)
		}
	}
}

func TestMySQLCompatibleUniqueIndexesMustAllMatchConfiguredKeys(t *testing.T) {
	if !mysqlCompatibleUniqueIndexesMatchKeys([]string{"tenant_id", "id"}, [][]string{{"tenant_id", "id"}}) {
		t.Fatal("configured composite key was rejected")
	}
	if mysqlCompatibleUniqueIndexesMatchKeys([]string{"id"}, [][]string{{"id"}, {"email"}}) {
		t.Fatal("competing unique index was accepted")
	}
	if mysqlCompatibleUniqueIndexesMatchKeys([]string{"tenant_id", "id"}, [][]string{{"id", "tenant_id"}}) {
		t.Fatal("different unique key order was accepted")
	}
}

func TestMySQLCompatibleOnDuplicateKeyClauseUpdatesOnlyNonKeyColumns(t *testing.T) {
	got := mysqlCompatibleOnDuplicateKeyClause([]string{"id", "name", "updated_at"}, []string{"id"})
	want := " AS new_values ON DUPLICATE KEY UPDATE `name` = new_values.`name`, `updated_at` = new_values.`updated_at`"
	if got != want {
		t.Fatalf("mysqlCompatibleOnDuplicateKeyClause() = %q, want %q", got, want)
	}

	keyOnly := mysqlCompatibleOnDuplicateKeyClause([]string{"id"}, []string{"id"})
	if !strings.Contains(keyOnly, "`id` = new_values.`id`") {
		t.Fatalf("key-only clause = %q, want idempotent no-op update", keyOnly)
	}
}

func TestMySQLCompatibleTableWriterRejectsSpatialUpsert(t *testing.T) {
	writer := MySQLCompatibleTableWriter{EngineType: "oceanbase", EngineName: "OceanBase", DefaultPort: 2881}
	err := writer.PrepareTableUpsert(nil, nil, plugin.EngineCatalogPath{}, plugin.TableUpsertOptions{
		Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "shape", Type: datatype.FieldTypeGeometry}},
		Keys:   []string{"id"},
	})
	if err == nil || !strings.Contains(err.Error(), "does not support spatial fields") {
		t.Fatalf("PrepareTableUpsert() spatial error = %v", err)
	}
}
