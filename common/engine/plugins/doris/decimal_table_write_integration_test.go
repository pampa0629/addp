package doris

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestIntegrationDorisDecimalTableWriteDefinition(t *testing.T) {
	if os.Getenv("ADDP_DORIS_INTEGRATION") != "1" {
		t.Skip("set ADDP_DORIS_INTEGRATION=1 to run Doris integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connInfo := dorisIntegrationConnInfo(t)
	p := &DorisPlugin{}
	database := plugin.GetString(connInfo, "database")
	table := "addp_decimal_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	path := plugin.TabularItemPath(93002, "database", database, table)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = p.DeleteResource(cleanupCtx, connInfo, path)
	}()

	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 6, Nullable: false},
	}
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatalf("PrepareTableWrite() error = %v", err)
	}

	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var precision, scale int
	if err := db.QueryRowContext(ctx, `
		SELECT numeric_precision, numeric_scale
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ? AND column_name = 'amount'
	`, database, table).Scan(&precision, &scale); err != nil {
		t.Fatalf("query Doris decimal definition: %v", err)
	}
	if precision != 20 || scale != 6 {
		t.Fatalf("Doris amount definition = DECIMAL(%d,%d), want DECIMAL(20,6)", precision, scale)
	}
}

func dorisIntegrationConnInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	portText := strings.TrimSpace(os.Getenv("ADDP_TEST_DORIS_PORT"))
	if portText == "" {
		portText = "9030"
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid Doris integration port %q: %v", portText, err)
	}
	valueOr := func(key, fallback string) string {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
		return fallback
	}
	return plugin.ConnectionInfo{
		"host":     valueOr("ADDP_TEST_DORIS_HOST", "127.0.0.1"),
		"port":     port,
		"database": valueOr("ADDP_TEST_DORIS_DATABASE", "addp_doris_disposable"),
		"user":     valueOr("ADDP_TEST_DORIS_USER", "root"),
		"password": valueOr("ADDP_TEST_DORIS_PASSWORD", ""),
	}
}
