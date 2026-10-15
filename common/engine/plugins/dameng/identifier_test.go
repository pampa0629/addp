package dameng

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	_ "github.com/mattn/go-sqlite3"
)

func TestDM8TableExistencePreservesQuotedName(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE USER_OBJECTS (OBJECT_TYPE TEXT, OBJECT_NAME TEXT); INSERT INTO USER_OBJECTS VALUES ('TABLE','lowercase'), ('TABLE','MixedCase'), ('TABLE','UPPERCASE')`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lowercase", "MixedCase", "UPPERCASE"} {
		exists, err := tableExists(t.Context(), db, name)
		if err != nil || !exists {
			t.Errorf("exact name %q missing: exists=%v error=%v", name, exists, err)
		}
	}
	for _, name := range []string{"LOWERCASE", "mixedcase", "uppercase", `lowercase' OR 1=1 --`} {
		exists, err := tableExists(t.Context(), db, name)
		if err != nil || exists {
			t.Errorf("different quoted name %q resolved: exists=%v error=%v", name, exists, err)
		}
	}
}

func TestDM8MergeNameConflictCheck(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE USER_OBJECTS (OBJECT_TYPE TEXT, OBJECT_NAME TEXT);
		INSERT INTO USER_OBJECTS VALUES ('TABLE','Existing'),('TABLE','existing'),('VIEW','eXisting'),('TABLE','LONE'),('INDEX','lone')`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Existing", "existing", "eXisting", "EXISTING", "lone"} {
		if err := validateMergeTargetName(t.Context(), db, name); err == nil || !strings.Contains(err.Error(), "merge_target_name_conflict") {
			t.Errorf("ambiguous MERGE name %q accepted: %v", name, err)
		}
	}
	for _, name := range []string{"LONE", "new_table", `Existing' OR 1=1 --`} {
		if err := validateMergeTargetName(t.Context(), db, name); err != nil {
			t.Errorf("unambiguous name %q rejected: %v", name, err)
		}
	}
	if _, err := db.Exec("DROP TABLE USER_OBJECTS"); err != nil {
		t.Fatal(err)
	}
	if err := validateMergeTargetName(t.Context(), db, "new_table"); err == nil || strings.Contains(err.Error(), "merge_target_name_conflict") {
		t.Fatalf("dictionary failure did not fail closed: %v", err)
	}
}

// SQLite observes the real catalog query's target predicate and bindings.
// No field or constraint dictionary exists in this fixture: a leaf summary
// must not depend on those dictionaries or read any business rows.
func TestDM8ExactTableSummaryQuery(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE user_objects (object_name TEXT, object_type TEXT, created TIMESTAMP, last_ddl_time TIMESTAMP)`,
		`CREATE TABLE user_tab_comments (table_name TEXT, comments TEXT)`,
		`CREATE TABLE user_tables (table_name TEXT, num_rows INTEGER)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"UPPER", "upper", "MiXeD", `quoted'"table`} {
		if _, err := db.Exec("INSERT INTO user_objects (object_name,object_type) VALUES (?,'TABLE')", name); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO user_tab_comments VALUES (?,?)", name, name+" comment"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO user_tables VALUES (?,5)", name); err != nil {
			t.Fatal(err)
		}
		info, err := getTableInfo(t.Context(), db, name)
		if err != nil || info == nil || info.Name != name || info.Comment != name+" comment" || info.EstimatedRowCount == nil || *info.EstimatedRowCount != 5 || info.RowCount != nil || len(info.Fields) != 0 || len(info.PrimaryKey) != 0 {
			t.Fatalf("exact summary %q: %+v %v", name, info, err)
		}
	}
	for _, name := range []string{"missing", "Upper", "MIXED", `upper' OR 1=1 --`} {
		if info, err := getTableInfo(t.Context(), db, name); info != nil || !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
			t.Fatalf("missing name %q resolved: %+v %v", name, info, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO user_objects (object_name,object_type) VALUES ('not_a_table','INDEX')`); err != nil {
		t.Fatal(err)
	}
	if _, err := getTableInfo(t.Context(), db, "not_a_table"); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("non-tabular object resolved: %v", err)
	}
	if _, err := db.Exec("DROP TABLE user_objects"); err != nil {
		t.Fatal(err)
	}
	if info, err := getTableInfo(t.Context(), db, "upper"); info != nil || err == nil || errors.Is(err, sql.ErrNoRows) || plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("query error downgraded to absence: %+v %v", info, err)
	}
}

func TestDM8UniqueFieldsPreserveQuotedNames(t *testing.T) {
	if !equalFieldSet([]string{"keyId", "version"}, []string{"version", "keyId"}) {
		t.Fatal("same exact key set rejected")
	}
	if equalFieldSet([]string{"keyId"}, []string{"KEYID"}) {
		t.Fatal("different quoted key accepted")
	}
	if equalFieldSet([]string{"keyId", "Version"}, []string{"version", "keyId"}) {
		t.Fatal("different quoted compound key accepted")
	}
}
