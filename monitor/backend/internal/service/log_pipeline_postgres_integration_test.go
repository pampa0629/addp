package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type emptyLogRegistry struct{}

func (emptyLogRegistry) ListModules(context.Context) ([]*client.ModuleInfo, error) { return nil, nil }
func TestIntegrationPostgresPlatformLogLifecycleOutboxAndIsolation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("PostgreSQL owner gate required")
	}
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = EnsureMonitorStore(db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	nodeID := "pipeline-" + uuid.NewString()
	bootID := uuid.NewString()
	now := time.Now().UTC()
	var accepted atomic.Int64
	var fail atomic.Bool
	fail.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ADDP-Webhook-Signature") == "" || r.Header.Get("X-ADDP-Webhook-ID") == "" {
			t.Error("missing signature or id")
		}
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		accepted.Add(1)
		w.WriteHeader(202)
	}))
	defer server.Close()
	notifications := NewPlatformLogNotifications(db, []byte("addp-dev-encryption-key-2025!!!!"), true, NewHTTPWebhookSender(time.Second, true), nil, 3, 30*time.Second, time.Second, 5*time.Second)
	pipeline := NewLogPipelineService(db, nodeID, emptyLogRegistry{}, notifications)
	if err = pipeline.Initialize(ctx, now); err != nil {
		t.Fatal(err)
	}
	var destination models.PlatformLogDestination
	t.Cleanup(func() {
		var ids []uint
		if err := db.Model(&models.PlatformLogIncident{}).Where("node=?", nodeID).Pluck("id", &ids).Error; err != nil {
			t.Error(err)
		}
		for _, query := range []struct {
			model any
			where string
			arg   any
		}{{&models.PlatformLogDelivery{}, "destination_id=?", destination.ID}, {&models.PlatformLogEvent{}, "incident_id IN ?", ids}, {&models.PlatformLogIncident{}, "node=?", nodeID}, {&models.PlatformLogDestination{}, "id=?", destination.ID}, {&models.LogObserverBoot{}, "node=?", nodeID}, {&models.LogPipelineNode{}, "node=?", nodeID}} {
			if err := db.Where(query.where, query.arg).Delete(query.model).Error; err != nil {
				t.Error(err)
			}
		}
		var remaining int64
		if err := db.Model(&models.LogPipelineNode{}).Where("node=?", nodeID).Count(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("cleanup remaining=%d err=%v", remaining, err)
		}
	})
	input := LogDestinationInput{Name: nodeID, Channel: "webhook", URL: server.URL, EventTypes: []string{"opened", "resolved"}, Enabled: false}
	destination, err = notifications.Save(ctx, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	destination, err = notifications.SetSecret(ctx, destination.ID, destination.Version, "fixture-secret-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	input.Version = destination.Version
	input.Enabled = true
	destination, err = notifications.Save(ctx, destination.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = notifications.Save(ctx, destination.ID, input); !errors.Is(err, ErrLogConflict) {
		t.Fatalf("old version accepted: %v", err)
	}
	obs := healthyLogObservation(now, 1)
	obs.Node = nodeID
	obs.BootID = bootID
	obs.Collector.Dropped = 100
	if err = pipeline.Ingest(ctx, obs, now); err != nil {
		t.Fatal(err)
	}
	summary, err := pipeline.Summary(ctx, now)
	if err != nil || summary.Health != "healthy" || len(summary.Incidents) != 0 {
		t.Fatalf("initial summary %#v err %v", summary, err)
	}
	now = now.Add(30 * time.Second)
	obs.Sequence++
	obs.SampledAt = now
	obs.Collector.Dropped++
	var wg sync.WaitGroup
	for n := 0; n < 5; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := pipeline.Ingest(ctx, obs, now); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	summary, err = pipeline.Summary(ctx, now)
	if err != nil || len(summary.Incidents) != 1 || summary.Health != "alert" {
		t.Fatalf("dedup %#v err %v", summary, err)
	}
	incident := summary.Incidents[0]
	var events int64
	if err = db.Model(&models.PlatformLogEvent{}).Where("incident_id=?", incident.ID).Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("opened events=%d err=%v", events, err)
	}
	if processed, err := notifications.DispatchOnce(ctx, now); err != nil || !processed {
		t.Fatalf("failed delivery processed=%v err=%v", processed, err)
	}
	attempts, err := notifications.Deliveries(ctx, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range attempts.Data {
		if d.DestinationID == destination.ID {
			found = true
			if d.Status != "pending" || d.AttemptCount != 1 {
				t.Fatalf("retry state %#v", d)
			}
		}
	}
	if !found {
		t.Fatal("missing platform delivery")
	}
	fail.Store(false)
	if processed, err := notifications.DispatchOnce(ctx, now.Add(2*time.Second)); err != nil || !processed {
		t.Fatalf("retry processed=%v err=%v", processed, err)
	}
	altered := obs
	altered.Collector.Dropped++
	if err = pipeline.Ingest(ctx, altered, now); !errors.Is(err, ErrLogConflict) {
		t.Fatalf("same sequence changed payload accepted: %v", err)
	}
	for n := 0; n < 3; n++ {
		now = now.Add(30 * time.Second)
		obs.Sequence++
		obs.SampledAt = now
		if err = pipeline.Ingest(ctx, obs, now); err != nil {
			t.Fatal(err)
		}
	}
	summary, err = pipeline.Summary(ctx, now)
	if err != nil || len(summary.Incidents) != 0 {
		t.Fatalf("recovery %#v err %v", summary, err)
	}
	if processed, err := notifications.DispatchOnce(ctx, now); err != nil || !processed {
		t.Fatalf("recovery delivery=%v %v", processed, err)
	}
	if accepted.Load() != 2 {
		t.Fatalf("accepted deliveries %d, expected one opened and one resolved", accepted.Load())
	}
	// A duplicate report and repeated stale sweeps never manufacture duplicate lifecycle events.
	if err = pipeline.Ingest(ctx, obs, now); err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&models.PlatformLogEvent{}).Where("incident_id=?", incident.ID).Count(&events).Error; err != nil || events != 2 {
		t.Fatalf("lifecycle events=%d err=%v", events, err)
	}
	if err = pipeline.CheckStale(ctx, now.Add(121*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = pipeline.CheckStale(ctx, now.Add(136*time.Second)); err != nil {
		t.Fatal(err)
	}
	summary, err = pipeline.Summary(ctx, now.Add(136*time.Second))
	if err != nil || summary.Health != "unknown" || len(summary.Incidents) != 1 || summary.Incidents[0].Signal != "observation_missing" {
		t.Fatalf("stale summary %#v err=%v", summary, err)
	}
	// Changing boots rebuilds baselines; a retired boot cannot replace the new one.
	now = now.Add(150 * time.Second)
	obs.BootID = uuid.NewString()
	obs.Sequence = 1
	obs.SampledAt = now
	obs.Collector.Dropped = 0
	if err = pipeline.Ingest(ctx, obs, now); err != nil {
		t.Fatal(err)
	}
	old := obs
	old.BootID = bootID
	old.Sequence = 100
	old.SampledAt = now.Add(time.Second)
	if err = pipeline.Ingest(ctx, old, old.SampledAt); !errors.Is(err, ErrLogConflict) {
		t.Fatalf("retired observer boot accepted: %v", err)
	}
	// A crashed final attempt must not cause unlimited network retries after its lease expires.
	result := db.Model(&models.PlatformLogDelivery{}).Where("destination_id=? AND status='pending'", destination.ID).Updates(map[string]any{"status": "delivering", "attempt_count": 3, "lease_expires_at": now.Add(-time.Second)})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("seed expired final claim: %d %v", result.RowsAffected, result.Error)
	}
	if processed, err := notifications.DispatchOnce(ctx, now); err != nil || !processed {
		t.Fatalf("expired final claim processed=%v err=%v", processed, err)
	}
	var exhausted models.PlatformLogDelivery
	if err := db.Where("destination_id=? AND status='dead'", destination.ID).Take(&exhausted).Error; err != nil {
		t.Fatal(err)
	}
	if exhausted.SecretCiphertext != "" || exhausted.NextAttemptAt != nil || exhausted.ClaimID != "" || accepted.Load() != 2 {
		t.Fatal("exhausted delivery retained a credential or retried network send")
	}

}
