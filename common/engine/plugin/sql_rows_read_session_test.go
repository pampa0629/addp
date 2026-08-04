package plugin

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/addp/common/datatype"
	_ "github.com/mattn/go-sqlite3"
)

func TestSQLRowsTableReadSessionReadsBatchesAndClosesResources(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := db.Exec("CREATE TABLE records (id INTEGER, name TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec("INSERT INTO records VALUES (1, 'one'), (2, 'two'), (3, NULL)"); err != nil {
		t.Fatalf("insert rows: %v", err)
	}
	rows, err := db.Query("SELECT id, name FROM records ORDER BY id")
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	session, err := NewSQLRowsTableReadSession(db, rows, []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
	})
	if err != nil {
		t.Fatalf("NewSQLRowsTableReadSession() error = %v", err)
	}

	first, err := session.ReadBatch(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadBatch(first) error = %v", err)
	}
	if first.Offset != 0 || len(first.Rows) != 2 || first.Rows[0]["id"] != int64(1) || first.Rows[1]["name"] != "two" {
		t.Fatalf("first batch = %#v", first)
	}
	second, err := session.ReadBatch(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadBatch(second) error = %v", err)
	}
	if second.Offset != 2 || len(second.Rows) != 1 || second.Rows[0]["id"] != int64(3) || second.Rows[0]["name"] != nil {
		t.Fatalf("second batch = %#v", second)
	}
	empty, err := session.ReadBatch(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadBatch(empty) error = %v", err)
	}
	if empty.Offset != 3 || len(empty.Rows) != 0 {
		t.Fatalf("empty batch = %#v", empty)
	}

	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := session.ReadBatch(context.Background(), 1); err == nil {
		t.Fatal("ReadBatch() succeeded after Close()")
	}
	if err := db.PingContext(context.Background()); err == nil || !errors.Is(err, sql.ErrConnDone) && err.Error() != "sql: database is closed" {
		t.Fatalf("database remains usable after session close: %v", err)
	}
}
