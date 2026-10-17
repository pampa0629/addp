package repository

import (
	"os"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"

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
	// The canonical shared schema and all owner fixtures roll back together.
	if err := commonExecution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE SCHEMA IF NOT EXISTS develop`,
		`CREATE TABLE IF NOT EXISTS develop.data_migrations (version BIGINT PRIMARY KEY,name TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`DELETE FROM develop.data_migrations WHERE version=2026101701`,
	} {
		if err := tx.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows := []commonExecution.TaskExecution{
		{Module: "develop", TaskType: "query", Metadata: commonModels.JSONMap{
			"result":                  map[string]interface{}{"summary": map[string]interface{}{"preview_rows": []map[string]interface{}{{"phone": "138****5678"}}}},
			"protected_query_history": map[string]interface{}{"result": map[string]interface{}{"rows": []map[string]interface{}{{"phone": "private-result-sentinel"}}}},
		}},
		{Module: "develop", TaskType: "workflow", Metadata: commonModels.JSONMap{"protected_query_history": "other-task"}},
		{Module: "manager", TaskType: "query", Metadata: commonModels.JSONMap{"protected_query_history": "other-owner"}},
	}
	ids := make([]string, len(rows))
	tenantID := int(time.Now().UnixNano()%1_000_000_000 + 100)
	for i := range rows {
		rows[i].ExecutionID = uuid.NewString()
		rows[i].TenantID = tenantID
		rows[i].Source = rows[i].Module
		rows[i].Status = "success"
		rows[i].TriggerType = "manual"
		ids[i] = rows[i].ExecutionID
		if err := tx.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := migrateQueryResultStorage(tx); err != nil {
			t.Fatal(err)
		}
	}
	var value string
	if err := tx.Raw(`SELECT metadata::text FROM common.task_executions WHERE execution_id = ?`, ids[0]).Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(value, "private-result-sentinel") || strings.Contains(value, "protected_query_history") || !strings.Contains(value, "138****5678") {
		t.Fatal("migration did not preserve only the default preview")
	}
	var other int64
	if err := tx.Raw(`SELECT count(*) FROM common.task_executions WHERE execution_id IN ? AND metadata ? 'protected_query_history'`, ids).Scan(&other).Error; err != nil || other != 2 {
		t.Fatal("migration changed another owner or task type")
	}
	var successful int64
	if err := tx.Raw(`SELECT count(*) FROM common.task_executions WHERE execution_id IN ? AND status='success'`, ids).Scan(&successful).Error; err != nil {
		t.Fatal(err)
	}
	if successful != 3 {
		t.Fatal("migration altered execution state")
	}
}
