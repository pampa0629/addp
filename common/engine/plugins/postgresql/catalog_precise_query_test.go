package postgresql

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExactCatalogLookupBindsTargetAndPreservesSourceFilters(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sentinel := errors.New("stop before native execution")
	namespace, table := "business.space", "target.'table"
	calls := 0
	if err := db.Callback().Row().Before("gorm:row").Register("assert_exact_catalog", func(tx *gorm.DB) {
		calls++
		query := tx.Statement.SQL.String()
		if !strings.Contains(query, "t.table_name = $2") {
			t.Errorf("lookup lacks target predicate: %s", query)
		}
		for _, clause := range []string{"information_schema.tables", "has_schema_privilege(t.table_schema, 'USAGE')"} {
			if !strings.Contains(query, clause) {
				t.Errorf("lookup lost source filter %q: %s", clause, query)
			}
		}
		if !reflect.DeepEqual(tx.Statement.Vars, []interface{}{namespace, table}) {
			t.Errorf("bindings=%#v", tx.Statement.Vars)
		}
		if strings.Contains(query, table) || strings.Contains(query, namespace) {
			t.Error("target identifier interpolated into catalog query")
		}
		tx.AddError(sentinel)
	}); err != nil {
		t.Fatal(err)
	}
	result, err := (&PostgreSQLPlugin{}).getTable(t.Context(), db, namespace, table)
	if result != nil || !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("exact query result=%#v error=%v calls=%d", result, err, calls)
	}
}
