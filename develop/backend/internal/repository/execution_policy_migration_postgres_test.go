package repository

import (
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
)

func TestExecutionPolicyMigrationAgainstPostgres(t *testing.T) {
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
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS develop").Error; err != nil {
		t.Fatal(err)
	}
	// Test databases are disposable and the owner schema changes roll back together.
	for _, statement := range []string{
		"DROP TABLE IF EXISTS develop.execution_policy",
		"DROP TABLE IF EXISTS develop.query_policy",
		`CREATE TABLE develop.query_policy (id BIGSERIAL PRIMARY KEY, scope_type TEXT NOT NULL, tenant_id BIGINT, default_query_timeout INTEGER NOT NULL, max_query_timeout INTEGER NOT NULL, query_result_limit INTEGER NOT NULL, query_concurrency INTEGER NOT NULL DEFAULT 20, query_per_engine_concurrency INTEGER NOT NULL DEFAULT 5, version BIGINT NOT NULL, updated_by BIGINT NOT NULL, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ)`,
		`INSERT INTO develop.query_policy(scope_type,default_query_timeout,max_query_timeout,query_result_limit,query_concurrency,query_per_engine_concurrency,version,updated_by) VALUES('platform',60,600,250,12,3,7,11)`,
	} {
		if err := tx.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateExecutionPolicy(tx); err != nil {
		t.Fatal(err)
	}
	if tx.Migrator().HasTable("develop.query_policy") {
		t.Fatal("old policy table remains")
	}
	if err := tx.AutoMigrate(&models.ExecutionPolicy{}); err != nil {
		t.Fatal(err)
	}
	stored, err := NewExecutionPolicyRepository(tx).Get(t.Context(), "platform", nil)
	if err != nil || stored == nil {
		t.Fatalf("policy missing: %v", err)
	}
	if stored.QueryConcurrency != 12 || stored.QueryPerEngineConcurrency != 3 || stored.Version != 7 || stored.DefaultQueryTimeout != 60 || stored.WorkflowConcurrency != 2 || stored.NotebookConcurrency != 1 {
		t.Fatalf("policy migration lost values: %#v", stored)
	}
	if err := migrateExecutionPolicy(tx); err != nil {
		t.Fatal("migration is not idempotent:", err)
	}
	// Mixed old/new stores must fail rather than silently choose a source.
	if err := tx.Exec("CREATE TABLE develop.query_policy(id BIGINT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateExecutionPolicy(tx); err == nil {
		t.Fatal("dual policy stores accepted")
	}
}
