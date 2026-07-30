package service

import "testing"

func TestClassifySQLExecutionEffect(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    SQLExecutionEffect
		wantErr bool
	}{
		{name: "select", sql: "SELECT * FROM cities", want: SQLExecutionEffectRead},
		{name: "leading comments", sql: "-- comment\n/* nested /* comment */ ok */ SELECT 1;", want: SQLExecutionEffectRead},
		{name: "with select", sql: "WITH source AS (SELECT 1) SELECT * FROM source", want: SQLExecutionEffectRead},
		{name: "with recursive insert", sql: "WITH RECURSIVE ids AS (SELECT 1) INSERT INTO target SELECT * FROM ids", want: SQLExecutionEffectWrite},
		{name: "quoted keywords", sql: `SELECT 'DELETE; DROP', "UPDATE" FROM t`, want: SQLExecutionEffectRead},
		{name: "dollar quoted keywords", sql: `SELECT $tag$DELETE; DROP$tag$`, want: SQLExecutionEffectRead},
		{name: "show", sql: "SHOW search_path", want: SQLExecutionEffectRead},
		{name: "describe", sql: "DESCRIBE cities", want: SQLExecutionEffectRead},
		{name: "insert", sql: "INSERT INTO cities(id) VALUES (1)", want: SQLExecutionEffectWrite},
		{name: "merge", sql: "MERGE INTO cities USING incoming ON cities.id = incoming.id WHEN MATCHED THEN UPDATE SET name = incoming.name", want: SQLExecutionEffectWrite},
		{name: "ddl", sql: "CREATE TABLE cities(id bigint)", want: SQLExecutionEffectDDL},
		{name: "external call", sql: "CALL refresh_catalog()", want: SQLExecutionEffectExternalEffect},
		{name: "select for update", sql: "SELECT * FROM cities FOR UPDATE", want: SQLExecutionEffectWrite},
		{name: "select lock share", sql: "SELECT * FROM cities LOCK IN SHARE MODE", want: SQLExecutionEffectWrite},
		{name: "select into", sql: "SELECT * INTO OUTFILE '/tmp/cities.csv' FROM cities", want: SQLExecutionEffectExternalEffect},
		{name: "explain select", sql: "EXPLAIN (FORMAT JSON) SELECT * FROM cities", want: SQLExecutionEffectRead},
		{name: "explain analyze update", sql: "EXPLAIN ANALYZE UPDATE cities SET name = 'x'", want: SQLExecutionEffectWrite},
		{name: "multiple statements", sql: "SELECT 1; DELETE FROM cities", wantErr: true},
		{name: "unknown statement", sql: "SET search_path = public", wantErr: true},
		{name: "unclosed string", sql: "SELECT 'broken", wantErr: true},
		{name: "unclosed comment", sql: "SELECT 1 /* broken", wantErr: true},
		{name: "empty", sql: " -- only comment", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ClassifySQLExecutionEffect(test.sql)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ClassifySQLExecutionEffect() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ClassifySQLExecutionEffect() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ClassifySQLExecutionEffect() = %q, want %q", got, test.want)
			}
		})
	}
}
