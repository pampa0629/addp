package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationMySQLTableUpsert(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)

	ctx := context.Background()
	path := mysqlIntegrationTablePath(database, "orders")
	opts := plugin.TableUpsertOptions{
		Fields: []datatype.FieldInfo{
			{Name: "tenant_id", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
		},
		Keys: []string{"tenant_id", "id"},
	}
	if err := mysqlPlugin.PrepareTableUpsert(ctx, connInfo, path, opts); err != nil {
		t.Fatalf("PrepareTableUpsert failed: %v", err)
	}

	batch := &plugin.BatchData{
		Fields: opts.Fields,
		Rows: []map[string]interface{}{
			{"tenant_id": int64(1), "id": int64(10), "name": "first"},
		},
	}
	if err := mysqlPlugin.UpsertBatch(ctx, connInfo, path, batch, opts); err != nil {
		t.Fatalf("initial UpsertBatch failed: %v", err)
	}
	batch.Rows[0]["name"] = "updated"
	if err := mysqlPlugin.UpsertBatch(ctx, connInfo, path, batch, opts); err != nil {
		t.Fatalf("repeated UpsertBatch failed: %v", err)
	}

	var count int
	var name string
	if err := db.QueryRowContext(ctx,
		"SELECT count(*), max(name) FROM "+mysqlDialect().QualifiedTable(database, "orders")+" WHERE tenant_id = ? AND id = ?",
		1, 10,
	).Scan(&count, &name); err != nil {
		t.Fatalf("query upserted row: %v", err)
	}
	if count != 1 || name != "updated" {
		t.Fatalf("upserted row count=%d name=%q, want count=1 name=updated", count, name)
	}
}

func TestIntegrationMySQLTableUpsertRejectsAmbiguousUniqueConstraints(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)

	ctx := context.Background()
	qualified := mysqlDialect().QualifiedTable(database, "accounts")
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+" (id BIGINT NOT NULL, email VARCHAR(255) NOT NULL, PRIMARY KEY (id), UNIQUE KEY unique_email (email)) ENGINE=InnoDB"); err != nil {
		t.Fatalf("create ambiguous target: %v", err)
	}
	opts := plugin.TableUpsertOptions{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "email", Type: datatype.FieldTypeString, Nullable: false},
		},
		Keys: []string{"id"},
	}
	if err := mysqlPlugin.PrepareTableUpsert(ctx, connInfo, mysqlIntegrationTablePath(database, "accounts"), opts); err == nil {
		t.Fatal("PrepareTableUpsert accepted a target with a competing unique constraint")
	}
}

func TestIntegrationMySQLTableUpsertRejectsNullableKey(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)

	ctx := context.Background()
	qualified := mysqlDialect().QualifiedTable(database, "nullable_keys")
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+" (id BIGINT NULL, name TEXT NULL, UNIQUE KEY unique_id (id)) ENGINE=InnoDB"); err != nil {
		t.Fatalf("create nullable-key target: %v", err)
	}
	opts := plugin.TableUpsertOptions{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: true},
			{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
		},
		Keys: []string{"id"},
	}
	if err := mysqlPlugin.PrepareTableUpsert(ctx, connInfo, mysqlIntegrationTablePath(database, "nullable_keys"), opts); err == nil {
		t.Fatal("PrepareTableUpsert accepted a nullable upsert key")
	}
}

func openMySQLIntegrationServer(t *testing.T) (*sql.DB, *MySQLPlugin, plugin.ConnectionInfo) {
	t.Helper()
	if os.Getenv("ADDP_MYSQL_INTEGRATION") != "1" {
		t.Skip("set ADDP_MYSQL_INTEGRATION=1 to run MySQL integration tests")
	}
	password := os.Getenv("ADDP_TEST_MYSQL_PASSWORD")
	if password == "" {
		t.Skip("set ADDP_TEST_MYSQL_PASSWORD to run MySQL integration tests")
	}
	host := envOrDefault("ADDP_TEST_MYSQL_HOST", "127.0.0.1")
	port := envOrDefault("ADDP_TEST_MYSQL_PORT", "3306")
	user := envOrDefault("ADDP_TEST_MYSQL_USER", "root")
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatalf("invalid ADDP_TEST_MYSQL_PORT: %v", err)
	}
	connInfo := plugin.ConnectionInfo{"host": host, "port": port, "user": user, "password": password}
	mysqlPlugin := &MySQLPlugin{}
	dsn, err := mysqlPlugin.serverDSN(connInfo)
	if err != nil {
		t.Fatalf("build integration DSN: %v", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open MySQL integration database: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		t.Fatalf("ping MySQL integration database: %v", err)
	}
	return db, mysqlPlugin, connInfo
}

