package testpg

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
)

const databaseEnv = "ADDP_TEST_POSTGRES_DATABASE"

func ConnInfoFromEnv(t testing.TB) engineplugin.ConnectionInfo {
	t.Helper()

	database := strings.TrimSpace(os.Getenv(databaseEnv))
	if database == "" {
		t.Fatalf("%s must be set to a dedicated PostgreSQL test database, for example addp_test", databaseEnv)
	}
	if database == "addp" {
		t.Fatalf("%s=%q points to the development database; use a dedicated test database", databaseEnv, database)
	}

	return engineplugin.ConnectionInfo{
		"host":     env("ADDP_TEST_POSTGRES_HOST", "localhost"),
		"port":     env("ADDP_TEST_POSTGRES_PORT", "15432"),
		"user":     env("ADDP_TEST_POSTGRES_USER", "addp"),
		"password": env("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		"database": database,
		"sslmode":  env("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	}
}

func DropSchemasWithPrefixes(t testing.TB, ctx context.Context, db *sql.DB, prefixes ...string) {
	t.Helper()

	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			t.Fatalf("test schema cleanup prefix must not be empty")
		}

		rows, err := db.QueryContext(ctx, `
			SELECT nspname
			FROM pg_namespace
			WHERE nspname LIKE $1 ESCAPE '\'
		`, escapeLike(prefix)+"%")
		if err != nil {
			t.Fatalf("list test schemas with prefix %q failed: %v", prefix, err)
		}

		var schemas []string
		for rows.Next() {
			var schema string
			if err := rows.Scan(&schema); err != nil {
				_ = rows.Close()
				t.Fatalf("scan test schema with prefix %q failed: %v", prefix, err)
			}
			schemas = append(schemas, schema)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close test schema rows with prefix %q failed: %v", prefix, err)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate test schemas with prefix %q failed: %v", prefix, err)
		}

		for _, schema := range schemas {
			if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+quoteIdentifier(schema)+" CASCADE"); err != nil {
				t.Fatalf("drop stale test schema %q failed: %v", schema, err)
			}
		}
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func CreateSchema(t testing.TB, ctx context.Context, db *sql.DB, name string) {
	t.Helper()

	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %s", quoteIdentifier(name))); err != nil {
		t.Fatalf("create test schema failed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", quoteIdentifier(name))); err != nil {
			t.Errorf("drop test schema %q failed: %v", name, err)
		}
	})
}
