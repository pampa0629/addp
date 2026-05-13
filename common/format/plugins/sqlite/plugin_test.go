package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/addp/common/format"
	_ "github.com/mattn/go-sqlite3"
)

func TestDescribeTableRequiresExplicitTableOption(t *testing.T) {
	t.Parallel()

	parser := NewParser(nil)
	_, err := parser.DescribeTable(context.Background(), bytes.NewReader(sqliteTestDatabaseBytes(t)), nil)
	if err == nil {
		t.Fatal("DescribeTable() error = nil, want explicit table option error")
	}
}

func TestDescribeTableUsesSelectedTable(t *testing.T) {
	t.Parallel()

	parser := NewParser(nil)
	info, err := parser.DescribeTable(context.Background(), bytes.NewReader(sqliteTestDatabaseBytes(t)), &format.ParseOptions{
		ExtraParams: map[string]interface{}{"table": "cities"},
	})
	if err != nil {
		t.Fatalf("DescribeTable() error = %v", err)
	}
	if info.Name != "cities" {
		t.Fatalf("Name = %q, want cities", info.Name)
	}
	if len(info.Fields) != 2 || info.Fields[0].Name != "id" || info.Fields[1].Name != "name" {
		t.Fatalf("Fields = %#v, want id/name", info.Fields)
	}
}

func TestDescribeContainerReturnsLightweightChildren(t *testing.T) {
	t.Parallel()

	parser := NewParser(nil)
	info, err := parser.DescribeContainer(context.Background(), bytes.NewReader(sqliteTestDatabaseBytes(t)), &format.ParseOptions{
		ExtraParams: map[string]interface{}{
			"table_limit": 0,
			"row_limit":   0,
		},
	})
	if err != nil {
		t.Fatalf("DescribeContainer() error = %v", err)
	}
	if info.ChildCount != 2 || len(info.Children) != 2 {
		t.Fatalf("container children = %#v, want 2", info.Children)
	}
	if len(info.Children[0].Fields) != 0 {
		t.Fatalf("container child fields = %#v, want none", info.Children[0].Fields)
	}
	if info.Children[0].ColumnCount == nil {
		t.Fatalf("container child column_count missing: %#v", info.Children[0])
	}
}

func TestAnalyzeTableLimitZeroListsAllTables(t *testing.T) {
	t.Parallel()

	db, cleanup := openSQLiteTestDatabase(t, sqliteTestDatabaseBytes(t))
	defer cleanup()

	opts := DefaultOptions()
	opts.TableLimit = 0
	opts.SampleRowLimit = 0
	result, err := Analyze(context.Background(), db, &opts)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(result.Metadata.Tables) != 2 {
		t.Fatalf("tables = %#v, want 2 tables", result.Metadata.Tables)
	}
}

func sqliteTestDatabaseBytes(t *testing.T) []byte {
	t.Helper()

	tmp, err := os.CreateTemp("", "sqlite-plugin-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp db: %v", err)
	}
	defer os.Remove(path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE animals (name TEXT)`,
		`CREATE TABLE cities (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sqlite: %v", err)
	}
	return data
}

func openSQLiteTestDatabase(t *testing.T, data []byte) (*sql.DB, func()) {
	t.Helper()

	tmp, err := os.CreateTemp("", "sqlite-plugin-open-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		t.Fatalf("write temp db: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp db: %v", err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		os.Remove(path)
		t.Fatalf("open sqlite: %v", err)
	}
	return db, func() {
		_ = db.Close()
		_ = os.Remove(path)
	}
}