func openMySQLUpsertIntegration(t *testing.T) (*sql.DB, *MySQLPlugin, plugin.ConnectionInfo, string) {
	db, mysqlPlugin, connInfo := openMySQLIntegrationServer(t)
	manifestPath := os.Getenv("ADDP_MYSQL_PROVIDER_FIXTURE_MANIFEST")
	if manifestPath == "" {
		_ = db.Close()
		t.Fatal("use the standard Common MySQL gate to own disposable fixtures")
	}
	database := fmt.Sprintf("addp_common_mysql_it_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(context.Background(), "CREATE DATABASE "+mysqlDialect().QuoteIdentifier(database)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		db.Close()
		t.Fatalf("create MySQL integration database: %v", err)
	}
	t.Logf("owned MySQL fixture: %s", database)
	manifest, err := os.OpenFile(manifestPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		_, err = fmt.Fprintln(manifest, database)
		err = errors.Join(err, manifest.Close())
	}
	if err != nil {
		_, dropErr := db.ExecContext(context.Background(), "DROP DATABASE "+mysqlDialect().QuoteIdentifier(database))
		_ = db.Close()
		t.Fatalf("record owned MySQL fixture: %v; immediate cleanup: %v", err, dropErr)
	}
	return db, mysqlPlugin, connInfo, database
}

func mysqlIntegrationTablePath(database, table string) plugin.EngineCatalogPath {
	return plugin.EngineCatalogPath{Segments: []plugin.EngineCatalogSegment{
		{Term: "database", Kind: plugin.EngineCatalogRoleBranch, Name: database},
		{Term: "table", Kind: plugin.EngineCatalogRoleLeaf, Name: table},
	}}
}

func dropMySQLIntegrationDatabase(db *sql.DB, database string) {
	if db == nil || database == "" {
		return
	}
	_, _ = db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+mysqlDialect().QuoteIdentifier(database))
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

var mysqlOwnedFixtureName = regexp.MustCompile(`^addp_common_mysql_it_[0-9]{19}$`)

func parseMySQLOwnedFixtures(data string) ([]string, error) {
	var names []string
	seen := map[string]bool{}
	for _, name := range strings.Split(strings.TrimSuffix(data, "\n"), "\n") {
		if name == "" && data == "" {
			continue
		}
		if !mysqlOwnedFixtureName.MatchString(name) {
			return nil, fmt.Errorf("invalid owned MySQL fixture name")
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, nil
}

func TestIntegrationMySQLProviderFixtureCleanup(t *testing.T) {
	path := os.Getenv("ADDP_MYSQL_PROVIDER_FIXTURE_MANIFEST")
	if path == "" {
		t.Skip("standard gate owns fixture cleanup")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	names, err := parseMySQLOwnedFixtures(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		return
	}
	db, _, _ := openMySQLIntegrationServer(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for _, name := range names {
		if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+mysqlDialect().QuoteIdentifier(name)); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?", name).Scan(&count); err != nil || count != 0 {
			t.Fatalf("owned MySQL cleanup not proven: count=%d error=%v", count, err)
		}
		t.Logf("owned fixture removed; zero residual: %s", name)
	}
}

// Recovery discovery reads only the restricted test catalog, never business rows.
func TestIntegrationMySQLProviderFixtureInspect(t *testing.T) {
	if os.Getenv("ADDP_MYSQL_PROVIDER_FIXTURE_INSPECT") != "1" {
		t.Skip("explicit standard recovery inspection only")
	}
	db, _, _ := openMySQLIntegrationServer(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT schema_name FROM information_schema.schemata WHERE schema_name REGEXP '^addp_common_mysql_it_[0-9]{19}$' ORDER BY schema_name")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		tables, err := db.QueryContext(ctx, "SELECT table_name,COALESCE(CAST(create_time AS CHAR),'') FROM information_schema.tables WHERE table_schema=? ORDER BY table_name", name)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("test catalog database: %s", name)
		for tables.Next() {
			var table, created string
			if err := tables.Scan(&table, &created); err != nil {
				t.Fatal(err)
			}
			t.Logf("test catalog table: %s.%s created=%s", name, table, created)
		}
		if err := errors.Join(tables.Err(), tables.Close()); err != nil {
			t.Fatal(err)
		}
	}
}
