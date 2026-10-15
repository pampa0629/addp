package oracle

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// Execute the actual bounded catalog queries, with no business-row fixture or
// Spatial view unless the case needs one. Oracle DROP itself is covered by T2.
func TestOracleDeleteMetadataPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, owner           string
		index, metadata, fail bool
		want                  bool
	}{
		{name: "scalar without Spatial"},
		{name: "scalar with stale metadata", metadata: true, want: true},
		{name: "native geometry without metadata view", owner: "MDSYS", fail: true},
		{name: "unknown object without metadata view", owner: "CUSTOM", fail: true},
		{name: "domain index without metadata view", index: true, fail: true},
		{name: "native geometry empty metadata", owner: "MDSYS", metadata: true},
		{name: "native geometry metadata", owner: "MDSYS", metadata: true, want: true},
		{name: "domain metadata", index: true, metadata: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			for _, q := range []string{
				"CREATE TABLE all_tab_cols (owner TEXT, table_name TEXT, data_type_owner TEXT)",
				"CREATE TABLE all_indexes (owner TEXT, table_owner TEXT, table_name TEXT, ityp_owner TEXT)",
				"CREATE TABLE all_objects (owner TEXT, object_name TEXT)",
				"CREATE TABLE all_synonyms (owner TEXT, synonym_name TEXT)",
			} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			schema, table := "Case.schema", "quoted.'Table"
			if _, err := db.Exec("INSERT INTO all_tab_cols VALUES (?, ?, ?)", schema, table, sql.NullString{String: tc.owner, Valid: tc.owner != ""}); err != nil {
				t.Fatal(err)
			}
			// Same-name facts in other schemas and other tables must not matter.
			if _, err := db.Exec("INSERT INTO all_tab_cols VALUES ('OTHER', ?, 'MDSYS'), (?, 'OTHER', 'MDSYS')", table, schema); err != nil {
				t.Fatal(err)
			}
			if tc.index {
				// Index owner can differ from the table owner. Only table identity
				// determines whether this index belongs to the deletion target.
				if _, err := db.Exec("INSERT INTO all_indexes VALUES ('INDEX_OWNER', ?, ?, 'MDSYS')", schema, table); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec("INSERT INTO all_indexes VALUES (?, 'OTHER', ?, 'MDSYS')", schema, table); err != nil {
				t.Fatal(err)
			}
			if tc.metadata {
				if _, err := db.Exec("INSERT INTO all_objects VALUES (?, 'USER_SDO_GEOM_METADATA')", schema); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("CREATE TABLE USER_SDO_GEOM_METADATA (TABLE_NAME TEXT)"); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("INSERT INTO USER_SDO_GEOM_METADATA VALUES ('OTHER')"); err != nil {
					t.Fatal(err)
				}
				if tc.want {
					if _, err := db.Exec("INSERT INTO USER_SDO_GEOM_METADATA VALUES (?)", table); err != nil {
						t.Fatal(err)
					}
				}
			}
			got, err := oracleDeleteMetadataPreflight(t.Context(), db, schema, table)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("cleanup=%v error=%v", got, err)
			}
			if tc.fail && !strings.Contains(err.Error(), "metadata") {
				t.Fatalf("error lost metadata context: %v", err)
			}
			if tc.fail {
				if _, err := db.Exec("CREATE TABLE all_tables (owner TEXT, table_name TEXT)"); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("INSERT INTO all_tables VALUES (?, ?)", schema, table); err != nil {
					t.Fatal(err)
				}
				// Native DROP ... PURGE is invalid in SQLite. Seeing the metadata
				// failure proves that the real delete path did not reach that DDL.
				if err := deleteOracleTable(t.Context(), db, schema, table); err == nil || !strings.Contains(err.Error(), "check Oracle delete spatial metadata") {
					t.Fatalf("delete reached DDL before metadata failure: %v", err)
				}
			}
			if !tc.metadata {
				if _, err := db.Exec("DROP TABLE all_tab_cols"); err != nil {
					t.Fatal(err)
				}
				if _, err := oracleDeleteMetadataPreflight(t.Context(), db, schema, table); err == nil {
					t.Fatal("dictionary failure treated as scalar proof")
				}
			}
		})
	}
}

func TestOracleDeleteMissingTable(t *testing.T) {
	for _, mode := range []string{"no Spatial", "orphan metadata", "broken public synonym", "inaccessible private synonym", "dictionary failure"} {
		t.Run(mode, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			for _, q := range []string{
				"CREATE TABLE all_tables (owner TEXT, table_name TEXT)",
				"CREATE TABLE all_tab_cols (owner TEXT, table_name TEXT, data_type_owner TEXT)",
				"CREATE TABLE all_indexes (owner TEXT, table_owner TEXT, table_name TEXT, ityp_owner TEXT)",
				"CREATE TABLE all_objects (owner TEXT, object_name TEXT)",
				"CREATE TABLE all_synonyms (owner TEXT, synonym_name TEXT)",
			} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			schema, table := "Case.schema", "quoted.'Table"
			// Other users' objects and private synonyms cannot prove availability.
			for _, q := range []string{
				"INSERT INTO all_objects VALUES ('OTHER', 'USER_SDO_GEOM_METADATA')",
				"INSERT INTO all_synonyms VALUES ('OTHER', 'USER_SDO_GEOM_METADATA')",
			} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "orphan metadata":
				for _, q := range []string{
					"CREATE TABLE USER_SDO_GEOM_METADATA (TABLE_NAME TEXT, COLUMN_NAME TEXT)",
					"INSERT INTO all_synonyms VALUES ('PUBLIC', 'USER_SDO_GEOM_METADATA')",
				} {
					if _, err := db.Exec(q); err != nil {
						t.Fatal(err)
					}
				}
				for _, name := range []string{table, table, "QUOTED.'TABLE", "OTHER"} {
					if _, err := db.Exec("INSERT INTO USER_SDO_GEOM_METADATA VALUES (?, 'Shape')", name); err != nil {
						t.Fatal(err)
					}
				}
			case "broken public synonym":
				if _, err := db.Exec("INSERT INTO all_synonyms VALUES ('PUBLIC', 'USER_SDO_GEOM_METADATA')"); err != nil {
					t.Fatal(err)
				}
			case "inaccessible private synonym":
				if _, err := db.Exec("INSERT INTO all_synonyms VALUES (?, 'USER_SDO_GEOM_METADATA')", schema); err != nil {
					t.Fatal(err)
				}
			case "dictionary failure":
				if _, err := db.Exec("DROP TABLE all_synonyms"); err != nil {
					t.Fatal(err)
				}
			}
			err = deleteOracleTable(t.Context(), db, schema, table)
			if mode == "broken public synonym" || mode == "inaccessible private synonym" || mode == "dictionary failure" {
				if err == nil || !strings.Contains(err.Error(), "check Oracle delete spatial metadata") {
					t.Fatalf("metadata failure lost: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := deleteOracleTable(t.Context(), db, schema, table); err != nil {
				t.Fatalf("repeated delete: %v", err)
			}
			if mode == "orphan metadata" {
				var count int
				if err := db.QueryRow("SELECT COUNT(*) FROM USER_SDO_GEOM_METADATA").Scan(&count); err != nil || count != 2 {
					t.Fatalf("wrong cleanup scope: %d %v", count, err)
				}
			}
		})
	}
}
