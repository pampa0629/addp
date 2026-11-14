package service

import (
	"context"
	"errors"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"strconv"
	"testing"
	"time"
)

// Controlled Owner facts exercise real persistence, rules and notification transactions.
func TestIntegrationPostgresCDCPerformanceRules(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-monitor-postgres")
	}
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = EnsureMonitorStore(db); err != nil {
		t.Fatal(err)
	}
	tenant := int(time.Now().UnixNano()%100000000) + 700000000
	taskID := "71"
	at := time.Now().UTC().Truncate(time.Second)
	t.Cleanup(func() {
		for _, model := range []any{&models.NotificationRoute{}, &models.WebhookDelivery{}, &models.AlertEvent{}, &models.AlertIncident{}, &models.AlertRule{}, &models.WebhookDestination{}, &commonExecution.TaskExecution{}} {
			if e := db.Where("tenant_id=?", tenant).Delete(model).Error; e != nil {
				t.Error(e)
			}
		}
	})
	createPostgresAlertExecution(t, db, tenant, taskID, "running", commonModels.JSONMap{"continuous": map[string]any{"capture": map[string]any{"generation": 1}}}, at)
	if err = db.Model(&commonExecution.TaskExecution{}).Where("tenant_id=?", tenant).Updates(map[string]any{"module": "transfer", "task_type": "sync", "source": "transfer"}).Error; err != nil {
		t.Fatal(err)
	}
	var e commonExecution.TaskExecution
	if err = db.Where("tenant_id=?", tenant).First(&e).Error; err != nil {
		t.Fatal(err)
	}
	web := NewWebhookService(db, []byte("addp-dev-encryption-key-2025!!!!"), true, "http://localhost:5170", nil)
	dest, err := web.CreateDestination(context.Background(), CreateWebhookDestinationInput{TenantID: tenant, Name: uuid.NewString(), URL: "http://127.0.0.1:18080/hook", Secret: "0123456789abcdef", Enabled: true, EventTypes: []string{"opened", "resolved"}})
	if err != nil {
		t.Fatal(err)
	}
	alerts := NewAlertService(db, NewNotificationService(web, nil))
	caps := commonModels.JSONString(monitorTaskCapabilitiesForTest(monitorTaskCapabilityForTest("sync", false)))
	rules := NewAlertRuleService(db, alerts, fakeTaskProviderLister{providers: []*commonModels.TaskProvider{{ModuleName: "transfer", Enabled: true, TaskProviderDeclaration: commonModels.TaskProviderDeclaration{Capabilities: &caps}}}})
	targetPage := &commonClient.TransferCDCRisks{Items: []commonClient.TransferCDCRiskItem{{TenantID: uint(tenant), TaskID: 71, Generation: 1, State: "observing", SourceType: "postgresql"}}}
	alerts.SetCDCRiskReader(&riskPageReader{pages: []*commonClient.TransferCDCRisks{targetPage, targetPage, targetPage}})
	for _, code := range cdcPerformanceCodes {
		input := CreateAlertRuleInput{TenantID: tenant, Name: code, Module: "transfer", TaskType: "sync", SourceTaskID: taskID, RuleType: code, Enabled: true, Severity: "warning", Routes: []AlertRuleRouteInput{{Channel: "webhook", DestinationID: dest.ID}}}
		if code == models.AlertRuleCDCBacklog {
			input.BacklogThreshold = "2"
		}
		if _, err = rules.Create(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	at = time.Now().UTC().Add(time.Second)
	feed := func(seconds int, offset, lag, count int64, delay float64, state string, unavailable bool) {
		t.Helper()
		now := at.Add(time.Duration(seconds) * time.Second)
		row := commonClient.TransferCDCRiskItem{TenantID: uint(tenant), TaskID: 71, Generation: 1, ExecutionID: e.ExecutionID, State: "observing", InitialSnapshotCompleted: performanceSnapshotCompleted(), ConsumptionState: state, Source: commonClient.CDCRiskObservation{State: "unknown"}, Buffer: commonClient.CDCRiskObservation{State: "unknown"}, Capture: commonClient.CDCCaptureObservation{State: "unknown"}, BufferSample: &commonClient.CDCBufferSample{CommittedOffset: strconv.FormatInt(offset, 10), LagOffsets: strconv.FormatInt(lag, 10), SampledAt: now, ExpiresAt: now.Add(30 * time.Second)}, DelaySample: &commonClient.CDCDelaySample{DelaySeconds: delay, EventCount: strconv.FormatInt(count, 10), LastEventAgeSeconds: 1, SampledAt: now, ExpiresAt: now.Add(30 * time.Second)}}
		reader := &riskPageReader{pages: []*commonClient.TransferCDCRisks{{Items: []commonClient.TransferCDCRiskItem{row}}}}
		if unavailable {
			reader.err = context.DeadlineExceeded
		}
		alerts.SetCDCRiskReader(reader)
		if err := alerts.Evaluate(context.Background(), now); err != nil {
			t.Fatal(err)
		}
	}
	for sec := 0; sec <= 300; sec += 15 {
		feed(sec, 10, 3, int64(sec+1), 80, "running", false)
	}
	var incidents []models.AlertIncident
	if err = db.Where("tenant_id=? AND signal_code IN ?", tenant, cdcPerformanceCodes).Order("id").Find(&incidents).Error; err != nil || len(incidents) != 3 {
		t.Fatal("three sustained rules", err, len(incidents))
	}
	for _, i := range incidents {
		if i.Status != "open" || i.Severity != "warning" {
			t.Fatal(i)
		}
		assertRuleOutboxCounts(t, db, i.ID, 1, 1, 0)
	}
	// Reloading the service uses the persisted window, not a wall-clock timer.
	reloaded, err := rules.List(context.Background(), tenant)
	if err != nil || len(reloaded) != 3 {
		t.Fatal(err)
	}
	for _, r := range reloaded {
		if !r.CDCWindow.Active {
			t.Fatal("window not persisted", r.RuleType)
		}
	}
	feed(315, 10, 3, 316, 80, "paused", false)
	var paused models.AlertIncident
	if err = db.First(&paused, incidents[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if paused.Details["evaluation_state"] != "paused" || !paused.LastObservedAt.Equal(incidents[0].LastObservedAt) {
		t.Fatal("pause changed evidence", paused)
	}
	feed(330, 10, 3, 331, 80, "running", true)
	if err = db.First(&paused, paused.ID).Error; err != nil || paused.Status != "open" || !paused.LastObservedAt.Equal(incidents[0].LastObservedAt) {
		t.Fatal("unknown recovered", err)
	}
	for sec := 345; sec <= 435; sec += 15 {
		feed(sec, int64(sec/30)*30, 1, int64(sec), 20, "running", false)
	}
	for sec := 450; sec <= 510; sec += 15 {
		feed(sec, int64(sec), 0, int64(sec), 20, "running", false)
	}
	for _, old := range incidents {
		var current models.AlertIncident
		if err = db.First(&current, old.ID).Error; err != nil || current.Status != "resolved" {
			t.Fatal("recovery failed", current.SignalCode, err)
		}
		assertRuleOutboxCounts(t, db, old.ID, 2, 2, 0)
	}
	var backlogRule models.AlertRule
	if err = db.Where("tenant_id=? AND rule_type=?", tenant, models.AlertRuleCDCBacklog).First(&backlogRule).Error; err != nil {
		t.Fatal(err)
	}
	for sec := 525; sec <= 825; sec += 15 {
		feed(sec, int64(sec), 3, int64(sec), 20, "running", false)
	}
	var editedIncident models.AlertIncident
	if err = db.Where("alert_rule_id=? AND status='open'", backlogRule.ID).First(&editedIncident).Error; err != nil {
		t.Fatal("backlog did not reopen", err)
	}
	assertRuleOutboxCounts(t, db, editedIncident.ID, 1, 1, 0)
	limit := "9007199254740993"
	updated, err := rules.Update(context.Background(), UpdateAlertRuleInput{TenantID: tenant, ID: backlogRule.ID, BacklogThreshold: &limit}, at.Add(840*time.Second))
	if err != nil || updated.BacklogThreshold != limit || updated.CDCWindow.Active || updated.CDCWindow.TriggerSince != nil || updated.CDCWindow.ResetAt == nil {
		t.Fatal("rule edit retained old evidence", err)
	}
	if err = db.First(&editedIncident, editedIncident.ID).Error; err != nil || editedIncident.Status != "resolved" {
		t.Fatal("rule change did not close old incident", err)
	}
	assertRuleOutboxCounts(t, db, editedIncident.ID, 2, 2, 0)
	invalid := "01"
	if _, err = rules.Update(context.Background(), UpdateAlertRuleInput{TenantID: tenant, ID: backlogRule.ID, BacklogThreshold: &invalid}, at.Add(841*time.Second)); !errors.Is(err, ErrAlertRuleInvalid) {
		t.Fatal("invalid edit admitted", err)
	}

}
