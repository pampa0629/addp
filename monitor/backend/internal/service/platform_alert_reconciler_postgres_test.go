package service

import (
	"context"
	"encoding/json"
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
	evaluate := func(e PlatformAlertEvaluation, n *PlatformAlertService, at time.Time) error {
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
	if _, err := f.n.Retry(ctx, resourceDelivery.ID, PlatformAlertDeliveryRetryInput{ExpectedManualRetryCount: &zero, DestinationVersion: f.target.Version}, now); !errors.Is(err, ErrLogRetryUnavailable) {
		t.Fatal("unsubscribed resource retry accepted", err)
	}
	deliveries, err := f.n.Deliveries(ctx, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	foundResource := false
	for _, delivery := range deliveries.Data {
		if delivery.ID == resourceDelivery.ID {
			foundResource = delivery.Category == models.PlatformAlertCategoryResource
		}
	}
	if !foundResource {
		t.Fatal("generic delivery list omitted original resource category")
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
	if err != nil || summary.ActiveIncidents != 1 || activeLogTestIncident(t, db, subject).ID != logIncident.ID {
		t.Fatal("resource recovery leaked into log view", err)
	}
	if _, err := f.n.ManageIncident(ctx, resourceIncident.ID, 1, "acknowledge", nil, 1, now); !errors.Is(err, ErrLogConflict) {
		t.Fatal("resolved resource did not enforce generic management CAS", err)
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

func TestIntegrationPostgresPlatformAlertExplicitCategoriesAndCAS(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	ctx := context.Background()
	for _, categories := range [][]string{nil, {}, {"resource", "resource"}, {"unknown"}} {
		_, err := f.n.Save(ctx, 0, PlatformAlertDestinationInput{Name: uuid.NewString(), Channel: "webhook", URL: f.target.URL, Categories: categories, EventTypes: []string{"opened"}})
		if !errors.Is(err, ErrLogInvalid) {
			t.Fatal("invalid/implicit categories accepted", categories, err)
		}
	}
	input := PlatformAlertDestinationInput{Name: f.target.Name, Channel: f.target.Channel, URL: f.target.URL, Categories: []string{"resource", "log_pipeline"}, EventTypes: []string{"opened"}, Version: f.target.Version}
	target, err := f.n.Save(ctx, f.target.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(target.Categories) != 2 || target.Categories[0] != "log_pipeline" {
		t.Fatal("categories not explicit and canonical", target)
	}
	if _, err = f.n.Save(ctx, f.target.ID, input); !errors.Is(err, ErrLogConflict) {
		t.Fatal("stale category mutation accepted", err)
	}
	rows, err := f.n.Destinations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.ID == target.ID {
			found = len(row.Categories) == 2 && row.SecretCiphertext == target.SecretCiphertext
		}
	}
	if !found {
		t.Fatal("generic management omitted subscribed target")
	}
}

func TestIntegrationPostgresPlatformAlertResourceRoutingAndManagement(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	db, ctx := f.db, context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	target, err := f.n.Save(ctx, 0, PlatformAlertDestinationInput{Categories: []string{"resource"}, Name: "resource-" + uuid.NewString(), Channel: "webhook", URL: f.target.URL, EventTypes: []string{"opened", "resolved"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("destination_id=?", target.ID).Delete(&models.PlatformAlertDelivery{})
		db.Delete(&models.PlatformAlertDestination{}, target.ID)
	})
	target, err = f.n.SetSecret(ctx, target.ID, target.Version, "fixture-secret-resource-123456")
	if err != nil {
		t.Fatal(err)
	}
	target, err = f.n.Save(ctx, target.ID, PlatformAlertDestinationInput{Version: target.Version, Categories: []string{"resource"}, Name: target.Name, Channel: "webhook", URL: target.URL, EventTypes: []string{"opened", "resolved"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	subject := uuid.NewString()
	t.Cleanup(func() {
		db.Exec(`DELETE FROM monitor.platform_alert_deliveries WHERE event_id IN (SELECT id FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?))`, subject)
		db.Exec(`DELETE FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?)`, subject)
		db.Where("subject_id=?", subject).Delete(&models.PlatformAlertIncident{})
	})
	e := PlatformAlertEvaluation{Category: models.PlatformAlertCategoryResource, SubjectKind: models.PlatformAlertSubjectHostNode, SubjectID: subject, Signal: "node.memory.used_percent", Severity: "warning", Verdict: PlatformAlertRisk}
	if err := db.Transaction(func(tx *gorm.DB) error { return reconcilePlatformAlert(tx, f.n, e, now) }); err != nil {
		t.Fatal(err)
	}
	list, err := f.n.Incidents(ctx, 1, 20, "resource", "active", subject)
	if err != nil || list.Total != 1 {
		t.Fatal("resource list", list, err)
	}
	incident := list.Data[0]
	row, err := f.n.ManageIncident(ctx, incident.ID, incident.Version, "acknowledge", nil, 1, now)
	if err != nil || row.Status != "acknowledged" {
		t.Fatal("acknowledge", row, err)
	}
	if _, err := f.n.ManageIncident(ctx, incident.ID, incident.Version, "acknowledge", nil, 1, now); !errors.Is(err, ErrLogConflict) {
		t.Fatal("stale action accepted", err)
	}
	until := now.Add(time.Hour)
	row, err = f.n.ManageIncident(ctx, row.ID, row.Version, "suppress", &until, 1, now)
	if err != nil || row.SuppressedUntil == nil {
		t.Fatal("suppress", row, err)
	}
	var delivery models.PlatformAlertDelivery
	if err := db.Joins("JOIN monitor.platform_alert_events e ON e.id=platform_alert_deliveries.event_id").Where("e.incident_id=?", incident.ID).Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.DestinationID != target.ID {
		t.Fatal("resource sent to pre-existing log subscriber", delivery.DestinationID)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(delivery.Payload), &payload); err != nil || payload["schema"] != "addp.platform-resource-alert/v1" || payload["subject_id"] != subject || payload["node"] != nil {
		t.Fatal("wrong formal resource payload", payload, err)
	}
	if _, err := wecomPlatformAlertPayload(delivery.Payload, delivery.ID); err != nil {
		t.Fatal("resource WeCom formatter", err)
	}
	if err := f.n.send(ctx, delivery, now); err != nil {
		t.Fatal("resource webhook formatter", err)
	}
	// Recovery records one same-source event. Active suppression applies to its
	// notification without rewriting the opened event or its original payload.
	e.Verdict = PlatformAlertNormal
	if err := db.Transaction(func(tx *gorm.DB) error { return reconcilePlatformAlert(tx, f.n, e, now.Add(time.Minute)) }); err != nil {
		t.Fatal(err)
	}
	list, err = f.n.Incidents(ctx, 1, 20, "resource", "resolved", subject)
	if err != nil || list.Total != 1 {
		t.Fatal("recovery", err)
	}
	if _, err := f.n.ManageIncident(ctx, row.ID, list.Data[0].Version, "acknowledge", nil, 1, now.Add(time.Minute)); !errors.Is(err, ErrLogConflict) {
		t.Fatal("resolved event handled", err)
	}
	var events int64
	db.Model(&models.PlatformAlertEvent{}).Where("incident_id=?", incident.ID).Count(&events)
	if events != 2 {
		t.Fatal("lifecycle duplicated", events)
	}
}
