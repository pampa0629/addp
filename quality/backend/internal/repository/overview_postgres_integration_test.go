package repository

import (
	"context"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func TestIntegrationPostgresOverviewSeparatesAttemptAndObservation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("PostgreSQL integration gate required")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	tenant := time.Now().UnixNano()%100000000 + 970000000
	t.Cleanup(func() {
		db.Where("tenant_id=?", tenant).Delete(&commonExecution.TaskExecution{})
		db.Where("tenant_id=?", tenant).Delete(&models.QualityPlan{})
	})
	plan := models.QualityPlan{TenantID: tenant, Code: "overview", Name: "overview", Version: 2, TableBindings: []byte(`[{"alias":"a","locator":""}]`)}
	if err = db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	bindings := []models.PlanTableBinding{{Alias: "a", Locator: "addp://engine/12/path/east/orders?type=table"}}
	key, _ := models.PlanTargetKey(bindings)
	now := time.Now().UTC()
	var latestID string
	for i, status := range []string{"success", "failed"} {
		e := newQualityRepositoryTestExecution(uuid.NewString(), int(tenant), now.Add(time.Duration(i)*time.Millisecond))
		e.SourceTaskID = commonExecution.NewSourceTaskIDFromInt(int(plan.ID))
		e.Status = status
		e.CompletedAt = &now
		e.ExecutionConfig = commonModels.JSONMap{"target_key": key, "table_bindings": bindings, "task_version": 1, "owner_domain_id": nil}
		if i == 0 {
			e.Metadata = commonModels.JSONMap{"schema_version": "addp.quality.plan-result/v1", "rules": []interface{}{map[string]interface{}{"passed": true}, map[string]interface{}{"passed": false}}}
		} else {
			e.ErrorDetails = commonModels.JSONMap{"code": "quality.plan.sql_execution_failed"}
		}
		if err = db.Create(e).Error; err != nil {
			t.Fatal(err)
		}
		latestID = e.ExecutionID
	}
	result, err := NewOverviewRepository(db).Get(context.Background(), tenant, nil, 30, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanCount != 1 || result.Total != 1 || len(result.Data) != 1 || result.NeverRunPlans != 0 {
		t.Fatalf("overview=%+v", result)
	}
	scope := result.Data[0]
	if scope.ExecutionID == nil || *scope.ExecutionID != latestID || scope.PassRate == nil || *scope.PassRate != 50 || scope.ObservedExecutionID == nil || *scope.ObservedExecutionID == latestID {
		t.Fatalf("scope=%+v", scope)
	}
	if len(result.Trend) != 1 || result.Trend[0].RuntimeErrors != 1 || result.Trend[0].TotalRules != 2 || result.Trend[0].PassedRules != 1 {
		t.Fatalf("trend=%+v", result.Trend)
	}
	// A second target contributes checks, not an average of per-run percentages.
	west := newQualityRepositoryTestExecution(uuid.NewString(), int(tenant), now.Add(2*time.Millisecond))
	west.SourceTaskID = commonExecution.NewSourceTaskIDFromInt(int(plan.ID))
	west.Status = "success"
	west.CompletedAt = &now
	bindings[0].Locator = "addp://engine/12/path/west/orders?type=table"
	westKey, _ := models.PlanTargetKey(bindings)
	west.ExecutionConfig = commonModels.JSONMap{"target_key": westKey, "table_bindings": bindings, "task_version": 2, "owner_domain_id": int64(7)}
	west.Metadata = commonModels.JSONMap{"schema_version": "addp.quality.plan-result/v1", "rules": []interface{}{map[string]interface{}{"passed": true}, map[string]interface{}{"passed": true}, map[string]interface{}{"passed": true}}}
	if err = db.Create(west).Error; err != nil {
		t.Fatal(err)
	}
	// Moving current ownership must not rewrite execution-time attribution.
	if err = db.Model(&plan).Update("owner_domain_id", 9).Error; err != nil {
		t.Fatal(err)
	}
	unused := models.QualityPlan{TenantID: tenant, Code: "unused", Name: "unused", Version: 1, TableBindings: []byte(`[{"alias":"a","locator":""}]`)}
	if err = db.Create(&unused).Error; err != nil {
		t.Fatal(err)
	}
	legacy := newQualityRepositoryTestExecution(uuid.NewString(), int(tenant), now.Add(3*time.Millisecond))
	legacy.SourceTaskID = west.SourceTaskID
	legacy.Status = "failed"
	legacy.CompletedAt = &now
	legacy.ExecutionConfig = commonModels.JSONMap{}
	if err = db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewOverviewRepository(db)
	result, err = repo.Get(context.Background(), tenant, nil, 30, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || result.NeverRunPlans != 1 || result.UnscopedExecutions != 1 || len(result.Trend) != 1 || result.Trend[0].PassRate == nil || *result.Trend[0].PassRate != 80 {
		t.Fatalf("weighted overview: %+v", result)
	}
	for _, scope := range result.Data {
		if scope.PlanID == unused.ID && (scope.PassRate != nil || scope.ObservedExecutionID != nil || scope.TargetKey != nil) {
			t.Fatalf("unexecuted plan got fabricated score: %+v", scope)
		}
	}
	page, err := repo.Get(context.Background(), tenant, nil, 30, 2, 1)
	if err != nil || page.Total != 3 || page.TotalPages != 3 || len(page.Data) != 1 {
		t.Fatalf("pagination: %+v %v", page, err)
	}
	for _, domain := range []int64{0, 7, 9} {
		filtered, err := repo.Get(context.Background(), tenant, &domain, 30, 1, 20)
		if err != nil {
			t.Fatal(err)
		}
		switch domain {
		case 0:
			if filtered.PlanCount != 1 || len(filtered.Trend) != 1 || filtered.Trend[0].Executions != 2 || filtered.UnscopedExecutions != 0 {
				t.Fatalf("public snapshot ownership: %+v", filtered)
			}
		case 7:
			if filtered.PlanCount != 0 || len(filtered.Trend) != 1 || filtered.Trend[0].TotalRules != 3 {
				t.Fatalf("historical snapshot ownership: %+v", filtered)
			}
		case 9:
			if filtered.PlanCount != 1 || filtered.Total != 2 || len(filtered.Trend) != 0 {
				t.Fatalf("current ownership: %+v", filtered)
			}
		}
	}
	other, err := NewOverviewRepository(db).Get(context.Background(), tenant+1, nil, 30, 1, 20)
	if err != nil || other.PlanCount != 0 || other.Total != 0 || len(other.Trend) != 0 {
		t.Fatalf("tenant isolation %v %+v", err, other)
	}
}
