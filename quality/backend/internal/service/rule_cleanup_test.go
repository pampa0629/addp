package service

import (
	"context"
	"encoding/json"
	"github.com/addp/common/events"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"testing"
)

func TestTenantCleanupOwnsRulesButEngineCleanupDoesNot(t *testing.T) {
	db := newQualityCleanupTestDB(t)
	ctx := context.Background()
	repo := repository.NewRuleRepository(db)
	for _, tenant := range []int64{7, 8} {
		rule := models.QualityRule{TenantID: tenant, Code: "required", RuleContent: models.RuleContent{Name: "Required", Type: "not_null", Params: json.RawMessage("{}")}, CreatedBy: 1, UpdatedBy: 1}
		if err := repo.Create(ctx, &rule); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewCleanupService(db, nil, nil)
	engine, err := svc.ExecuteCleanup(ctx, 7, events.CleanupModePhysical, map[string]interface{}{"engine_id": int64(12)})
	if err != nil || engine.DeletedRules != 0 {
		t.Fatalf("engine removed neutral rules: %+v %v", engine, err)
	}
	tenant, err := svc.ExecuteCleanup(ctx, 7, events.CleanupModePhysical, map[string]interface{}{"tenant_id": int64(7)})
	if err != nil || len(tenant.Errors) > 0 || tenant.DeletedRules != 1 {
		t.Fatalf("tenant cleanup: %+v %v", tenant, err)
	}
	for _, model := range []interface{}{&models.QualityRule{}, &models.RuleRevision{}} {
		var count int64
		if err := db.Model(model).Where("tenant_id=7").Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("tenant remnants %T: %d %v", model, count, err)
		}
		if err := db.Model(model).Where("tenant_id=8").Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("other tenant %T: %d %v", model, count, err)
		}
	}
}
