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
