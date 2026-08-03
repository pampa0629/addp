package duckdb

import (
	"context"
	"testing"
)

func TestEnsureDuckDBExtensionDoesNotInstallMissingExtension(t *testing.T) {
	db, err := OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := ensureDuckDBExtension(context.Background(), conn, "missing_addp_extension", "missing_addp_extension"); err == nil {
		t.Fatal("missing extension unexpectedly loaded")
	}
}
