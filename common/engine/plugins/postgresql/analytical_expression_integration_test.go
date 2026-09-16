package postgresql

import (
	"github.com/addp/common/query/sqlcompile/conformance"
	"testing"
)

func TestIntegrationPostgresAnalyticalExpressions(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	conformance.PreparedExpressions(t, provider, info)
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
	conformance.PreparedCalendar(t, provider, info)
}

func TestIntegrationPostgresAnalyticalRelations(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	conformance.PreparedRelations(t, provider, info)
	t.Run("result requests", func(t *testing.T) { conformance.PreparedResultRequests(t, provider, info) })
}

func TestIntegrationPostgresAnalyticalDateBuckets(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	conformance.PreparedDateBuckets(t, provider, info)
}

func TestIntegrationPostgresAnalyticalText(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(t.Context(), "SET DateStyle = 'SQL, DMY'"); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(t.Context(), "SET TIME ZONE 'Pacific/Kiritimati'"); err != nil {
		t.Fatal(err)
	}
	conformance.NativeText(t, conn, analyticalExpressionDialect{}, nil)
	conformance.PreparedText(t, provider, info)
}
