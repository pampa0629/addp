package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/schema"
	"github.com/addp/model/internal/config"
	"github.com/addp/model/internal/migration"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

func TestModelServerProcess(t *testing.T) {
	if os.Getenv("ADDP_TEST_RUN_MODEL_MAIN") == "1" {
		main()
	}
}

func requireModelStartupExit(t *testing.T, expected string) {
	t.Helper()
	t.Setenv("ADDP_TEST_RUN_MODEL_MAIN", "1")
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestModelServerProcess$")
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), expected) {
		t.Fatalf("expected Model to exit 1 at %q, got %v: %s", expected, err, output)
	}
	t.Logf("Model main exited 1 at %q", expected)
}

func TestPostgresStartupErrors(t *testing.T) {
	t.Setenv("PROJECT_ROOT", t.TempDir())
	socket, err := os.MkdirTemp("/tmp", "model-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(socket) })
	t.Setenv("POSTGRES_HOST", socket)
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_DB", "addp_test")
	t.Setenv("ADDP_INFRA_DATABASE_PROVIDER", "mysql")
	requireModelStartupExit(t, `unsupported platform database provider "mysql"`)
	t.Setenv("ADDP_INFRA_DATABASE_PROVIDER", "postgres")
	requireModelStartupExit(t, "dial unix "+socket+"/.s.PGSQL.5432")
}

func TestPostgresModelDatabaseStartup(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("run make test-model-postgres")
	}
	target, err := url.Parse(dsn)
	if err != nil || target.User == nil {
		t.Fatal("invalid Model test PostgreSQL URL")
	}
	t.Setenv("PROJECT_ROOT", t.TempDir())
	t.Setenv("POSTGRES_HOST", target.Hostname())
	t.Setenv("POSTGRES_PORT", target.Port())
	t.Setenv("POSTGRES_USER", target.User.Username())
	password, _ := target.User.Password()
	t.Setenv("POSTGRES_PASSWORD", password)
	database := strings.TrimPrefix(target.Path, "/")
	t.Setenv("POSTGRES_DB", database)
	t.Setenv("MODEL_SERVICE_CLIENT_SECRET", "")
	t.Setenv("TZ", "")
	t.Setenv("ADDP_INFRA_DATABASE_PROVIDER", "")
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	cold := os.Getenv("ADDP_TEST_MODEL_STARTUP_COLD") == "1"
	if cold {
		if database != "addp_model_disposable" {
			t.Fatal("cold Model startup requires the Job-owned database")
		}
		requireModelStartupExit(t, "Failed to ensure execution store:")
		assertModelRelations(t, db, false, "model.entities", "model.schema_migrations")
		if err := schema.InitializeCommon(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("CREATE SCHEMA model; CREATE TABLE model.standard_reference_guards (fixture boolean)").Error; err != nil {
			t.Fatal(err)
		}
		requireModelStartupExit(t, "apply model migration 005_add_standard_reference_guards.up.sql")
		assertModelRelations(t, db, false, "model.entities", "model.schema_migrations")
		assertModelRelations(t, db, true, "model.standard_reference_guards")
		if err := db.Exec("DROP TABLE model.standard_reference_guards").Error; err != nil {
			t.Fatal(err)
		}
	}
	var hasCommonRevision bool
	if err := db.Raw("SELECT to_regclass('common.startup_schema_revision') IS NOT NULL").Row().Scan(&hasCommonRevision); err != nil {
		t.Fatal(err)
	}
	const serviceIdentity = "service token source requires System URL, client ID and a 32-byte client secret"
	if hasCommonRevision {
		if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
			t.Fatal(err)
		}
		requireModelStartupExit(t, serviceIdentity)
	} else {
		requireModelStartupExit(t, "Failed to ensure execution store:")
		t.Log("Local test database has no System version record; positive Model main runs in the isolated CI job")
		if err := migration.Run(db); err != nil {
			t.Fatal(err)
		}
	}
	assertModelRelations(t, db, true, "model.cleanup_results")
	var before []modelMigrationEntry
	readModelMigrations(t, db, &before)
	// A committed fixture is visible across the two startup processes.
	var marker models.Entity
	if cold {
		marker = models.Entity{TenantID: time.Now().UnixNano(), Code: "platform_restart_probe", Name: "Restart probe", Status: "draft", CreatedBy: 1}
		if err := repository.NewEntityRepository(db).Create(&marker); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Unscoped().Delete(&marker).Error; err != nil {
				t.Error(err)
			}
		})
	}

	t.Setenv("ADDP_INFRA_DATABASE_PROVIDER", "postgres")
	t.Setenv("TZ", "Asia/Tokyo")
	if hasCommonRevision {
		requireModelStartupExit(t, serviceIdentity)
	}

	explicit, err := openDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	explicitPool, err := explicit.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer explicitPool.Close()
	for _, session := range []struct {
		db   *gorm.DB
		zone string
	}{{db, "Asia/Shanghai"}, {explicit, "Asia/Tokyo"}} {
		var name, path, zone string
		err := session.db.Raw("SELECT current_database(), current_setting('search_path'), current_setting('TimeZone')").Row().Scan(&name, &path, &zone)
		if err != nil || name != database || path != "model" || zone != session.zone {
			t.Fatalf("database=%q search_path=%q timezone=%q error=%v", name, path, zone, err)
		}
		t.Logf("database=%s search_path=%s timezone=%s", name, path, zone)
	}
	if cold {
		var reloaded models.Entity
		if err := explicit.First(&reloaded, marker.ID).Error; err != nil ||
			reloaded.TenantID != marker.TenantID || reloaded.Name != marker.Name || reloaded.Code != marker.Code {
			t.Fatalf("Model startup changed the committed fixture: %v", err)
		}
	}
	var after []modelMigrationEntry
	readModelMigrations(t, explicit, &after)
	if len(before) != len(after) {
		t.Fatal("migration ledger size changed on restart")
	}
	for i := range before {
		if before[i].Version != after[i].Version || !before[i].AppliedAt.Equal(after[i].AppliedAt) {
			t.Fatal("migration ledger changed on restart")
		}
	}
	assertModelConflict(t, explicit, func(repo *repository.EntityRepository) error {
		entity := models.Entity{TenantID: time.Now().UnixNano(), Code: "platform_startup_probe", Name: "Startup probe", Status: "draft", CreatedBy: 1}
		if err := repo.Create(&entity); err != nil {
			t.Fatalf("insert fixture: %v", err)
		}
		entity.ID = 0
		return repo.Create(&entity)
	})
	assertModelConflict(t, explicit, func(repo *repository.EntityRepository) error {
		return repo.CreateAttribute(&models.EntityAttribute{EntityID: -1, Name: "Missing", ColumnName: "missing", DataType: "string"})
	})
}

