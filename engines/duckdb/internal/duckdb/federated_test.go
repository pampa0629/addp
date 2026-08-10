package duckdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	commonquery "github.com/addp/common/query"
)

var testFederatedSessionOptions = FederatedSessionOptions{MemoryLimit: "128MB", Threads: 1}

func TestPrepareFederatedQueryRejectsUnregisteredLocalFileAccess(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private.csv")
	if err := os.WriteFile(path, []byte("secret\nnot-for-query-workbench\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	query := fmt.Sprintf("SELECT * FROM read_csv_auto('%s')", strings.ReplaceAll(path, "'", "''"))
	session, err := PrepareFederatedQueryWithEngines(context.Background(), query, nil, nil, testFederatedSessionOptions)
	if err != nil {
		t.Fatalf("PrepareFederatedQueryWithEngines() error = %v", err)
	}
	defer session.Close()

	rows, err := session.Conn.QueryContext(context.Background(), session.RewrittenSQL)
	if err == nil {
		rows.Close()
		t.Fatal("unregistered local file access must be rejected")
	}
}

func TestPrepareFederatedQueryDoesNotRequireObjectTablesForLocalSQL(t *testing.T) {
	t.Parallel()

	session, err := PrepareFederatedQueryWithEngines(context.Background(), "SELECT version()", nil, nil, testFederatedSessionOptions)
	if err != nil {
		t.Fatalf("PrepareFederatedQueryWithEngines() error = %v", err)
	}
	defer session.Close()
	var value string
	if err := session.Conn.QueryRowContext(context.Background(), session.RewrittenSQL).Scan(&value); err != nil {
		t.Fatalf("execute rewritten SQL: %v", err)
	}
	if value == "" {
		t.Fatal("DuckDB version must not be empty")
	}
}

func TestServicePaginationWithExistingLimitExecutes(t *testing.T) {
	t.Parallel()

	baseQuery := "SELECT * FROM (VALUES (1), (2)) AS business(id) LIMIT 10"
	serviceQuery := commonquery.PaginateQuerySQL(baseQuery, 10, 0)
	session, err := PrepareFederatedQueryWithEngines(context.Background(), serviceQuery, nil, nil, testFederatedSessionOptions)
	if err != nil {
		t.Fatalf("PrepareFederatedQueryWithEngines() error = %v", err)
	}
	defer session.Close()

	runtimeQuery := fmt.Sprintf("SELECT * FROM (%s) AS addp_query LIMIT 1000 OFFSET 0", session.RewrittenSQL)
	result, err := ExecuteQuery(context.Background(), session.Conn, runtimeQuery)
	if err != nil {
		t.Fatalf("ExecuteQuery() error = %v\nSQL: %s", err, runtimeQuery)
	}
	if result.RowCount != 2 {
		t.Fatalf("row count = %d, want 2", result.RowCount)
	}
}

func TestExecuteQueryBindsArgumentsWithoutSQLInterpolation(t *testing.T) {
	t.Parallel()

	session, err := PrepareFederatedQueryWithEngines(
		context.Background(),
		"SELECT value FROM (VALUES ('safe'), ('other')) AS business(value) WHERE value = ?",
		nil, nil, testFederatedSessionOptions,
	)
	if err != nil {
		t.Fatalf("PrepareFederatedQueryWithEngines() error = %v", err)
	}
	defer session.Close()

	argument := "safe' OR 1=1 --"
	result, err := ExecuteQuery(context.Background(), session.Conn, session.RewrittenSQL, argument)
	if err != nil {
		t.Fatalf("ExecuteQuery() error = %v", err)
	}
	if result.RowCount != 0 {
		t.Fatalf("row count = %d, want 0", result.RowCount)
	}
}

func TestDuckDBDescribeReturnsOutputColumns(t *testing.T) {
	t.Parallel()

	session, err := PrepareFederatedQueryWithEngines(
		context.Background(), "SELECT 1::BIGINT AS id, 'value'::VARCHAR AS name", nil, nil, testFederatedSessionOptions,
	)
	if err != nil {
		t.Fatalf("PrepareFederatedQueryWithEngines() error = %v", err)
	}
	defer session.Close()
	result, err := ExecuteQuery(context.Background(), session.Conn, "DESCRIBE "+session.RewrittenSQL)
	if err != nil {
		t.Fatalf("ExecuteQuery(DESCRIBE) error = %v", err)
	}
	if result.RowCount != 2 || result.Rows[0]["column_name"] != "id" || result.Rows[1]["column_name"] != "name" {
		t.Fatalf("describe rows = %#v", result.Rows)
	}
}

func TestPrepareFederatedQueryLocksSecurityConfiguration(t *testing.T) {
	t.Parallel()

	session, err := PrepareFederatedQueryWithEngines(context.Background(), "SELECT version()", nil, nil, testFederatedSessionOptions)
	if err != nil {
		t.Fatalf("PrepareFederatedQueryWithEngines() error = %v", err)
	}
	defer session.Close()

	if _, err := session.Conn.ExecContext(context.Background(), "SET enable_external_access = true"); err == nil {
		t.Fatal("security configuration must remain locked for user SQL")
	}
}

func TestPrepareFederatedQueryAppliesResourceLimitsBeforeLock(t *testing.T) {
	t.Parallel()

	session, err := PrepareFederatedQueryWithEngines(context.Background(), "SELECT 1", nil, nil, testFederatedSessionOptions)
	if err != nil {
		t.Fatalf("PrepareFederatedQueryWithEngines() error = %v", err)
	}
	defer session.Close()

	var threads int
	if err := session.Conn.QueryRowContext(context.Background(), "SELECT current_setting('threads')::INTEGER").Scan(&threads); err != nil {
		t.Fatalf("read DuckDB threads setting: %v", err)
	}
	if threads != testFederatedSessionOptions.Threads {
		t.Fatalf("DuckDB threads = %d, want %d", threads, testFederatedSessionOptions.Threads)
	}
	if _, err := session.Conn.ExecContext(context.Background(), "SET threads = 2"); err == nil {
		t.Fatal("resource configuration must remain locked for user SQL")
	}
}

func TestLockExternalAccessAllowsOnlyRegisteredPath(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	allowedPath := filepath.Join(directory, "allowed.csv")
	blockedPath := filepath.Join(directory, "blocked.csv")
	for _, path := range []string{allowedPath, blockedPath} {
		if err := os.WriteFile(path, []byte("value\n42\n"), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}
	db, err := OpenDB()
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("db.Conn() error = %v", err)
	}
	defer conn.Close()
	if err := lockExternalAccessWithAllowedLocations(context.Background(), conn, nil, []string{allowedPath}); err != nil {
		t.Fatalf("lockExternalAccessWithAllowedLocations() error = %v", err)
	}

	var value int
	allowedQuery := fmt.Sprintf("SELECT value FROM read_csv_auto('%s')", strings.ReplaceAll(allowedPath, "'", "''"))
	if err := conn.QueryRowContext(context.Background(), allowedQuery).Scan(&value); err != nil || value != 42 {
		t.Fatalf("allowed path query value=%d error=%v", value, err)
	}
	blockedQuery := fmt.Sprintf("SELECT value FROM read_csv_auto('%s')", strings.ReplaceAll(blockedPath, "'", "''"))
	if err := conn.QueryRowContext(context.Background(), blockedQuery).Scan(&value); err == nil {
		t.Fatal("path absent from the execution whitelist must be rejected")
	}
}

func TestAllowedObjectTableLocationsSeparatesDatasetsAndFiles(t *testing.T) {
	t.Parallel()

	directories, paths := allowedObjectTableLocations(map[string]map[string]string{
		"lake": {
			"partitioned": "bucket/lake/sales",
			"single":      "s3://bucket/lake/snapshot.parquet",
		},
		"lake_alias": {
			"partitioned": "bucket/lake/sales",
		},
	})
	if want := []string{"s3://bucket/lake/sales"}; !reflect.DeepEqual(directories, want) {
		t.Fatalf("directories = %#v, want %#v", directories, want)
	}
	if want := []string{"s3://bucket/lake/snapshot.parquet"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}
