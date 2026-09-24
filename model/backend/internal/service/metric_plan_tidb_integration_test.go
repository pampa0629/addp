package service

import (
	"database/sql"
	"os"
	"regexp"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/tidb"
	"github.com/addp/common/query"
)

// The standard TiDB gate owns the isolated server and this database's lifecycle.
func TestIntegrationTiDBMetricSemantics(t *testing.T) {
	if os.Getenv("ADDP_TIDB_INTEGRATION") != "1" {
		t.Skip("requires the standard TiDB gate")
	}
	database := os.Getenv("ADDP_TEST_TIDB_DATABASE")
	if !regexp.MustCompile(`^[a-zA-Z0-9_]*disposable[a-zA-Z0-9_]*$`).MatchString(database) {
		t.Fatal("TiDB metric gate requires a disposable database identifier")
	}
	provider := &tidb.Plugin{}
	conn := plugin.ConnectionInfo{"host": os.Getenv("ADDP_TEST_TIDB_HOST"), "port": os.Getenv("ADDP_TEST_TIDB_PORT"), "user": os.Getenv("ADDP_TEST_TIDB_USER"), "password": os.Getenv("ADDP_TEST_TIDB_PASSWORD"), "database": "mysql"}
	dsn, err := provider.BuildDSN(conn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dialect := query.ForDialect(query.DialectMySQL)
	// Refuse an existing database; cleanup must never claim another run's data.
	if _, err := db.ExecContext(t.Context(), "CREATE DATABASE "+dialect.QuoteIdentifier(database)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP DATABASE " + dialect.QuoteIdentifier(database)); err != nil {
			t.Errorf("clean TiDB metric database: %v", err)
		}
	})
	t.Cleanup(func() { _ = plugin.ClosePool(2) })
	conn["database"] = database
	caps, err := provider.ResolveCapabilities(t.Context(), conn, provider.Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	if caps.Compute.Query.Analytical == nil || !caps.Compute.Query.Analytical.Supported {
		t.Fatal("TiDB analytical instance is not certified")
	}
	runMetricGolden(t, provider, conn, db, database, dialect)
}
