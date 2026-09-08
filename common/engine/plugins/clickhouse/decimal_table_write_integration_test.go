package clickhouse

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

func TestIntegrationClickHouseDecimalTableWriteDefinition(t *testing.T) {
	if os.Getenv("ADDP_CLICKHOUSE_INTEGRATION") != "1" {
		t.Skip("set ADDP_CLICKHOUSE_INTEGRATION=1 to run ClickHouse integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connInfo := clickhouseIntegrationConnInfo(t)
	p := &ClickHousePlugin{}
	database := plugin.GetString(connInfo, "database")
	table := "addp_decimal_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	path := plugin.TabularItemPath(93003, "database", database, table)
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
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var nativeType string
	if err := db.QueryRowContext(ctx, `
		SELECT type
		FROM system.columns
		WHERE database = ? AND table = ? AND name = 'amount'
	`, database, table).Scan(&nativeType); err != nil {
		t.Fatalf("query ClickHouse decimal definition: %v", err)
	}
	precision, scale, ok := clickhouseDecimalPrecisionScale(nativeType)
	if !ok || precision != 20 || scale != 6 {
		t.Fatalf("ClickHouse amount definition = %q, want Decimal(20,6)", nativeType)
	}
}

func clickhouseIntegrationConnInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	portText := strings.TrimSpace(os.Getenv("ADDP_TEST_CLICKHOUSE_PORT"))
	if portText == "" {
		portText = "9000"
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid ClickHouse integration port %q: %v", portText, err)
	}
	valueOr := func(key, fallback string) string {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
		return fallback
	}
	return plugin.ConnectionInfo{
		"host":     valueOr("ADDP_TEST_CLICKHOUSE_HOST", "127.0.0.1"),
		"port":     port,
		"database": valueOr("ADDP_TEST_CLICKHOUSE_DATABASE", "addp_clickhouse_disposable"),
		"user":     valueOr("ADDP_TEST_CLICKHOUSE_USER", "default"),
		"password": valueOr("ADDP_TEST_CLICKHOUSE_PASSWORD", "addp_clickhouse_password"),
	}
}
