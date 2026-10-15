package oracle

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
)

func TestIntegrationOracleDecimalTableWriteDefinition(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_DECIMAL_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_DECIMAL_INTEGRATION=1 to run Oracle decimal integration test")
	}
	t.Run("definition_and_evolution", testOracleDefinitionAndEvolution)
	t.Run("non_spatial_delete", testOracleNonSpatialDelete)
	t.Run("delete_metadata_guard", testOracleDeleteMetadataGuard)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	schema := strings.ToUpper(plugin.GetString(connInfo, "user"))
	table := "ADDP_DECIMAL_" + strings.ToUpper(uuid.NewString()[:8])
	path := plugin.TabularItemPath(93001, plugin.EngineCatalogTermSchema, schema, table)

	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer func() {
		qualified := commonquery.ForDialect("oracle").QualifiedTable(schema, table)
		_, _ = db.ExecContext(context.Background(), "DROP TABLE "+qualified+" PURGE")
	}()

	fields := []datatype.FieldInfo{
		{Name: "ID", Type: datatype.FieldTypeBigInt, Nullable: false},
		{Name: "AMOUNT", Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 6, Nullable: false},
	}
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatalf("PrepareTableWrite() error = %v", err)
	}

	var precision, scale int
	if err := db.QueryRowContext(ctx, `
		SELECT data_precision, data_scale
		  FROM user_tab_columns
		 WHERE table_name = :1 AND column_name = 'AMOUNT'
	`, table).Scan(&precision, &scale); err != nil {
		t.Fatalf("query Oracle decimal definition: %v", err)
	}
	if precision != 20 || scale != 6 {
		t.Fatalf("Oracle AMOUNT definition = NUMBER(%d,%d), want NUMBER(20,6)", precision, scale)
	}
}

