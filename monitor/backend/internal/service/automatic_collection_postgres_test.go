package service

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresAutomaticCollectionCASAndAlertBoundary(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor PostgreSQL gate")
	}
	ctx := context.Background()
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = EnsureMonitorStore(db); err != nil {
		t.Fatal(err)
	}
	store := repository.NewAutomaticCollectionRepository(db)
	now := time.Now().UTC()
	module := "monitor-" + uuid.NewString()
	identity := commonmodels.ObservabilityModuleIdentity{
		ModuleName: module, Role: "worker", RuntimeName: "sync", InstanceID: "worker-before", LeaseExpiresAt: now.Add(time.Minute),
		ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:9100/metrics"}, ProcessStartedAt: &now,
	}
	key := metricsdiscovery.ProcessCollectionScope(identity)
	t.Cleanup(func() {
		if err := db.Where("id = ?", key.ID).Delete(&models.AutomaticCollectionScope{}).Error; err != nil {
			t.Error(err)
		}
	})
	facts := &resourceRuleIdentity{processFacts: []commonmodels.ObservabilityModuleIdentity{identity}, disabled: true}
	o := NewResourceObservationService(nil, nil, nil, nil, true, resourceSourcePolicy(t), ProcessObservationDependencies{}).WithAutomaticCollection(store)
	before, err := runtimeSources(ctx, o, facts, 1, func(commonmodels.ObservabilityModuleIdentity) bool { return true })
	if err != nil {
		t.Fatal(err)
	}

	// Two first writers observe the same default revision. Only one may save it.
	args := AutomaticCollectionInput{Enabled: new(bool), Version: 1}
	svc := NewMonitoringTargetService(&targetTestStore{}, &targetTestNodes{}, facts, false, nil).WithAutomaticCollection(store, nil)
	page, err := svc.AutomaticScopes(ctx, "fixture-user")
	if err != nil || len(page.Data) == 0 {
		t.Fatal(page, err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Save(ctx, key, *args.Enabled, args.Version)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, repository.ErrTargetConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("default revision: successes=%d conflicts=%d", successes, conflicts)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		current, err := before.collectionCurrent(tx)
		if current || err != nil {
			t.Fatalf("pre-pause evidence is still current: %v, %v", current, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	facts.processFacts[0].InstanceID = "worker-after"
	if _, err := runtimeSources(ctx, o, facts, 1, func(commonmodels.ObservabilityModuleIdentity) bool { return true }); !errors.Is(err, errCollectionPaused) {
		t.Fatalf("new replica ignored saved pause: %v", err)
	}
	facts.processFacts[0].RuntimeName = "continuous_sync"
	if _, err := runtimeSources(ctx, o, facts, 1, func(commonmodels.ObservabilityModuleIdentity) bool { return true }); err != nil {
		t.Fatalf("sibling purpose affected: %v", err)
	}
	facts.err = resourcequery.ErrUnavailable // previously saved scope remains manageable
	enabled := true
	row, err := svc.SaveAutomaticScope(ctx, "fixture-user", key.ID, AutomaticCollectionInput{Enabled: &enabled, Version: 2})
	if err != nil || !row.Enabled || row.Version != 3 || row.ChangedAt.Before(now) {
		t.Fatalf("resume under missing source: %#v %v", row, err)
	}
	facts.err, facts.processFacts[0].RuntimeName = nil, "sync"
	after, err := runtimeSources(ctx, o, facts, 1, func(commonmodels.ObservabilityModuleIdentity) bool { return true })
	if err != nil || after.collectionChanged.Before(now) || after.signature == before.signature {
		t.Fatalf("post-resume gate did not advance: %#v %v", after, err)
	}
}
