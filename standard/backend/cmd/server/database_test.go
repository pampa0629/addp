package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/schema"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Run the real main in a child process so its fatal exit is observable.
func TestStandardServerProcess(t *testing.T) {
	if os.Getenv("STANDARD_TEST_RUN_MAIN") == "1" {
		os.Args = os.Args[:1]
		main()
	}
}

func requireStartupExit(t *testing.T, expected string) {
	t.Helper()
	t.Setenv("STANDARD_TEST_RUN_MAIN", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStandardServerProcess$")
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), expected) {
		t.Fatalf("expected startup exit 1 at %q, got %v: %s", expected, err, output)
	}
	t.Logf("Standard main exited 1 at %q", expected)
}

func TestDatabaseStartupFailure(t *testing.T) {
	t.Setenv("PROJECT_ROOT", t.TempDir())
	// No PostgreSQL socket exists at this address, irrespective of local TCP services.
	socketDir, err := os.MkdirTemp("/tmp", "std-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(socketDir) })
	t.Setenv("POSTGRES_HOST", socketDir)
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_DB", "addp_test")
	t.Setenv("ADDP_DATABASE_RUNTIME_USER", "test")
	t.Setenv("ADDP_DATABASE_RUNTIME_PASSWORD", "test")
	for _, tc := range []struct{ provider, cause string }{
		{"unknown", `unsupported platform database provider "unknown"`},
		{"postgres", "dial unix " + socketDir + "/.s.PGSQL.5432: connect: no such file or directory"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			t.Setenv("ADDP_INFRA_DATABASE_PROVIDER", tc.provider)
			requireStartupExit(t, tc.cause)
		})
	}
}

func TestDatabaseStartupAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("run make test-standard-postgres")
	}
	target, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test PostgreSQL URL")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Prepare the prerequisite normally published by System Backend.
	if err := schema.InitializeCommon(admin); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROJECT_ROOT", t.TempDir())
	t.Setenv("POSTGRES_HOST", target.Hostname())
	t.Setenv("POSTGRES_PORT", target.Port())
	t.Setenv("POSTGRES_USER", target.User.Username())
	password, _ := target.User.Password()
	t.Setenv("ADDP_DATABASE_RUNTIME_USER", target.User.Username())
	t.Setenv("ADDP_DATABASE_RUNTIME_PASSWORD", password)
	if err := repository.PrepareDatabase(admin); err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(target.Path, "/")
	t.Setenv("POSTGRES_DB", name)
	// Runtime must reach service identity setup after both version checks.
	// Stop there without contacting another module or starting background workers.
	t.Setenv("STANDARD_SERVICE_CLIENT_SECRET", "")
	for _, tc := range []struct{ label, provider, zone, expectedZone string }{
		{"default", "", "", "Asia/Shanghai"},
		{"explicit", "postgres", "Asia/Tokyo", "Asia/Tokyo"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			t.Setenv("ADDP_INFRA_DATABASE_PROVIDER", tc.provider)
			t.Setenv("TZ", tc.zone)
			requireStartupExit(t, "service token source requires System URL, client ID and a 32-byte client secret")
			db, err := openDatabase()
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			var database, searchPath, zone string
			err = db.Raw("SELECT current_database(), current_setting('search_path'), current_setting('TimeZone')").Row().Scan(&database, &searchPath, &zone)
			if err != nil || database != name || searchPath != "standard" || zone != tc.expectedZone {
				t.Fatalf("database=%q search_path=%q timezone=%q err=%v", database, searchPath, zone, err)
			}
			t.Logf("database=%s search_path=%s timezone=%s", database, searchPath, zone)
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			domains := repository.NewDomainRepository(tx)
			domain := models.Domain{TenantID: time.Now().UnixNano(), Code: "startup_probe", Name: "startup probe", CreatedBy: 1}
			if err := domains.Create(&domain); err != nil {
				t.Fatal(err)
			}
			domain.ID = 0
			if err := domains.Create(&domain); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("expected a repository conflict for duplicate tenant/code, got %v", err)
			}
		})
	}
}
