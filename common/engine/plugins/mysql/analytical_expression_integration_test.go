package mysql

import (
	"github.com/addp/common/query/sqlcompile/conformance"
	"testing"
)

func TestIntegrationMySQLAnalyticalExpressions(t *testing.T) {
	db, provider, info, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	info["database"] = database
	conformance.PreparedExpressions(t, &integerPreparedProvider{MySQLPlugin: provider, compiler: conformance.RelationalFixtureCompiler{Expression: analyticalExpressionDialect{}, Result: analyticalResultDialect{}}}, info)
}

func TestIntegrationMySQLAnalyticalCalendar(t *testing.T) {
	db, provider, info, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	info["database"] = database
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conformance.NativeCalendar(t, conn, analyticalExpressionDialect{}, func(t *testing.T) {
		var warnings int
		if err := conn.QueryRowContext(t.Context(), "SHOW COUNT(*) WARNINGS").Scan(&warnings); err != nil || warnings != 0 {
			t.Fatalf("warnings=%d err=%v", warnings, err)
		}
	})
	conformance.PreparedCalendar(t, &integerPreparedProvider{MySQLPlugin: provider, compiler: conformance.RelationalFixtureCompiler{Expression: analyticalExpressionDialect{}, Result: analyticalResultDialect{}}}, info)
}

func TestIntegrationMySQLAnalyticalRelations(t *testing.T) {
	db, provider, info, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	info["database"] = database
	conformance.PreparedRelations(t, &integerPreparedProvider{MySQLPlugin: provider, compiler: conformance.RelationalFixtureCompiler{Expression: analyticalExpressionDialect{}, Result: analyticalResultDialect{}}}, info)
}
