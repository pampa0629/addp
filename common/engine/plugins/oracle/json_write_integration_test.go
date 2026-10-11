package oracle

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pluginshared "github.com/addp/common/engine/plugins/shared"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
)

func TestIntegrationOracleTypedJSONWrites(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_JSON_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_JSON_INTEGRATION=1 to run Oracle JSON integration test")
	}
	for _, method := range []string{"session", "partitioned_apply"} {
		t.Run(method, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			connInfo := oracleIntegrationConnInfo(t)
			p := &OraclePlugin{}
			dsn, err := p.BuildDSN(connInfo)
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("oracle", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			schema := strings.ToUpper(connInfo["user"].(string))
			table := "ADDP_JSON_" + strings.ToUpper(uuid.NewString()[:8])
			qualified := commonquery.ForDialect("oracle").QualifiedTable(schema, table)
			defer dropOracleApplyIntegrationTable(db, schema, table)
			path := oracleApplyIntegrationPath(schema, table)
			fields := []datatype.FieldInfo{
				{Name: "ID", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
				{Name: "DOCUMENT", Type: datatype.FieldTypeJSON, Nullable: true},
				{Name: "PAYLOAD", Type: datatype.FieldTypeBytes},
			}
			opts := plugin.PartitionedTableChangeApplyOptions{
				ApplyIdentity: uuid.NewString(), SourceIdentity: "addp://engine/12/path/public/json_test?type=table",
				Fields: fields, Keys: []string{"ID"},
			}
			if method == "session" {
				err = p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields})
			} else {
				err = p.PreparePartitionedTableChangeApply(ctx, connInfo, path, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			var sqlType string
			if err := db.QueryRowContext(ctx, "SELECT data_type FROM user_tab_columns WHERE table_name = :1 AND column_name = 'DOCUMENT'", table).Scan(&sqlType); err != nil || sqlType != "JSON" {
				t.Fatalf("native JSON column type=%q err=%v", sqlType, err)
			}
			values := []interface{}{
				[]interface{}{1, "中文", false, nil},
				map[string]interface{}{"items": []interface{}{true, map[string]interface{}{"n": 7}}},
				[]interface{}{}, map[string]interface{}{}, []interface{}(nil), map[string]interface{}(nil),
				nil, []byte(nil), json.RawMessage(nil),
				true, int64(9007199254740993), json.Number("12345678901234567890.123456789"), 1.25,
				` {"n":9007199254740993} `, []byte(`[1,null]`), json.RawMessage(`{"raw":true}`),
				`"中文\n字符串"`, `""`, "null",
				map[string]interface{}{"large": strings.Repeat("x", 8192)},
			}
			rows := make([]map[string]interface{}, len(values))
			for i, value := range values {
				rows[i] = map[string]interface{}{"ID": int64(i + 1), "DOCUMENT": value, "PAYLOAD": []byte{0, 1, 255}}
			}
			var offset int64
			write := func(rows []map[string]interface{}) error {
				if method == "session" {
					sessionFields := append([]datatype.FieldInfo(nil), fields...)
					session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: sessionFields})
					if err != nil {
						return err
					}
					defer session.Abort(ctx)
					// The session owns its opening schema; later metadata cannot redefine JSON.
					sessionFields[1].Type = datatype.FieldTypeString
					if err := session.WriteBatch(ctx, &plugin.BatchData{
						Fields: []datatype.FieldInfo{{Name: "DOCUMENT", Type: datatype.FieldTypeString}}, Rows: rows,
					}); err != nil {
						return err
					}
					return session.Close(ctx)
				}
				changes := make([]plugin.PartitionedTableChange, len(rows))
				for i, row := range rows {
					changes[i] = plugin.PartitionedTableChange{
						Operation: plugin.TableChangeOperationUpsert,
						Position:  pluginshared.KafkaOffsetPosition("0", offset+int64(i+1)), Row: row,
					}
				}
				end := offset + int64(len(rows))
				_, err := p.ApplyPartitionedTableChanges(ctx, connInfo, path, oracleApplyBatch("0", offset, end, changes...), opts)
				if err == nil {
					offset = end
				}
				return err
			}
			if err := write(rows); err != nil {
				t.Fatalf("write JSON matrix: %v", err)
			}
			assertOracleJSONRows(t, ctx, db, qualified, values)
			if method == "partitioned_apply" {
				// Matched MERGE updates must preserve both SQL NULL and JSON null too.
				for i := range rows {
					rows[i]["DOCUMENT"] = values[len(values)-1-i]
				}
				if err := write(rows); err != nil {
					t.Fatalf("update JSON matrix: %v", err)
				}
				reversed := make([]interface{}, len(values))
				for i := range reversed {
					reversed[i] = values[len(values)-1-i]
				}
				assertOracleJSONRows(t, ctx, db, qualified, reversed)
			}
			// A valid earlier row and invalid later row must roll back together.
			for i, invalid := range []interface{}{"{", []byte{}, json.RawMessage("{} []"), math.NaN(), make(chan int)} {
				id := int64(100 + i*2)
				err := write([]map[string]interface{}{
					{"ID": id, "DOCUMENT": map[string]interface{}{"valid": true}, "PAYLOAD": []byte{1}},
					{"ID": id + 1, "DOCUMENT": invalid, "PAYLOAD": []byte{1}},
				})
				if err == nil || !strings.Contains(err.Error(), "DOCUMENT") {
					t.Fatalf("invalid %T: expected field error, got %v", invalid, err)
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified+" WHERE \"ID\" >= 100").Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed write left %d rows, err=%v", count, err)
				}
				if method == "partitioned_apply" {
					var committed int64
					ledger := commonquery.ForDialect("oracle").QualifiedTable(schema, oracleTransferApplyLedgerTable)
					if err := db.QueryRowContext(ctx, "SELECT next_offset FROM "+ledger+" WHERE apply_identity = :1 AND partition_key = '0'", opts.ApplyIdentity).Scan(&committed); err != nil || committed != offset {
						t.Fatalf("failed JSON advanced ledger: got %d want %d err=%v", committed, offset, err)
					}
				}
			}
			// Recovery at the same offset must remain possible after rejected JSON.
			if err := write([]map[string]interface{}{{"ID": int64(999), "DOCUMENT": []interface{}{true}, "PAYLOAD": []byte{1}}}); err != nil {
				t.Fatalf("write after rejection: %v", err)
			}
		})
	}
}

