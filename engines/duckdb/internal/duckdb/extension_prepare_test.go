package duckdb

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareRequiredExtensionsDownloadsOfficialLayout(t *testing.T) {
	payload := []byte("signed-extension-fixture")
	var archive bytes.Buffer
	writer := gzip.NewWriter(&archive)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(archive.Bytes())
	}))
	defer server.Close()

	output := t.TempDir()
	if err := PrepareRequiredExtensions(context.Background(), server.Client(), server.URL, "test_platform", output); err != nil {
		t.Fatalf("PrepareRequiredExtensions() error = %v", err)
	}
	if requests != len(RequiredExtensionFileNames()) {
		t.Fatalf("requests = %d", requests)
	}
	for _, fileName := range RequiredExtensionFileNames() {
		content, err := os.ReadFile(filepath.Join(output, fileName))
		if err != nil || !bytes.Equal(content, payload) {
			t.Fatalf("extension %s content=%q error=%v", fileName, content, err)
		}
	}

	if err := PrepareRequiredExtensions(context.Background(), server.Client(), server.URL, "test_platform", output); err != nil {
		t.Fatalf("second PrepareRequiredExtensions() error = %v", err)
	}
	if requests != len(RequiredExtensionFileNames()) {
		t.Fatalf("existing extensions were downloaded again: requests=%d", requests)
	}
}

func TestDuckDBVersionMatchesDriver(t *testing.T) {
	db, err := OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version string
	if err := db.QueryRow("SELECT version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != DuckDBVersion {
		t.Fatalf("driver DuckDB version = %q, extension version = %q", version, DuckDBVersion)
	}
}
