package mysql

import (
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestValidateMySQLUpsertOptions(t *testing.T) {
	keys, err := validateMySQLUpsertOptions(plugin.TableUpsertOptions{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "name", Type: datatype.FieldTypeString},
		},
		Keys: []string{"id"},
	})
	if err != nil {
		t.Fatalf("validateMySQLUpsertOptions() error = %v", err)
	}
	if len(keys) != 1 || keys[0] != "id" {
		t.Fatalf("keys = %#v, want [id]", keys)
	}
}

func TestValidateMySQLUpsertOptionsRejectsMissingAndDuplicateKeys(t *testing.T) {
	tests := []plugin.TableUpsertOptions{
		{Fields: []datatype.FieldInfo{{Name: "id"}}},
		{Fields: []datatype.FieldInfo{{Name: "id"}}, Keys: []string{"id", "id"}},
		{Fields: []datatype.FieldInfo{{Name: "id"}}, Keys: []string{"missing"}},
	}
	for _, opts := range tests {
		if _, err := validateMySQLUpsertOptions(opts); err == nil {
			t.Fatalf("validateMySQLUpsertOptions(%#v) succeeded, want error", opts)
		}
	}
}

func TestMySQLUniqueIndexesCompatibleRequiresOnlyConfiguredKeys(t *testing.T) {
	if !mysqlUniqueIndexesCompatible([]string{"tenant_id", "id"}, [][]string{{"tenant_id", "id"}}) {
		t.Fatal("configured composite key was rejected")
	}
	if mysqlUniqueIndexesCompatible([]string{"id"}, [][]string{{"id"}, {"email"}}) {
		t.Fatal("competing unique index was accepted")
	}
	if mysqlUniqueIndexesCompatible([]string{"tenant_id", "id"}, [][]string{{"id", "tenant_id"}}) {
		t.Fatal("different unique key order was accepted")
	}
}

func TestMySQLOnDuplicateKeyClauseUpdatesOnlyNonKeyColumns(t *testing.T) {
	got := mysqlOnDuplicateKeyClause([]string{"id", "name", "updated_at"}, []string{"id"})
	want := " AS new_values ON DUPLICATE KEY UPDATE `name` = new_values.`name`, `updated_at` = new_values.`updated_at`"
	if got != want {
		t.Fatalf("mysqlOnDuplicateKeyClause() = %q, want %q", got, want)
	}

	keyOnly := mysqlOnDuplicateKeyClause([]string{"id"}, []string{"id"})
	if !strings.Contains(keyOnly, "`id` = new_values.`id`") {
		t.Fatalf("key-only clause = %q, want idempotent no-op update", keyOnly)
	}
}
