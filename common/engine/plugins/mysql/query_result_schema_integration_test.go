package mysql

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationMySQLResultSchema(t *testing.T) {
	db, provider, serverInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := t.Context()
	table := mysqlDialect().QualifiedTable(database, "result_schema")
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (small_value INT, signed_value BIGINT, unsigned_value BIGINT UNSIGNED, amount DECIMAL(38,18), optional_value BIGINT) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (2,9007199254740993,18446744073709551615,12345678901234567890.123456789012345678,NULL)"); err != nil {
		t.Fatal(err)
	}
	info := plugin.ConnectionInfo{}
	for key, value := range serverInfo {
		info[key] = value
	}
	info["database"] = database
	for _, predicate := range []string{"small_value=2", "small_value<0"} {
		result, err := provider.ExecuteSQL(ctx, info, "SELECT small_value AS small_alias,signed_value AS signed_alias,unsigned_value AS unsigned_alias,amount,optional_value FROM "+table+" WHERE "+predicate, plugin.QueryOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Fields) != 5 || result.Fields[0].Name != "small_alias" || result.Fields[0].Type != datatype.FieldTypeInt || result.Fields[1].Type != datatype.FieldTypeBigInt || result.Fields[2].Type != datatype.FieldTypeBigInt || result.Fields[3].Type != datatype.FieldTypeDecimal || result.Fields[3].Precision != 38 || result.Fields[3].Scale != 18 {
			t.Fatalf("native MySQL output schema lost: %+v", result.Fields)
		}
		wire, err := datatype.QueryRowsForJSON(result.Fields, result.Rows)
		if err != nil {
			t.Fatal(err)
		}
		if predicate == "small_value<0" {
			if len(wire) != 0 {
				t.Fatal("empty result contains rows")
			}
			continue
		}
		if len(wire) != 1 || wire[0]["small_alias"] != int64(2) || wire[0]["signed_alias"] != "9007199254740993" || wire[0]["unsigned_alias"] != "18446744073709551615" || wire[0]["amount"] != "12345678901234567890.123456789012345678" || wire[0]["optional_value"] != nil {
			t.Fatal("native MySQL numeric JSON representation changed")
		}
	}
	// Geometry aliases and expressions use the same canonical EWKB as cursors.
	binaryResult, err := provider.ExecuteSQL(ctx, info, "SELECT UNHEX('00FF80') AS payload, UNHEX('') AS empty_payload, CAST(NULL AS BINARY) AS null_payload, ST_GeomFromText('POINT(1 2)',4326) AS shape_alias, CAST('中文' AS CHAR) AS label", plugin.QueryOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	row := binaryResult.Rows[0]
	payload, ok := row["payload"].([]byte)
	if !ok || !bytes.Equal(payload, []byte{0, 255, 128}) {
		t.Fatalf("binary expression changed: %T", row["payload"])
	}
	shape, ok := row["shape_alias"].([]byte)
	if !ok || len(shape) == 0 || binaryResult.Fields[3].Type != datatype.FieldTypeGeometry {
		t.Fatalf("geometry result lost type: %+v", binaryResult.Fields)
	}
	var expectedWKB []byte
	if err := db.QueryRowContext(ctx, "SELECT ST_AsWKB(ST_GeomFromText('POINT(1 2)',4326), 'axis-order=long-lat')").Scan(&expectedWKB); err != nil {
		t.Fatal(err)
	}
	expectedEWKB, err := mysqlWKBToEWKB(expectedWKB, 4326)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expectedEWKB, shape) {
		t.Fatal("query geometry changed native XY axis order")
	}
	geometry, err := ewkb.Unmarshal(shape)
	if err != nil || geometry.SRID() != 4326 {
		t.Fatalf("MySQL geometry not normalized: %v", err)
	}
	prepared, err := provider.PrepareQuery(ctx, info, plugin.QueryRequest{Language: "sql", Query: "SELECT ST_GeomFromText('POINT(1 2)',4326) AS shape_alias", Options: plugin.QueryOptions{ReadOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := provider.OpenQueryReadSession(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := session.ReadBatch(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(batch.Rows[0]["shape_alias"].([]byte), shape) {
		t.Fatal("preview and continuous geometry differ")
	}
	if empty, ok := row["empty_payload"].([]byte); !ok || empty == nil || len(empty) != 0 || row["null_payload"] != nil || row["label"] != "中文" {
		t.Fatal("NULL, empty or text changed")
	}
	wire, err := datatype.QueryRowsForJSON(binaryResult.Fields, binaryResult.Rows)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded[0]["payload"] != base64.StdEncoding.EncodeToString(payload) || decoded[0]["shape_alias"].(map[string]interface{})["srid"] != float64(4326) || decoded[0]["empty_payload"] != "" || decoded[0]["null_payload"] != nil {
		t.Fatal("native binary JSON round trip changed")
	}
}