type modelMigrationEntry struct {
	Version   string
	AppliedAt time.Time
}

func readModelMigrations(t *testing.T, db *gorm.DB, rows *[]modelMigrationEntry) {
	t.Helper()
	if err := db.Table("model.schema_migrations").Order("version").Find(rows).Error; err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.Glob("../../internal/migration/sql/*.up.sql")
	if err != nil || len(expected) == 0 || len(*rows) != len(expected) {
		t.Fatalf("expected %d migration records, got %d: %v", len(expected), len(*rows), err)
	}
	for i, file := range expected {
		if (*rows)[i].Version != filepath.Base(file) {
			t.Fatalf("expected migration %s, got %s", filepath.Base(file), (*rows)[i].Version)
		}
	}
}

func assertModelRelations(t *testing.T, db *gorm.DB, expected bool, names ...string) {
	t.Helper()
	for _, name := range names {
		var exists bool
		err := db.Raw("SELECT to_regclass(?) IS NOT NULL", name).Row().Scan(&exists)
		if err != nil || exists != expected {
			t.Fatalf("relation %s exists=%t, want %t, error=%v", name, exists, expected, err)
		}
	}
}

func assertModelConflict(t *testing.T, db *gorm.DB, write func(*repository.EntityRepository) error) {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := write(repository.NewEntityRepository(tx)); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expected Model repository conflict, got %v", err)
	}
}
