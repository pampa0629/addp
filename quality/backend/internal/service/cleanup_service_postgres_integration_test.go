package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresQualityCleanupUsesExecutionFacts(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityServiceIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatalf("run quality migrations: %v", err)
	}

	tenantID := time.Now().UnixNano()%100000000 + 930000000
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Issue{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.CheckTask{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.RuleApplication{}).Error
	})

	rule := createQualityCleanupRuleApplication(t, db, tenantID, 12, fmt.Sprintf("cleanup_%d", tenantID))
	task := createQualityCleanupCheckTask(t, db, tenantID, 12, rule.Table)
	issue := models.Issue{
		TenantID: tenantID, ExecutionID: "exec-" + rule.Table, LastExecutionID: "exec-" + rule.Table,
		RuleApplicationID: rule.ID, RuleType: "not_null", Severity: "error", ColumnName: rule.ColumnName,
		Table: rule.Table, SchemaName: rule.SchemaName, EngineID: rule.EngineID,
		FailedCount: 1, TotalCount: 10, PassRate: 90, Detail: []byte(`{}`), Status: "open",
	}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatalf("create issue: %v", err)
	}
	execution := createQualityCleanupExecution(t, db, task, commonExecution.ExecutionStatusPending)
	svc := NewCleanupService(db, nil, nil)

	stats, err := svc.ExecuteCleanup(context.Background(), uint(tenantID), events.CleanupModeLogical, map[string]interface{}{"engine_id": int64(12)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() with active execution error = %v", err)
	}
	if len(stats.Errors) != 1 || stats.DisabledRuleApps != 0 {
		t.Fatalf("active execution cleanup stats = %#v, want one conflict and no updates", stats)
	}

	completedAt := time.Now().UTC()
	if err := db.Model(&commonExecution.TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ?", execution.ExecutionID, tenantID).
		Updates(map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt, "updated_at": completedAt}).Error; err != nil {
		t.Fatalf("complete execution: %v", err)
	}
	if err := db.Model(&models.CheckTask{}).Where("id = ? AND tenant_id = ?", task.ID, tenantID).
		Update("last_execution_status", commonExecution.ExecutionStatusRunning).Error; err != nil {
		t.Fatalf("write stale task summary: %v", err)
	}

	stats, err = svc.ExecuteCleanup(context.Background(), uint(tenantID), events.CleanupModeLogical, map[string]interface{}{"engine_id": int64(12)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() after completion error = %v", err)
	}
	if len(stats.Errors) != 0 || stats.DisabledRuleApps != 1 || stats.IgnoredIssues != 1 {
		t.Fatalf("completed execution cleanup stats = %#v, want committed logical cleanup", stats)
	}
}
