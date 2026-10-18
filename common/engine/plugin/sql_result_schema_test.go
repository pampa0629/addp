package plugin

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
)

type resultSchemaDriver struct{}
type resultSchemaConn struct{}
type resultSchemaRows struct {
	query string
	read  bool
}

func (resultSchemaDriver) Open(string) (driver.Conn, error)  { return resultSchemaConn{}, nil }
func (resultSchemaConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (resultSchemaConn) Close() error                        { return nil }
func (resultSchemaConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }
func (resultSchemaConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	return &resultSchemaRows{query: query}, nil
}
func (r *resultSchemaRows) Columns() []string {
	if r.query == "duplicate" {
		return []string{"same", "same", "amount", "nullable"}
	}
	return []string{"small", "large", "amount", "nullable"}
}
func (r *resultSchemaRows) Close() error { return nil }
func (r *resultSchemaRows) Next(values []driver.Value) error {
	if r.read || r.query == "empty" {
		return io.EOF
	}
	r.read = true
	copy(values, []driver.Value{int64(2), int64(9007199254740993), []byte("12345678901234567890.123456789012345678"), nil})
	return nil
}
func (r *resultSchemaRows) ColumnTypeDatabaseTypeName(index int) string {
	return []string{"INT4", "INT8", "NUMERIC", "INT8"}[index]
}
func (r *resultSchemaRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	return 38, 18, index == 2
}
func (r *resultSchemaRows) ColumnTypeNullable(index int) (bool, bool) { return index == 3, true }
func init()                                                           { sql.Register("addp-result-schema-test", resultSchemaDriver{}) }

func TestSQLResultSchemaComesFromNativeColumnsIncludingEmptyResults(t *testing.T) {
	db, err := sql.Open("addp-result-schema-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mapper := func(native string) datatype.FieldType {
		switch {
		case native == "INT4":
			return datatype.FieldTypeInt
		case native == "INT8":
			return datatype.FieldTypeBigInt
		case strings.HasPrefix(native, "NUMERIC("):
			return datatype.FieldTypeDecimal
		}
		return datatype.FieldTypeUnknown
	}
	for _, query := range []string{"rows", "empty"} {
		rows, err := db.QueryContext(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		result, err := scanRuntimeSQLRows(rows, SQLResultFieldMapper(mapper))
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Fields) != 4 || result.Fields[0].Type != datatype.FieldTypeInt || result.Fields[1].Type != datatype.FieldTypeBigInt || result.Fields[2].Type != datatype.FieldTypeDecimal || result.Fields[2].Precision != 38 || result.Fields[2].Scale != 18 || !result.Fields[3].Nullable {
			t.Fatalf("native schema lost: %#v", result.Fields)
		}
		if query == "rows" && (result.Rows[0]["small"] != int64(2) || result.Rows[0]["large"] != int64(9007199254740993)) {
			t.Fatal("native integers changed")
		}
	}
	rows, err := db.QueryContext(t.Context(), "duplicate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = scanRuntimeSQLRows(rows, SQLResultFieldMapper(mapper)); err == nil {
		t.Fatal("ambiguous output columns accepted")
	}
}
