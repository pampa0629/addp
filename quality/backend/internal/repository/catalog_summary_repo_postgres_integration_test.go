package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresCatalogSummaryRepository(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()%100000000 + 930000000
	now := time.Now().UTC()
	task := models.QualityPlan{TenantID: tenantID, Name: "Catalog summary", Code: "catalog_summary", Version: 1, TableBindings: []byte(`[{"alias":"orders","locator":"addp://engine/7/path/public/orders?type=table"}]`), Rules: []byte(`{"schema_version":"addp.quality.plan-rules/v1","rules":[]}`), CreatedBy: 1, LastRunAt: &now, LastExecutionID: fmt.Sprintf("quality-summary-%d", tenantID), LastExecutionStatus: commonExecution.ExecutionStatusSuccess}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	sourceTaskID := fmt.Sprintf("%d", task.ID)
	execution := commonExecution.TaskExecution{TenantID: int(tenantID), ExecutionID: task.LastExecutionID, Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan, Source: commonExecution.ModuleQuality, SourceTaskID: &sourceTaskID, Status: commonExecution.ExecutionStatusSuccess, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual, Metadata: commonModels.JSONMap{"schema_version": "addp.quality.plan-result/v1", "quality_score": 96.25}, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	issue := models.Issue{TenantID: tenantID, ExecutionID: execution.ExecutionID, LastExecutionID: execution.ExecutionID, PlanID: task.ID, RuleKey: "0fef14f6-50fb-44fd-a193-28430c7e4b42", RuleType: "not_null", Severity: "error", ColumnName: "id", Table: "orders", SchemaName: "public", EngineID: 7, FailedCount: 1, TotalCount: 10, PassRate: 90, Status: "open"}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Issue{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.QualityPlan{}).Error
	})
	facts, err := NewCatalogSummaryRepository(db).Resolve(context.Background(), tenantID, []models.CatalogSummaryReference{{EngineID: 7, SchemaName: "public", TableName: "orders"}})
	if err != nil {
		t.Fatal(err)
	}
	fact, exists := facts[catalogSummaryKey(7, "public", "orders")]
	if !exists || fact.OpenIssues != 1 || fact.Execution == nil {
		t.Fatalf("fact = %#v", fact)
	}
	if score := QualityScoreFromExecution(fact.Execution); score == nil || *score != 96.25 {
		t.Fatalf("score = %v", score)
	}
}
