package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/addp/common/authorization"
)

func TestRunCheckWritesCanonicalCatalogReport(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(
		[]string{"--check", "--repository-root", testRepositoryRoot(t)},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}
	var report authorization.AuthorizationCatalogReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if len(report.Permissions) != 295 || len(report.Roles) != 16 {
		t.Fatalf("report counts = permissions:%d roles:%d", len(report.Permissions), len(report.Roles))
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRequiresExactlyOneModeAndRepositoryRoot(t *testing.T) {
	if err := run(nil, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("run() error = nil, want mode error")
	}
	if err := run([]string{"--check"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("run() error = nil, want --repository-root error")
	}
	if err := run([]string{"--check", "--repository-root", ".", "extra"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("run() error = nil, want positional argument error")
	}
	if err := run([]string{"--check", "--generate-owner-constants", "--repository-root", "."}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("run() error = nil, want mutually exclusive mode error")
	}
	if err := run([]string{"--generate-tool-catalog", "--check-tool-catalog", "--repository-root", "."}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("run() error = nil, want Tool catalog mode error")
	}
}

func TestRunCheckToolCatalogWritesSummary(t *testing.T) {
	root := testRepositoryRoot(t)
	var stdout bytes.Buffer
	if err := run(
		[]string{"--check-tool-catalog", "--repository-root", root},
		&stdout,
		&bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(authorization.SystemToolAuthorizationCatalogRelativePath)) {
		t.Fatalf("Tool catalog summary = %q", stdout.String())
	}
}

func TestRunCoverageReportIsComplete(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(
		[]string{"--coverage-report", "--repository-root", testRepositoryRoot(t)},
		&stdout,
		&bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	var report authorization.AuthorizationCoverageReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != authorization.AuthorizationCoverageReportSchemaVersion {
		t.Fatalf("coverage schema version = %q", report.SchemaVersion)
	}
	if !report.Complete || len(report.Issues) != 0 {
		t.Fatalf("repository coverage = %#v, want complete", report)
	}
}

func TestRunCheckSQLSeedWritesSummary(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(
		[]string{"--check-sql-seed", "--repository-root", testRepositoryRoot(t)},
		&stdout,
		&bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(authorization.IAMCatalogSeedRelativePath)) {
		t.Fatalf("SQL seed summary = %q", stdout.String())
	}
}

func TestRunReportsOutputWriteFailure(t *testing.T) {
	err := run(
		[]string{"--check", "--repository-root", testRepositoryRoot(t)},
		errorWriter{},
		&bytes.Buffer{},
	)
	if err == nil || !errors.Is(err, errWrite) {
		t.Fatalf("run() error = %v, want write error", err)
	}
}

var errWrite = errors.New("write failed")

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errWrite
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", ".."))
}
