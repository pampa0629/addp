package migration

import (
	"context"
	"encoding/json"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func TestIntegrationPostgresIssueScopeBackfillUsesOnlyFrozenEvidence(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("PostgreSQL integration gate required")
	}
	db, err := gorm.Open(postgres.Open(qualityMigrationIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
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
	tenant := time.Now().UnixNano()%100000000 + 960000000
	plan := models.QualityPlan{TenantID: tenant, Code: "scope_backfill", Name: "scope backfill", Version: 1, TableBindings: []byte(`[{"alias":"a","locator":"addp://engine/12/path/current/orders?type=table"}]`)}
	if err = tx.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	oldBindings := []models.PlanTableBinding{{Alias: "a", Locator: "addp://engine/12/path/historical/orders?type=table"}}
	var ids []int64
	var historyID string
	var originalConfig []byte
	for i := 0; i < 3; i++ {
		e := commonExecution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant), Module: "quality", TaskType: "quality_plan", Source: "quality", SourceTaskID: commonExecution.NewSourceTaskIDFromInt(int(plan.ID)), Status: "success", TriggerType: "manual", ExecutionBoundary: "bounded", ExecutionConfig: commonModels.JSONMap{"table_bindings": oldBindings}}
		if i == 1 {
			e.ExecutionConfig = commonModels.JSONMap{"old": true}
		}
		if i == 2 {
			e.TenantID++
		}
		if err = tx.Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			historyID = e.ExecutionID
			originalConfig, _ = json.Marshal(e.ExecutionConfig)
		}
		issue := models.Issue{TenantID: tenant, PlanID: plan.ID, ExecutionID: e.ExecutionID, LastExecutionID: e.ExecutionID, RuleKey: uuid.NewString(), RuleType: "not_null", Severity: "error", Status: "open"}
		if err = tx.Create(&issue).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, issue.ID)
	}
	if err = backfillIssueTargetScopes(tx); err != nil {
		t.Fatal(err)
	}
	key, _ := models.PlanTargetKey(oldBindings)
	for i, id := range ids {
		var issue models.Issue
		if err = tx.First(&issue, id).Error; err != nil {
			t.Fatal(err)
		}
		if i == 0 && (issue.TargetKey == nil || *issue.TargetKey != key) {
			t.Fatalf("missing historical scope: %+v", issue)
		}
		if i > 0 && issue.TargetKey != nil {
			t.Fatal("invented scope from missing or cross-tenant evidence")
		}
	}
	var e commonExecution.TaskExecution
	if err = tx.Where("execution_id=?", historyID).First(&e).Error; err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(e.ExecutionConfig)
	if string(after) != string(originalConfig) {
		t.Fatal("historical execution was rewritten")
	}
}
