package postgresql

import (
	"github.com/addp/common/query/sqlcompile/conformance"
	"testing"
)

func TestIntegrationPostgresAnalyticalExpressions(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	conformance.PreparedExpressions(t, &integerPreparedProvider{PostgreSQLPlugin: provider, compiler: conformance.RelationalFixtureCompiler{Expression: analyticalExpressionDialect{}, Result: analyticalResultDialect{}, Scan: analyticalScanDialect{}}}, info)
}

func TestIntegrationPostgresAnalyticalCalendar(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conformance.NativeCalendar(t, conn, analyticalExpressionDialect{}, nil)
	conformance.PreparedCalendar(t, &integerPreparedProvider{PostgreSQLPlugin: provider, compiler: conformance.RelationalFixtureCompiler{Expression: analyticalExpressionDialect{}, Result: analyticalResultDialect{}, Scan: analyticalScanDialect{}}}, info)
}

func TestIntegrationPostgresAnalyticalRelations(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	conformance.PreparedRelations(t, &integerPreparedProvider{PostgreSQLPlugin: provider, compiler: conformance.RelationalFixtureCompiler{Expression: analyticalExpressionDialect{}, Result: analyticalResultDialect{}, Scan: analyticalScanDialect{}}}, info)
}
