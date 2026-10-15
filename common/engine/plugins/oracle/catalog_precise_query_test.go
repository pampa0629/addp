package oracle

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
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
		if !strings.Contains(query, "o.object_name = ?") {
			t.Errorf("lookup lacks target predicate: %s", query)
		}
		for _, clause := range []string{"all_objects", "all_secondary_objects", "internal_comment.comments LIKE ?"} {
			if !strings.Contains(query, clause) {
				t.Errorf("lookup lost source filter %q: %s", clause, query)
			}
		}
		if !reflect.DeepEqual(tx.Statement.Vars, []interface{}{namespace, InternalCaptureTableCommentPrefix + "%", InternalTransferApplyTableComment, table}) {
			t.Errorf("bindings=%#v", tx.Statement.Vars)
		}
		if strings.Contains(query, table) || strings.Contains(query, namespace) {
			t.Error("target identifier interpolated into catalog query")
		}
		tx.AddError(sentinel)
	}); err != nil {
		t.Fatal(err)
	}
	result, err := (&OraclePlugin{}).getTable(t.Context(), db, namespace, table)
	if result != nil || !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("exact query result=%#v error=%v calls=%d", result, err, calls)
	}
}

func TestCatalogNamespacesUseAvailableDictionaryColumns(t *testing.T) {
	for _, maintenanceColumn := range []bool{false, true} {
		name := "11g_dictionary"
		if maintenanceColumn {
			name = "dictionary_with_maintenance_flag"
		}
		t.Run(name, func(t *testing.T) {
			db := oracleNamespaceDictionaryFixture(t, maintenanceColumn)
			p := &OraclePlugin{}
			root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 27)
			entries, err := p.listNamespaces(t.Context(), db, root)
			if err != nil {
				t.Fatal(err)
			}
			if got := oracleCatalogEntryNames(entries); !reflect.DeepEqual(got, []string{"BUSINESS", "MIDREAL"}) {
				t.Fatalf("schemas = %v, want accessible business and own empty schema", got)
			}
			if entries[0].LeafCount == nil || *entries[0].LeafCount != 3 || entries[1].LeafCount == nil || *entries[1].LeafCount != 0 {
				t.Fatalf("leaf counts = %v, %v, want 3, 0", entries[0].LeafCount, entries[1].LeafCount)
			}
			for _, schema := range []string{"SYS", "SYSMAN", "EXFSYS", "OWBSYS", "APEX_030200", "FLOWS_FILES"} {
				if _, err := p.getTable(t.Context(), db, schema, "INTERNAL_TABLE"); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorUnsupported) {
					t.Errorf("direct system schema lookup %s: %v", schema, err)
				}
			}
		})
	}
}

func TestCatalogNamespacesRejectDictionaryProbeFailure(t *testing.T) {
	db := oracleNamespaceDictionaryFixture(t, true)
	if err := db.Exec("DROP TABLE all_tab_columns").Error; err != nil {
		t.Fatal(err)
	}
	p := &OraclePlugin{}
	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 27)
	entries, err := p.listNamespaces(t.Context(), db, root)
	if err == nil || len(entries) != 0 || !strings.Contains(err.Error(), "dictionary") {
		t.Fatalf("dictionary failure must not return a weakened catalog: entries=%v err=%v", entries, err)
	}
}

func oracleNamespaceDictionaryFixture(t *testing.T, maintenanceColumn bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	// USER is Oracle's current-user expression; this fixture supplies its value
	// as a column so the native catalog SELECT can execute unchanged in SQLite.
	userColumns := "username TEXT, USER TEXT DEFAULT 'MIDREAL'"
	if maintenanceColumn {
		userColumns += ", oracle_maintained TEXT DEFAULT 'N'"
	}
	for _, statement := range []string{
		"CREATE TABLE all_users (" + userColumns + ")",
		"CREATE TABLE all_tab_columns (owner TEXT, table_name TEXT, column_name TEXT)",
		"CREATE TABLE all_objects (owner TEXT, object_name TEXT, object_type TEXT)",
		"CREATE TABLE all_mviews (owner TEXT, mview_name TEXT)",
		"CREATE TABLE all_secondary_objects (secondary_object_owner TEXT, secondary_object_name TEXT)",
		"CREATE TABLE all_tab_comments (owner TEXT, table_name TEXT, comments TEXT)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, schema := range []string{"BUSINESS", "MIDREAL", "EMPTY", "INACCESSIBLE", "PDBADMIN", "SYS", "SYSMAN", "EXFSYS", "OWBSYS", "APEX_030200", "FLOWS_FILES"} {
		if err := db.Exec("INSERT INTO all_users (username) VALUES (?)", schema).Error; err != nil {
			t.Fatal(err)
		}
	}
	if maintenanceColumn {
		for _, statement := range []string{
			"INSERT INTO all_tab_columns VALUES ('SYS', 'ALL_USERS', 'ORACLE_MAINTAINED')",
			"INSERT INTO all_users (username, oracle_maintained) VALUES ('NATIVE_INTERNAL', 'Y')",
			"INSERT INTO all_objects VALUES ('NATIVE_INTERNAL', 'INTERNAL_TABLE', 'TABLE')",
		} {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, row := range [][3]string{
		{"BUSINESS", "ORDERS", "TABLE"}, {"BUSINESS", "ORDER_VIEW", "VIEW"},
		{"BUSINESS", "ORDER_MV", "MATERIALIZED VIEW"}, {"BUSINESS", "ORDER_MV", "TABLE"},
		{"BUSINESS", "ORDER_ALIAS", "SYNONYM"}, {"BUSINESS", "SECONDARY_TABLE", "TABLE"},
		{"BUSINESS", "CAPTURE_TABLE", "TABLE"}, {"BUSINESS", "APPLY_LEDGER", "TABLE"},
		{"SYS", "INTERNAL_TABLE", "TABLE"}, {"SYSMAN", "INTERNAL_TABLE", "TABLE"},
		{"EXFSYS", "INTERNAL_TABLE", "TABLE"}, {"OWBSYS", "INTERNAL_TABLE", "TABLE"},
		{"APEX_030200", "INTERNAL_TABLE", "TABLE"}, {"FLOWS_FILES", "INTERNAL_TABLE", "TABLE"},
	} {
		if err := db.Exec("INSERT INTO all_objects VALUES (?, ?, ?)", row[0], row[1], row[2]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []struct {
		sql  string
		args []interface{}
	}{
		{"INSERT INTO all_mviews VALUES ('BUSINESS', 'ORDER_MV')", nil},
		{"INSERT INTO all_secondary_objects VALUES ('BUSINESS', 'SECONDARY_TABLE')", nil},
		{"INSERT INTO all_tab_comments VALUES ('BUSINESS', 'CAPTURE_TABLE', ?)", []interface{}{InternalCaptureTableCommentPrefix + "fixture"}},
		{"INSERT INTO all_tab_comments VALUES ('BUSINESS', 'APPLY_LEDGER', ?)", []interface{}{InternalTransferApplyTableComment}},
	} {
		if err := db.Exec(statement.sql, statement.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
