package repository

import (
	"context"
	"errors"
	commonapi "github.com/addp/common/api"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

func TestInstalledQualityStorage(t *testing.T) {
	if os.Getenv("ADDP_TEST_QUALITY_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Quality deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "quality"}, "quality", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := context.Background()
	repo := NewRuleRepository(db)
	rule := models.QualityRule{TenantID: 17, Code: "orders_present", RuleContent: models.RuleContent{Name: "订单质量", Type: "not_null", Params: []byte("{}")}}
	if err := repo.Create(ctx, &rule); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, 17, rule.ID)
	if err != nil || got.Code != rule.Code || got.RevisionNo != 1 {
		t.Fatalf("stored rule: %v %v", got, err)
	}
	var revision models.RuleRevision
	if err := db.Where("tenant_id=? AND rule_id=? AND revision_no=?", 17, rule.ID, 1).First(&revision).Error; err != nil || revision.Name != rule.Name {
		t.Fatalf("revision: %v %v", revision, err)
	}
	if _, err := repo.Get(ctx, 18, rule.ID); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("other tenant rule: %v", err)
	}
	checkQualityOverview(t, db)
	t.Run("duplicate plan target is excluded while active", func(t *testing.T) {
		const tenant = 18
		plan := createPlanRepositoryTestTask(t, db, tenant)
		plans := NewPlanRepository(db)
		for _, tc := range []struct {
			region   string
			conflict bool
		}{{"east", false}, {"east", true}, {"west", false}} {
			run := newQualityRepositoryTestExecution(uuid.NewString(), tenant, time.Now().UTC())
			request := models.PlanRunRequest{TableBindings: map[string]string{"orders": "addp://engine/12/path/" + tc.region + "/orders?type=table"}}
			_, err := plans.CreateExecution(ctx, plan.ID, tenant, run, request)
			if tc.conflict {
				if !errors.Is(err, commonapi.ErrConflict) {
					t.Fatalf("expected same-target conflict, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("create %s execution: %v", tc.region, err)
			}
		}
	})
	t.Logf("quality rule=%d revision=1, tenant isolation and attempt/observation overview confirmed", rule.ID)
}
