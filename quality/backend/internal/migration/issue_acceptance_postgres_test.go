package migration

import (
	"context"
	"os"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresAcceptanceMigrationPreservesManualHistory(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard PostgreSQL gate required")
	}
	db, err := gorm.Open(postgres.Open(qualityMigrationIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = NewRunner(db).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	defer tx.Rollback()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if err := tx.Exec(q, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	exec(`DROP TABLE quality.issue_actions`)
	exec(`ALTER TABLE quality.issues DROP COLUMN version, DROP COLUMN evidence, DROP COLUMN accepted_keys, DROP COLUMN evidence_reason, DROP COLUMN accepted_count, DROP COLUMN pending_count`)
	exec(`INSERT INTO quality.plans(id,tenant_id,code,name,table_bindings,created_by,updated_by) VALUES(987654321,987654321,'audit_migration','audit','[]',42,42)`)
	exec(`INSERT INTO quality.issues(tenant_id,plan_id,execution_id,last_execution_id,rule_key,rule_type,severity,column_name,table_name,schema_name,engine_id,failed_count,total_count,pass_rate,status,resolved_at,resolved_by,resolution_note)
VALUES(987654321,987654321,'historical','historical','00000000-0000-4000-8000-000000000001','foreign_key','warning','person_id','members','outdoor',1,77,100,23,'ignored',now(),42,'historical explanation')`)
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatal(err)
	}
	exec(catalog.Files[15].Contents)
	var issue models.Issue
	if err = tx.Where("tenant_id=?", 987654321).First(&issue).Error; err != nil {
		t.Fatal(err)
	}
	if issue.Status != "ignored" || issue.Version != 1 || issue.AcceptedCount != 0 || issue.Evidence != nil {
		t.Fatalf("invented acceptance: %+v", issue)
	}
	var action models.IssueAction
	if err = tx.Where("tenant_id=? AND issue_id=?", 987654321, issue.ID).First(&action).Error; err != nil {
		t.Fatal(err)
	}
	if action.Action != "ignored" || action.Note != "historical explanation" || action.ActorID == nil || *action.ActorID != 42 || action.AcceptedCount != 0 {
		t.Fatalf("audit changed: %+v", action)
	}
}
