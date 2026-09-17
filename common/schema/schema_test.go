package schema

import (
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRequireIsReadOnlyAndRejectsMissingOrMismatchedRevision(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS owner").Error; err != nil {
		t.Fatal(err)
	}
	if err := Require(db, "owner", 1); err == nil {
		t.Fatal("missing schema accepted")
	}
	if db.Migrator().HasTable("owner.startup_schema_revision") {
		t.Fatal("Require wrote schema")
	}
	if err := db.Exec("CREATE TABLE owner.startup_schema_revision (singleton BOOLEAN, version BIGINT); INSERT INTO owner.startup_schema_revision VALUES(TRUE, 2)").Error; err != nil {
		t.Fatal(err)
	}
	if err := Require(db, "owner", 1); err == nil {
		t.Fatal("older process accepted")
	}
	if err := Require(db, "owner", 3); err == nil {
		t.Fatal("unmigrated version accepted")
	}
	if err := Require(db, "owner", 2); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStartupSchemaOwnership(t *testing.T) {
	dsn := os.Getenv("SCHEMA_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SCHEMA_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	var database string
	if err := db.Raw("SELECT current_database()").Scan(&database).Error; err != nil {
		t.Fatal(err)
	}
	if database != "addp_test" && !(os.Getenv("CI") == "true" && strings.Contains(database, "test")) {
		t.Fatalf("unsafe test database %q", database)
	}
	const name = "startup_contract_test"
	cleanup := func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS " + name + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	var applied atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	results := make(chan error, 2)
	apply := func(tx *gorm.DB) error {
		if applied.Add(1) == 1 {
			close(entered)
			<-release
		}
		return tx.Exec("CREATE TABLE " + name + ".data (id BIGINT PRIMARY KEY)").Error
	}
	go func() { results <- Migrate(db, name, 1, apply) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("migration did not begin")
	}
	go func() { results <- Migrate(db, name, 1, apply) }()
	if err := Require(db, name, 1); err == nil {
		t.Fatal("Worker observed uncommitted schema")
	}
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("migration deadlock")
		}
	}
	if applied.Load() != 1 {
		t.Fatalf("migration applied %d times", applied.Load())
	}
	// A Worker with a read-only transaction can validate and read business data.
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET TRANSACTION READ ONLY").Error; err != nil {
			return err
		}
		return Require(tx, name, 1)
	}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("migration failed")
	if err := Migrate(db, name, 2, func(tx *gorm.DB) error {
		if err := tx.Exec("ALTER TABLE " + name + ".data ADD COLUMN failed TEXT").Error; err != nil {
			return err
		}
		return failure
	}); !errors.Is(err, failure) {
		t.Fatalf("failure lost: %v", err)
	}
	if err := Require(db, name, 1); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasColumn(name+".data", "failed") {
		t.Fatal("failed DDL committed")
	}
	if err := Migrate(db, name, 2, func(tx *gorm.DB) error { return tx.Exec("ALTER TABLE " + name + ".data ADD COLUMN applied TEXT").Error }); err != nil {
		t.Fatal(err)
	}
	if err := Require(db, name, 1); err == nil {
		t.Fatal("old Worker accepted upgraded schema")
	}
	if err := Migrate(db, name, 1, func(*gorm.DB) error { t.Error("downgrade callback ran"); return nil }); err == nil {
		t.Fatal("downgrade accepted")
	}
}
