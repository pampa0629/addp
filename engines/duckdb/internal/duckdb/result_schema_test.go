package duckdb

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
)

func TestNativeQueryOutputSchemaAndExactJSON(t *testing.T) {
	db, err := OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	query := `SELECT CAST(1 AS INTEGER) AS small, CAST(1 AS BIGINT) AS small_big,
		CAST(9007199254740993 AS BIGINT) AS id,
		CAST(100.123456789012345678 AS DECIMAL(38,18)) AS amount,
		CAST(NULL AS VARCHAR) AS missing, '中文  ' AS label,
		struct_pack(" 原名, ""字" := CAST(9007199254740993 AS BIGINT),
		amounts := [CAST(1.000000000000000001 AS DECIMAL(38,18)), NULL]) AS nested`
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "row", true: "empty"}[empty], func(t *testing.T) {
			statement := query
			if empty {
				statement += " WHERE false"
			}
			result, err := ExecuteQuery(context.Background(), conn, statement)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Fields) != 7 || len(result.Columns) != 7 {
				t.Fatalf("schema = %#v", result)
			}
			if result.Fields[0].Type != datatype.FieldTypeInt || result.Fields[1].Type != datatype.FieldTypeBigInt ||
				result.Fields[2].Type != datatype.FieldTypeBigInt || result.Fields[3].Type != datatype.FieldTypeDecimal ||
				result.Fields[3].Precision != 38 || result.Fields[3].Scale != 18 {
				t.Fatalf("native fields = %#v", result.Fields)
			}
			nested := result.Fields[6]
			if nested.Type != datatype.FieldTypeJSON || len(nested.Fields) != 2 || nested.Fields[0].Name != " 原名, \"字" ||
				nested.Fields[0].Type != datatype.FieldTypeBigInt || nested.Fields[1].Element == nil ||
				nested.Fields[1].Element.Type != datatype.FieldTypeDecimal || nested.Fields[1].Element.Scale != 18 {
				t.Fatalf("nested native schema = %#v", nested)
			}
			if empty {
				if len(result.Rows) != 0 {
					t.Fatal("empty query returned rows")
				}
				return
			}
			if _, ok := result.Rows[0]["small_big"].(int64); !ok {
				t.Fatalf("provider bigint is not native: %T", result.Rows[0]["small_big"])
			}
			if _, ok := result.Rows[0]["amount"].(exactDecimal); !ok {
				t.Fatalf("provider decimal is not native: %T", result.Rows[0]["amount"])
			}
			rows, err := datatype.RowsForJSON(result.Fields, result.Rows)
			if err != nil {
				t.Fatal(err)
			}
			if rows[0]["small_big"] != "1" || rows[0]["id"] != "9007199254740993" ||
				rows[0]["amount"] != "100.123456789012345678" || rows[0]["missing"] != nil || rows[0]["label"] != "中文  " {
				t.Fatalf("public rows = %#v", rows)
			}
			encoded, err := json.Marshal(rows)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded), `"1.000000000000000001"`) || !strings.Contains(string(encoded), `"9007199254740993"`) {
				t.Fatalf("nested exact JSON = %s", encoded)
			}
		})
	}
}

func TestNativeOutputRejectsDuplicateNamesAndUnsupportedTypes(t *testing.T) {
	db, err := OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, statement := range []string{"SELECT 1 AS n, 2 AS n", "SELECT map(['n'], [1]) AS unsupported"} {
		if _, err := ExecuteQuery(context.Background(), conn, statement); err == nil {
			t.Fatalf("unexpected output admitted: %s", statement)
		}
	}
}
