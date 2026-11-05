package plugin

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	if r.query == "binary" {
		return []string{"payload_alias", "expression", "empty_bytes", "null_bytes", "shape_alias", "label", "amount", "document"}
	}
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
	if r.query == "binary" {
		copy(values, []driver.Value{[]byte{0, 255, 128}, []byte{0, 254}, []byte{}, nil, resultSchemaGeometry(), []byte("中文"), []byte("12345678901234567890.123456789012345678"), []byte(`{"a":1}`)})
		return nil
	}
	copy(values, []driver.Value{int64(2), int64(9007199254740993), []byte("12345678901234567890.123456789012345678"), nil})
	return nil
}
func (r *resultSchemaRows) ColumnTypeDatabaseTypeName(index int) string {
	if r.query == "binary" {
		return []string{"BYTEA", "BYTEA", "BYTEA", "BYTEA", "GEOMETRY", "TEXT", "NUMERIC", "JSON"}[index]
	}
	return []string{"INT4", "INT8", "NUMERIC", "INT8"}[index]
}

func TestSQLResultPreservesBinaryValuesForJSON(t *testing.T) {
	db, err := sql.Open("addp-result-schema-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(), "binary")
	if err != nil {
		t.Fatal(err)
	}
	result, err := ScanSQLRows(rows, SQLResultFieldMapper(func(native string) datatype.FieldType {
		switch {
		case native == "BYTEA":
			return datatype.FieldTypeBytes
		case native == "GEOMETRY":
			return datatype.FieldTypeGeometry
		case strings.HasPrefix(native, "NUMERIC"):
			return datatype.FieldTypeDecimal
		case native == "JSON":
			return datatype.FieldTypeJSON
		default:
			return datatype.FieldTypeString
		}
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	row := result.Rows[0]
	for name, want := range map[string][]byte{"payload_alias": {0, 255, 128}, "expression": {0, 254}, "empty_bytes": {}, "shape_alias": resultSchemaGeometry()} {
		got, ok := row[name].([]byte)
		if !ok || got == nil || !bytes.Equal(got, want) {
			t.Fatalf("%s lost binary value: %T %#v", name, row[name], row[name])
		}
	}
	if row["null_bytes"] != nil || row["label"] != "中文" || row["amount"] != "12345678901234567890.123456789012345678" || row["document"] != `{"a":1}` {
		t.Fatal("non-binary values changed")
	}
	wire, err := datatype.QueryRowsForJSON(result.Fields, result.Rows)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`\u0000`)) || bytes.Contains(encoded, []byte(`\ufffd`)) {
		t.Fatal("binary data encoded as text")
	}
	var decoded []map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded[0]["payload_alias"] != base64.StdEncoding.EncodeToString([]byte{0, 255, 128}) || decoded[0]["empty_bytes"] != "" || decoded[0]["null_bytes"] != nil {
		t.Fatal("binary JSON representation lost")
	}
}
func (r *resultSchemaRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	if r.query == "binary" {
		return 38, 18, index == 6
	}
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
		result, err := ScanSQLRows(rows, SQLResultFieldMapper(mapper), nil)
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
	if _, err = ScanSQLRows(rows, SQLResultFieldMapper(mapper), nil); err == nil {
		t.Fatal("ambiguous output columns accepted")
	}
}

func resultSchemaGeometry() []byte {
	raw, _ := hex.DecodeString("0101000020e6100000000000000000f03f0000000000000040")
	return raw
}
