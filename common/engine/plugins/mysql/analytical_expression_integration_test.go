package mysql

import (
	"github.com/addp/common/engine/plugins/shared/analytical"
	"github.com/addp/common/query/sqlcompile/conformance"
	"testing"
)

func TestIntegrationMySQLAnalyticalExpressions(t *testing.T) {
	db, provider, info, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	info["database"] = database
	conformance.PreparedExpressions(t, provider, info)
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
	conformance.NativeCalendar(t, conn, analytical.MySQLCompatibleExpressionDialect{}, func(t *testing.T) {
		var warnings int
		if err := conn.QueryRowContext(t.Context(), "SHOW COUNT(*) WARNINGS").Scan(&warnings); err != nil || warnings != 0 {
			t.Fatalf("warnings=%d err=%v", warnings, err)
		}
	})
	conformance.PreparedCalendar(t, provider, info)
}

func TestIntegrationMySQLAnalyticalRelations(t *testing.T) {
	db, provider, info, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	info["database"] = database
	conformance.PreparedRelations(t, provider, info)
	t.Run("result requests", func(t *testing.T) { conformance.PreparedResultRequests(t, provider, info) })
}

func TestIntegrationMySQLAnalyticalDateBuckets(t *testing.T) {
	db, provider, info, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	info["database"] = database
	conformance.PreparedDateBuckets(t, provider, info)
}

func TestIntegrationMySQLAnalyticalText(t *testing.T) {
	db, provider, info, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	info["database"] = database
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(t.Context(), "SET lc_time_names = 'de_DE'"); err != nil {
		t.Fatal(err)
	}
	conformance.NativeText(t, conn, analytical.MySQLCompatibleExpressionDialect{}, func(t *testing.T) {
		var warnings int
		if err := conn.QueryRowContext(t.Context(), "SHOW COUNT(*) WARNINGS").Scan(&warnings); err != nil || warnings != 0 {
			t.Fatalf("warnings=%d err=%v", warnings, err)
		}
	})
	conformance.PreparedText(t, provider, info)
}
