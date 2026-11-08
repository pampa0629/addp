package repository

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/addp/common/schema"
	"github.com/addp/meta/internal/models"
	metamigrations "github.com/addp/meta/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAuthorizationBasisStartupMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard Meta PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(`CREATE TABLE meta.startup_schema_revision(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),version bigint NOT NULL);INSERT INTO meta.startup_schema_revision VALUES(true,5)`).Error; err != nil {
		t.Fatal(err)
	}
	const name = "028_authorization_basis_revision.sql"
	data, err := metamigrations.FS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	fixture := fstest.MapFS{name: &fstest.MapFile{Data: data}}
	applied := 0
	upgrade := func() error {
		return schema.Migrate(tx, "meta", SchemaVersion, func(db *gorm.DB) error { applied++; return runSQLMigrations(db, fixture, ".") })
	}
	if err := upgrade(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Require(tx, "meta", SchemaVersion); err != nil {
		t.Fatal("new Worker cannot start", err)
	}
	if err := schema.Require(tx, "meta", 5); err == nil {
		t.Fatal("old Worker accepted new schema")
	}
	var generation string
	if err := tx.Raw("SELECT generation::text FROM meta.authorization_basis_generation WHERE singleton").Scan(&generation).Error; err != nil || generation == "" {
		t.Fatal("startup did not apply revision migration", err)
	}
	if err := upgrade(); err != nil || applied != 1 {
		t.Fatal("restart replayed revision migration", applied, err)
	}
}
