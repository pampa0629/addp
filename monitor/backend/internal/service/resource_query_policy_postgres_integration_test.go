package service

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/resourcequery"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresResourceQueryPolicyFirstCASAndHotRead(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor PostgreSQL gate")
	}
	db, e := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := sqlDB.Close(); e != nil {
			t.Error(e)
		}
	})
	if e := EnsureMonitorStore(db); e != nil {
		t.Fatal(e)
	}
	var count int64
	if e := db.Model(&models.ResourceQueryPolicy{}).Count(&count).Error; e != nil || count != 0 {
		t.Fatal("standard fixture requires no resource-query policy", e)
	}
	t.Cleanup(func() {
		if e := db.Where("id = 1").Delete(&models.ResourceQueryPolicy{}).Error; e != nil {
			t.Error(e)
		}
		var n int64
		db.Model(&models.ResourceQueryPolicy{}).Count(&n)
		if n != 0 {
			t.Error("query policy residual")
		}
	})
	repo := repository.NewResourceQueryPolicyRepository(db)
	service := NewResourceObservationService(repo, nil, nil, nil, false, nil, ProcessObservationDependencies{})
	initial, e := service.Policy(context.Background())
	if e != nil || initial.Version != 0 || initial.Budget != resourcequery.DefaultBudget() {
		t.Fatal(initial, e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := service.UpdatePolicy(context.Background(), ResourceQueryPolicyInput{Version: initial.Version, Budget: initial.Budget}, 1)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	won, conflicts := 0, 0
	for e := range results {
		if e == nil {
			won++
		} else if errors.Is(e, resourcequery.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if won != 1 || conflicts != 1 {
		t.Fatal(won, conflicts)
	}
	before, e := service.Policy(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	before.MaxMetrics = 1
	before.TimeoutSeconds = 1
	updated, e := service.UpdatePolicy(context.Background(), ResourceQueryPolicyInput{Version: before.Version, Budget: before.Budget}, 1)
	if e != nil || updated.Version != 2 || updated.PendingRestart {
		t.Fatal(updated, e)
	}
	// A separate service instance immediately reads the committed version, without cache/restart.
	second := NewResourceObservationService(repo, nil, nil, nil, false, nil, ProcessObservationDependencies{})
	fresh, e := second.Policy(context.Background())
	if e != nil || fresh.Version != 2 || fresh.MaxMetrics != 1 || fresh.TimeoutSeconds != 1 {
		t.Fatal(fresh, e)
	}
	if _, e := service.UpdatePolicy(context.Background(), ResourceQueryPolicyInput{Version: before.Version, Budget: before.Budget}, 1); !errors.Is(e, resourcequery.ErrConflict) {
		t.Fatal(e)
	}
	fresh.MaxMetrics = 13
	if _, e := service.UpdatePolicy(context.Background(), ResourceQueryPolicyInput{Version: fresh.Version, Budget: fresh.Budget}, 1); !errors.Is(e, resourcequery.ErrInvalid) {
		t.Fatal(e)
	}
	after, e := service.Policy(context.Background())
	if e != nil || after.Version != 2 {
		t.Fatal("invalid policy saved", after, e)
	}
}
