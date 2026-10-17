package repository

import (
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestQueryResultMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	// All fixture DDL and rows roll back inside the standard disposable database.
	statements := []string{
		`CREATE SCHEMA IF NOT EXISTS common`, `CREATE SCHEMA IF NOT EXISTS develop`,
		`DROP TABLE IF EXISTS common.task_executions CASCADE`,
		`CREATE TABLE common.task_executions (module TEXT,task_type TEXT,metadata JSONB,status TEXT)`,
		`CREATE TABLE IF NOT EXISTS develop.data_migrations (version BIGINT PRIMARY KEY,name TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`DELETE FROM develop.data_migrations WHERE version=2026101701`,
		`INSERT INTO common.task_executions VALUES ('develop','query','{"result":{"summary":{"preview_rows":[{"phone":"138****5678"}]}},"protected_query_history":{"result":{"rows":[{"phone":"private-result-sentinel"}]}}}', 'success'),('develop','workflow','{"protected_query_history":"other-task"}', 'success'),('other','query','{"protected_query_history":"other-owner"}', 'success')`,
	}
	for _, s := range statements {
		if err := tx.Exec(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := migrateQueryResultStorage(tx); err != nil {
			t.Fatal(err)
		}
	}
	var value string
	if err := tx.Raw(`SELECT metadata::text FROM common.task_executions WHERE module='develop' AND task_type='query'`).Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(value, "private-result-sentinel") || strings.Contains(value, "protected_query_history") || !strings.Contains(value, "138****5678") {
		t.Fatal("migration did not preserve only the default preview")
	}
	var other int64
	if err := tx.Raw(`SELECT count(*) FROM common.task_executions WHERE metadata ? 'protected_query_history'`).Scan(&other).Error; err != nil || other != 2 {
		t.Fatal("migration changed another owner or task type")
	}
	var successful int64
	tx.Raw(`SELECT count(*) FROM common.task_executions WHERE status='success'`).Scan(&successful)
	if successful != 3 {
		t.Fatal("migration altered execution state")
	}
}
