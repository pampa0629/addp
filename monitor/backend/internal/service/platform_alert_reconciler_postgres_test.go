package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestIntegrationPostgresPlatformAlertSourceIsolationAndAtomicity(t *testing.T) {
	t.Setenv("ADDP_OBSERVABILITY_LOGS_ENABLED", "true")
	f := newLogRetryFixture(t, "webhook")
	db, ctx := f.db, context.Background()
	subject := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	pipeline := NewLogPipelineService(db, subject, emptyLogRegistry{}, f.n)
	if err := pipeline.Initialize(ctx, now); err != nil {
		t.Fatal(err)
	}
	ids := db.Model(&models.PlatformAlertIncident{}).Select("id").Where("subject_id=?", subject)
	eventIDs := db.Model(&models.PlatformAlertEvent{}).Select("id").Where("incident_id IN (?)", ids)
	t.Cleanup(func() {
		for _, entry := range []struct {
			model any
			where string
			value any
		}{
			{&models.PlatformAlertDelivery{}, "event_id IN (?)", eventIDs},
			{&models.PlatformAlertEvent{}, "incident_id IN (?)", ids},
			{&models.PlatformAlertIncident{}, "subject_id=?", subject},
			{&models.LogPipelineNode{}, "node=?", subject},
		} {
			if err := db.Where(entry.where, entry.value).Delete(entry.model).Error; err != nil {
				t.Error(err)
			}
			var count int64
			if err := db.Model(entry.model).Where(entry.where, entry.value).Count(&count).Error; err != nil || count != 0 {
				t.Errorf("platform alert fixture residue=%d err=%v", count, err)
			}
		}
	})
	logEvaluation := PlatformAlertEvaluation{Category: models.PlatformAlertCategoryLog, SubjectKind: models.PlatformAlertSubjectLogObserver, SubjectID: subject, Signal: "delivery_probe", Severity: "warning", Verdict: PlatformAlertRisk}
	evaluate := func(e PlatformAlertEvaluation, n *PlatformAlertNotifications, at time.Time) error {
		return db.Transaction(func(tx *gorm.DB) error { return reconcilePlatformAlert(tx, n, e, at) })
	}
	abort := errors.New("rollback evaluation source state")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := reconcilePlatformAlert(tx, f.n, logEvaluation, now); err != nil {
			return err
		}
		return abort
	}); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.PlatformAlertIncident{}).Where("subject_id=?", subject).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("incident escaped source transaction rollback", err)
	}
	// Eight independent transactions must produce one opened incident/event/delivery.
	var wg sync.WaitGroup
	errorsCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errorsCh <- evaluate(logEvaluation, f.n, now) }()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	var logIncident models.PlatformAlertIncident
	if err := db.Where("category=? AND subject_id=?", logEvaluation.Category, subject).Take(&logIncident).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.PlatformAlertEvent{}).Where("incident_id=?", logIncident.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent opened events=%d error=%v", count, err)
	}
	if err := db.Model(&models.PlatformAlertDelivery{}).Where("event_id IN (?) AND destination_id=?", eventIDs, f.target.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent opened deliveries=%d error=%v", count, err)
	}
	resource := PlatformAlertEvaluation{Category: models.PlatformAlertCategoryResource, SubjectKind: models.PlatformAlertSubjectHostNode, SubjectID: subject, Signal: logEvaluation.Signal, Severity: "warning", Verdict: PlatformAlertRisk}
	if err := evaluate(resource, nil, now); err != nil {
		t.Fatal("resource subject collided with log observer", err)
	}
	var resourceIncident models.PlatformAlertIncident
	if err := db.Where("category=? AND subject_id=?", resource.Category, subject).Take(&resourceIncident).Error; err != nil {
		t.Fatal(err)
	}
	var resourceEvent models.PlatformAlertEvent
	if err := db.Where("incident_id=?", resourceIncident.ID).Take(&resourceEvent).Error; err != nil {
		t.Fatal(err)
	}
	resourceDelivery := models.PlatformAlertDelivery{ID: uuid.NewString(), EventID: resourceEvent.ID, DestinationID: f.target.ID, Channel: "webhook", Recipients: models.StringList{}, Payload: `{"resource":true}`, Status: "dead", CreatedAt: now}
	if err := db.Create(&resourceDelivery).Error; err != nil {
		t.Fatal(err)
	}
	zero := 0
	if _, err := f.n.Retry(ctx, resourceDelivery.ID, LogDeliveryRetryInput{ExpectedManualRetryCount: &zero, DestinationVersion: f.target.Version}, now); !errors.Is(err, ErrLogNotFound) {
		t.Fatal("log-only retry exposed resource event", err)
	}
	deliveries, err := f.n.Deliveries(ctx, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, delivery := range deliveries.Data {
		if delivery.ID == resourceDelivery.ID {
			t.Fatal("log delivery list exposed resource event")
		}
	}
	for _, verdict := range []PlatformAlertVerdict{PlatformAlertUnknown, PlatformAlertPaused} {
		resource.Verdict = verdict
		if err := evaluate(resource, nil, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	var unchanged models.PlatformAlertIncident
	if err := db.First(&unchanged, resourceIncident.ID).Error; err != nil || unchanged.Status != "open" || unchanged.Version != resourceIncident.Version || !unchanged.LastObservedAt.Equal(now) {
		t.Fatal("unknown/paused rewrote active incident", err)
	}
	resource.Verdict = PlatformAlertNormal
	if err := evaluate(resource, nil, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	summary, err := pipeline.Summary(ctx, now.Add(2*time.Minute))
	if err != nil || len(summary.Incidents) != 1 || summary.Incidents[0].ID != logIncident.ID || summary.Incidents[0].Status != "open" || summary.Incidents[0].Node != subject {
		t.Fatal("resource recovery leaked into log view", err)
	}
	if _, err := pipeline.ManageIncident(ctx, resourceIncident.ID, 1, "acknowledge", nil, 1, now); !errors.Is(err, ErrLogNotFound) {
		t.Fatal("log management crossed source boundary", err)
	}
	logEvaluation.Severity = "critical"
	if err := evaluate(logEvaluation, f.n, now.Add(time.Minute)); !errors.Is(err, ErrLogConflict) {
		t.Fatal("severity overwritten within active incident", err)
	}
	logEvaluation.Severity = "warning"
	if err := evaluate(logEvaluation, f.n, now.Add(-time.Minute)); !errors.Is(err, ErrLogConflict) {
		t.Fatal("stale observation rewrote incident", err)
	}
}

func TestIntegrationPostgresPlatformLogAPICannotManageResourceSubscriptions(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	ctx, now := context.Background(), time.Now().UTC()
	for _, categories := range []models.StringList{{models.PlatformAlertCategoryResource}, {models.PlatformAlertCategoryLog, models.PlatformAlertCategoryResource}} {
		if err := f.db.Model(&f.target).Update("categories", categories).Error; err != nil {
			t.Fatal(err)
		}
		targets, err := f.n.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range targets {
			if target.ID == f.target.ID {
				t.Fatal("log-only list exposed broader subscription")
			}
		}
		if _, err := f.n.Save(ctx, f.target.ID, LogDestinationInput{Version: f.target.Version, Name: f.target.Name, Channel: f.target.Channel, URL: f.target.URL, EventTypes: []string{"opened"}}); !errors.Is(err, ErrLogNotFound) {
			t.Fatal("log-only update replaced broader subscription", err)
		}
		if _, err := f.n.SetSecret(ctx, f.target.ID, f.target.Version, "fixture-secret-123456789"); !errors.Is(err, ErrLogNotFound) {
			t.Fatal("log-only credential mutation accepted", err)
		}
		if err := f.n.Delete(ctx, f.target.ID, f.target.Version); !errors.Is(err, ErrLogNotFound) {
			t.Fatal("log-only delete accepted", err)
		}
		if err := f.n.Test(ctx, f.target.ID, now); !errors.Is(err, ErrLogNotFound) {
			t.Fatal("log-only test delivery accepted", err)
		}
		zero := 0
		if _, err := f.n.Retry(ctx, f.delivery.ID, LogDeliveryRetryInput{ExpectedManualRetryCount: &zero, DestinationVersion: f.target.Version}, now); !errors.Is(err, ErrLogRetryUnavailable) {
			t.Fatal("log-only retry accepted broader subscription", err)
		}
	}
	if len(f.webhook.messages) != 0 {
		t.Fatal("rejected cross-category operation sent external notification")
	}
}
