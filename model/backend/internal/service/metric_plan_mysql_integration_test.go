package service

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/mysql"
	"github.com/addp/common/query"
)

// The root Model MySQL gate exclusively creates and removes this test database.
func TestIntegrationMySQLMetricSemantics(t *testing.T) {
	if os.Getenv("ADDP_MYSQL_INTEGRATION") != "1" {
		t.Skip("requires the Model MySQL gate")
	}
	if os.Getenv("ADDP_TEST_MYSQL_PASSWORD") == "" {
		t.Fatal("ADDP_TEST_MYSQL_PASSWORD is required")
	}
	setting := func(name, fallback string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return fallback
	}
	provider := &mysql.MySQLPlugin{}
	conn := plugin.ConnectionInfo{"host": setting("ADDP_TEST_MYSQL_HOST", "127.0.0.1"), "port": setting("ADDP_TEST_MYSQL_PORT", "3306"), "user": setting("ADDP_TEST_MYSQL_USER", "root"), "password": os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), "database": "mysql"}
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
	database := fmt.Sprintf("addp_model_mysql_it_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE DATABASE " + dialect.QuoteIdentifier(database) + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP DATABASE " + dialect.QuoteIdentifier(database)); err != nil {
			t.Errorf("clean test database: %v", err)
		}
	})
	conn["database"] = database
	runMetricGolden(t, provider, conn, db, database, dialect)
}