func assertOracleJSONRows(t *testing.T, ctx context.Context, db *sql.DB, qualified string, values []interface{}) {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT \"ID\", JSON_SERIALIZE(\"DOCUMENT\" RETURNING CLOB), DBMS_LOB.SUBSTR(\"PAYLOAD\", 3, 1) FROM "+qualified+" ORDER BY \"ID\"")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var id int64
		var document sql.NullString
		var payload []byte
		if err := rows.Scan(&id, &document, &payload); err != nil {
			t.Fatal(err)
		}
		if index >= len(values) || id != int64(index+1) || !reflect.DeepEqual(payload, []byte{0, 1, 255}) {
			t.Fatalf("row id=%d index=%d payload=%v", id, index, payload)
		}
		value := values[index]
		sqlNull := value == nil
		switch v := value.(type) {
		case []byte:
			sqlNull = v == nil
		case json.RawMessage:
			sqlNull = v == nil
		}
		if document.Valid == sqlNull {
			t.Fatalf("row %d SQL NULL=%v want=%v", id, !document.Valid, sqlNull)
		}
		if !sqlNull {
			var expected []byte
			switch v := value.(type) {
			case string:
				expected = []byte(v)
			case []byte:
				expected = v
			case json.RawMessage:
				expected = v
			default:
				expected, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
			}
			decode := func(text string) interface{} {
				decoder := json.NewDecoder(strings.NewReader(text))
				decoder.UseNumber()
				var decoded interface{}
				if err := decoder.Decode(&decoded); err != nil {
					t.Fatalf("row %d invalid JSON: %v", id, err)
				}
				return decoded
			}
			if got, want := decode(document.String), decode(string(expected)); !reflect.DeepEqual(got, want) {
				t.Fatalf("row %d JSON mismatch: got %#v want %#v", id, got, want)
			}
		}
		index++
	}
	if err := rows.Err(); err != nil || index != len(values) {
		t.Fatalf("read %d/%d rows, err=%v", index, len(values), err)
	}
}
