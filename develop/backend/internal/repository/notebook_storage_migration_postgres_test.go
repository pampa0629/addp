package repository

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
)

func TestNotebookStorageMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("standard Develop PostgreSQL gate required")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	defer conn.Close()
	tx := db.Begin()
	defer tx.Rollback()
	for _, sql := range []string{
		`CREATE SCHEMA IF NOT EXISTS develop`,
		`CREATE TABLE IF NOT EXISTS develop.data_migrations(version bigint PRIMARY KEY,name text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`,
		`CREATE TABLE IF NOT EXISTS develop.dev_tasks(id bigserial PRIMARY KEY,tenant_id bigint NOT NULL,name text,dev_type text,content jsonb,deleted_at timestamptz)`,
		`DELETE FROM develop.data_migrations WHERE version=2026111601`,
	} {
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Roll back the full fixture, preserving the shared test database.
	var id uint
	if err := tx.Raw(`INSERT INTO develop.dev_tasks(tenant_id,name,dev_type,content,deleted_at) VALUES(7,'notebook_migration_fixture','script','{"notebook_path":"analysis.ipynb","minio_path":"tenant_7/notebooks/analysis.ipynb","kernel":"python3"}',now()) RETURNING id`).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateNotebookStorage(tx); err != nil {
		t.Fatal(err)
	}
	var locator string
	if err := tx.Raw("SELECT content->>'notebook_locator' FROM develop.dev_tasks WHERE id=?", id).Scan(&locator).Error; err != nil {
		t.Fatal(err)
	}
	if locator != "addp-infra://object/develop/tenant_7/notebooks/analysis.ipynb?type=object" {
		t.Fatalf("reference=%s", locator)
	}
	var legacy bool
	tx.Raw("SELECT jsonb_exists(content,'notebook_path') OR jsonb_exists(content,'minio_path') FROM develop.dev_tasks WHERE id=?", id).Scan(&legacy)
	if legacy {
		t.Fatal("legacy active fields retained")
	}
	if err := migrateNotebookStorage(tx); err != nil {
		t.Fatal("migration not repeatable:", err)
	}
	if err := tx.Exec(`DELETE FROM develop.data_migrations WHERE version=2026111601`).Error; err != nil {
		t.Fatal(err)
	}

	if err := tx.Exec(`UPDATE develop.dev_tasks SET content='{"notebook_path":"analysis.ipynb","minio_path":"tenant_7/notebooks/analysis.ipynb"}' WHERE id=?`, id).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(`INSERT INTO develop.dev_tasks(tenant_id,name,dev_type,content) VALUES (7,'notebook_migration_conflict','script','{"notebook_path":"other.ipynb","minio_path":"tenant_8/notebooks/other.ipynb"}')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateNotebookStorage(tx); err == nil {
		t.Fatal("cross-tenant legacy file accepted")
	}
	var count int64
	tx.Raw(`SELECT count(*) FROM develop.data_migrations WHERE version=2026111601`).Scan(&count)
	var unchanged bool
	if err := tx.Raw("SELECT jsonb_exists(content,'notebook_path') AND NOT jsonb_exists(content,'notebook_locator') FROM develop.dev_tasks WHERE id=?", id).Scan(&unchanged).Error; err != nil {
		t.Fatal(err)
	}
	if !unchanged {
		t.Fatal("earlier valid row changed despite later conflict")
	}
	if count != 0 {
		t.Fatal("version advanced after conflict")
	}
}