// Runs inside the existing Hosted Oracle gate, using only owned synthetic
// tables. This is also a regression for Oracle's implicitly committed DDL.
func testOracleDefinitionAndEvolution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p := &OraclePlugin{}
	info := oracleIntegrationConnInfo(t)
	schema := strings.ToUpper(plugin.GetString(info, "user"))
	table := "ADDP_DEF_" + strings.ToUpper(uuid.NewString()[:8])
	path := plugin.TabularItemPath(93001, plugin.EngineCatalogTermSchema, schema, table)
	qualified := commonquery.ForDialect("oracle").QualifiedTable(schema, table)
	dsn, err := p.BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var occupied int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME = :1", table).Scan(&occupied); err != nil || occupied != 0 {
		t.Fatalf("fixture name occupied: count=%d err=%v", occupied, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Own only this non-spatial fixture. DeleteResource's separate spatial
		// metadata lifecycle is outside this definition/evolution regression.
		if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME=:1", table).Scan(&occupied); err != nil {
			t.Error(err)
		} else if occupied == 1 {
			if _, err := db.ExecContext(cleanup, "DROP TABLE "+qualified+" PURGE"); err != nil {
				t.Error(err)
			}
		}
		if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME = :1", table).Scan(&occupied); err != nil || occupied != 0 {
			t.Errorf("fixture residue: count=%d err=%v", occupied, err)
		}
	}()
	for _, field := range []datatype.FieldInfo{
		{Name: "BAD", Type: datatype.FieldTypeArray},
		{Name: "BAD", Type: datatype.FieldTypeTime},
		{Name: "BAD", Type: datatype.FieldTypeBytes, PrimaryKey: true},
		{Name: "BAD", Type: datatype.FieldTypeDecimal, Precision: 39},
		{Name: "BAD", Type: datatype.FieldTypeGeometry},
	} {
		if err := p.PrepareTableWrite(ctx, info, path, plugin.TableWriteOptions{Fields: []datatype.FieldInfo{field}}); err == nil {
			t.Fatalf("unsupported definition accepted: %+v", field)
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME = :1", table).Scan(&occupied); err != nil || occupied != 0 {
			t.Fatalf("rejection created a target: count=%d err=%v", occupied, err)
		}
	}
	fields := []datatype.FieldInfo{
		{Name: "ID", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "DOCUMENT", Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "PAYLOAD", Type: datatype.FieldTypeBytes, Nullable: true},
		{Name: "AMOUNT", Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 6, Nullable: true},
	}
	if err := p.PrepareTableWrite(ctx, info, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatal(err)
	}
	session, err := p.OpenTableWriteSession(ctx, info, path, plugin.TableWriteSessionOptions{Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Abort(ctx)
	if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"ID": int64(1), "DOCUMENT": map[string]interface{}{"sentinel": true}, "PAYLOAD": []byte{0, 255}, "AMOUNT": "1.000001"}}}); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var objectID int64
	if err := db.QueryRowContext(ctx, "SELECT OBJECT_ID FROM USER_OBJECTS WHERE OBJECT_NAME=:1 AND OBJECT_TYPE='TABLE'", table).Scan(&objectID); err != nil {
		t.Fatal(err)
	}
	// A valid addition precedes each late error. No earlier ALTER may escape.
	for _, scenario := range []string{"incompatible_type", "required_addition", "primary_addition", "decimal_scale", "missing_primary"} {
		evolving := append([]datatype.FieldInfo{{Name: "FIRST_ADDITION", Type: datatype.FieldTypeJSON, Nullable: true}}, fields...)
		switch scenario {
		case "incompatible_type":
			evolving[2].Type = datatype.FieldTypeInt
		case "required_addition":
			evolving = append(evolving, datatype.FieldInfo{Name: "LAST_ADDITION", Type: datatype.FieldTypeBigInt})
		case "primary_addition":
			evolving = append(evolving, datatype.FieldInfo{Name: "LAST_ADDITION", Type: datatype.FieldTypeBigInt, Nullable: true, PrimaryKey: true})
		case "decimal_scale":
			evolving[4].Scale = 5
		case "missing_primary":
			evolving[2].PrimaryKey = true
		}
		if err := p.PrepareTableWrite(ctx, info, path, plugin.TableWriteOptions{Fields: evolving}); err == nil {
			t.Fatalf("accepted %s", scenario)
		}
		var columns, rows int
		var currentID int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_TAB_COLUMNS WHERE TABLE_NAME=:1", table).Scan(&columns); err != nil || columns != len(fields) {
			t.Fatalf("%s changed columns: %d %v", scenario, columns, err)
		}
		if err := db.QueryRowContext(ctx, "SELECT OBJECT_ID FROM USER_OBJECTS WHERE OBJECT_NAME=:1 AND OBJECT_TYPE='TABLE'", table).Scan(&currentID); err != nil || currentID != objectID {
			t.Fatalf("%s replaced target: %d %v", scenario, currentID, err)
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified+" WHERE \"ID\"=1 AND JSON_VALUE(\"DOCUMENT\", '$.sentinel')='true' AND DBMS_LOB.GETLENGTH(\"PAYLOAD\")=2 AND \"AMOUNT\"=1.000001").Scan(&rows); err != nil || rows != 1 {
			t.Fatalf("%s changed sentinel: %d %v", scenario, rows, err)
		}
	}
	// The same plan accepts multiple nullable additions, then remains idempotent.
	fields = append(fields, datatype.FieldInfo{Name: "ADDED_JSON", Type: datatype.FieldTypeJSON, Nullable: true}, datatype.FieldInfo{Name: "ADDED_BYTES", Type: datatype.FieldTypeBytes, Nullable: true})
	for i := 0; i < 2; i++ {
		if err := p.PrepareTableWrite(ctx, info, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
			t.Fatal(err)
		}
	}
	var columns int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_TAB_COLUMNS WHERE TABLE_NAME=:1", table).Scan(&columns); err != nil || columns != len(fields) {
		t.Fatalf("missing additions: %d %v", columns, err)
	}
}
