package scanruntime

import (
	"io"
	"log/slog"
	"os"
	"strconv"
	"testing"

	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/repository"
	"gorm.io/gorm"
)

func TestPostgresWorkerScanFactAuthority(t *testing.T) {
	if os.Getenv("ADDP_TEST_PG_FACT_AUTHORITY") != "1" {
		t.Skip("requires the isolated Meta fact authority gate")
	}
	port, err := strconv.Atoi(os.Getenv("POSTGRES_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := bootstrap.OpenDatabase(database.Config{Host: "127.0.0.1", Port: os.Getenv("POSTGRES_PORT"),
		User: "meta_gate_worker", Password: os.Getenv("ADDP_TEST_META_WORKER_PASSWORD"),
		DBName: "addp_test", Schema: "meta", SSLMode: "disable"}, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := repository.NewScanRepository(db)
	runtime := NewDatabaseRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	resource := &commonmodels.Engine{ID: 101, Name: "source", EngineType: "postgresql", ConnectionInfo: commonmodels.ConnectionInfo{
		"host": "127.0.0.1", "port": port, "database": "addp_test", "user": "source_gate_reader", "password": os.Getenv("ADDP_TEST_META_SOURCE_PASSWORD"), "sslmode": "disable",
	}}
	n, tables, fields, err := runtime.ScanNamespace(t.Context(), &postgresql.PostgreSQLPlugin{}, resource, 7, 101, "scan_source", "deep", true)
	wantFields := 2
	if os.Getenv("ADDP_TEST_META_RESCAN") == "1" {
		wantFields = 3
	}
	if err != nil || n != 1 || tables != 1 || fields != wantFields {
		t.Fatalf("scan returned namespaces=%d tables=%d fields=%d: %v", n, tables, fields, err)
	}
	t.Logf("scan_source returned one table and %d fields using the restricted Worker login", fields)
}
