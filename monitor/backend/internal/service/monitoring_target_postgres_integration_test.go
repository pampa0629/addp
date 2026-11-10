package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresMonitoringTargetsAtomicBudgetAndCAS(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := EnsureMonitorStore(db); err != nil {
		t.Fatal(err)
	}

	r := repository.NewMonitoringTargetRepository(db, metricsdiscovery.DefaultExternalTargetLimit)
	ctx := context.Background()
	existing, err := r.Snapshot(ctx)
	if err != nil || len(existing) != 0 {
		t.Fatalf("standard test requires clean target fixture: count=%d err=%v", len(existing), err)
	}
	limit := metricsdiscovery.DefaultExternalTargetLimit
	// Leave one slot for the two concurrent creates and keep a spare ID for a
	// contention-free assertion that the full budget rejects another write.
	seedCount := limit - 1
	ids := make([]string, limit+2)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	t.Cleanup(func() {
		if err := db.Where("id IN ?", ids).Delete(&models.MonitoringTarget{}).Error; err != nil {
			t.Error(err)
		}
		var remaining int64
		if err := db.Model(&models.MonitoringTarget{}).Where("id IN ?", ids).Count(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("target cleanup remaining=%d err=%v", remaining, err)
		}
	})
	newTarget := func(i int) metricsdiscovery.NodeTarget {
		return metricsdiscovery.NodeTarget{ID: ids[i], Version: 1, Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: uuid.NewString()}, MonitorKind: "host_resources", Source: metricsdiscovery.NodeSource{Type: "node_exporter", Endpoint: fmt.Sprintf("https://127.0.0.1:%d/metrics", 10000+i)}, Enabled: true}
	}
	for i := 0; i < seedCount; i++ {
		if _, err := r.Mutate(ctx, newTarget(i), true, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for i := seedCount; i < seedCount+2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			row := newTarget(i)
			var err error
			for attempt := 0; attempt < 100; attempt++ {
				_, err = r.Mutate(ctx, row, true, false, nil)
				if !errors.Is(err, repository.ErrTargetBusy) {
					break
				}
			}
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, metricsdiscovery.ErrBudgetExceeded) && !errors.Is(err, repository.ErrTargetBusy) {
			t.Fatal(err)
		}
	}
	rows, err := r.Snapshot(ctx)
	if err != nil || succeeded != 1 || len(rows) != limit {
		t.Fatalf("concurrent budget succeeded=%d count=%d want=%d err=%v", succeeded, len(rows), limit, err)
	}
	if _, err := r.Mutate(ctx, newTarget(limit+1), true, false, nil); !errors.Is(err, metricsdiscovery.ErrBudgetExceeded) {
		t.Fatalf("full budget did not reject another create: %v", err)
	}
	row, err := r.Get(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	row.Enabled = false
	lowered := repository.NewMonitoringTargetRepository(db, limit-2)
	updated, err := lowered.Mutate(ctx, row, false, false, nil)
	if err != nil || updated.Version != 2 {
		t.Fatalf("disable CAS after lowering the budget below the enabled count: %v", err)
	}
	if _, err := r.Mutate(ctx, row, false, false, nil); !errors.Is(err, repository.ErrTargetConflict) {
		t.Fatalf("stale update: %v", err)
	}
	wrong := updated
	wrong.Subject.NodeID = uuid.NewString()
	if _, err := r.Mutate(ctx, wrong, false, false, nil); !errors.Is(err, repository.ErrTargetConflict) {
		t.Fatalf("mutable subject: %v", err)
	}
	if _, err := r.Mutate(ctx, row, false, true, nil); !errors.Is(err, repository.ErrTargetConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	if _, err := r.Mutate(ctx, updated, false, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, updated.ID); !errors.Is(err, repository.ErrTargetNotFound) {
		t.Fatal("deleted target visible")
	}
}
