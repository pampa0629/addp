package mysql

import (
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
		wire, err := datatype.RowsForJSON(result.Fields, result.Rows)
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
}
