package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresAlertRuleLifecycleAndRoutedOutbox(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	if err := EnsureMonitorStore(db); err != nil {
		t.Fatalf("ensure monitor store: %v", err)
	}

	tenantID := int(time.Now().UnixNano()%100000000) + 600000000
	taskID := "alert-rule-" + uuid.NewString()
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.NotificationRoute{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.EmailDelivery{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.WebhookDelivery{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.AlertEvent{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.AlertIncident{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.AlertRule{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.EmailDestination{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.WebhookDestination{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
	})

	now := time.Now().UTC()
	createPostgresAlertExecution(t, db, tenantID, taskID, commonExecution.ExecutionStatusFailed, commonModels.JSONMap{}, now)
	webhookService := NewWebhookService(db, []byte("addp-dev-encryption-key-2025!!!!"), true, "http://localhost:5170", nil)
	webhookDestination, err := webhookService.CreateDestination(context.Background(), CreateWebhookDestinationInput{
		TenantID: tenantID, Name: "rule-webhook", URL: "http://127.0.0.1:18080/hook", Secret: "0123456789abcdef",
		Enabled: true, EventTypes: []string{monitorModels.AlertEventOpened, monitorModels.AlertEventResolved},
	})
	if err != nil {
		t.Fatal(err)
	}
	emailService := NewEmailService(db, "http://localhost:5170", nil)
	emailDestination, err := emailService.CreateDestination(context.Background(), CreateEmailDestinationInput{
		TenantID: tenantID, Name: "rule-email", Recipients: []string{"ops@example.com"}, Enabled: true,
		EventTypes: []string{monitorModels.AlertEventOpened, monitorModels.AlertEventResolved},
	})
	if err != nil {
		t.Fatal(err)
	}
	alertService := NewAlertService(db, NewNotificationService(webhookService, emailService))
	capabilities := commonModels.JSONString(monitorTaskCapabilitiesForTest(
		monitorTaskCapabilityForTest(commonExecution.TaskTypeWorkflow, false),
	))
	ruleService := NewAlertRuleService(db, alertService, fakeTaskProviderLister{providers: []*commonModels.TaskProvider{{
		ModuleName: commonExecution.ModuleDevelop, Capabilities: &capabilities, IsEnabled: true,
	}}})
	rule, err := ruleService.Create(context.Background(), CreateAlertRuleInput{
		TenantID: tenantID, Name: "latest failure", Module: commonExecution.ModuleDevelop,
		TaskType: commonExecution.TaskTypeWorkflow, SourceTaskID: taskID, SourceTaskName: "integration workflow",
		RuleType: monitorModels.AlertRuleLastTerminalFailed, FailureThreshold: 1,
		Severity: monitorModels.AlertSeverityCritical, Enabled: true,
		Routes: []AlertRuleRouteInput{
			{Channel: monitorModels.NotificationChannelWebhook, DestinationID: webhookDestination.ID},
			{Channel: monitorModels.NotificationChannelEmail, DestinationID: emailDestination.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := alertService.Evaluate(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var incident monitorModels.AlertIncident
	if err := db.Where("alert_rule_id = ?", rule.ID).First(&incident).Error; err != nil {
		t.Fatal(err)
	}
	if incident.Status != monitorModels.AlertStatusOpen || incident.RuleID != rule.RuleID {
		t.Fatalf("opened incident = %#v", incident)
	}
	assertRuleOutboxCounts(t, db, incident.ID, 1, 1, 1)

	createPostgresAlertExecution(t, db, tenantID, taskID, commonExecution.ExecutionStatusPending, commonModels.JSONMap{}, now.Add(2*time.Second))
	createPostgresAlertExecution(t, db, tenantID, taskID, commonExecution.ExecutionStatusCancelled, commonModels.JSONMap{}, now.Add(3*time.Second))
	if err := alertService.Evaluate(context.Background(), now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&incident, incident.ID).Error; err != nil {
		t.Fatal(err)
	}
	if incident.Status != monitorModels.AlertStatusOpen {
		t.Fatalf("pending/cancelled resolved incident = %#v", incident)
	}

	createPostgresAlertExecution(t, db, tenantID, taskID, commonExecution.ExecutionStatusSuccess, commonModels.JSONMap{}, now.Add(5*time.Second))
	if err := alertService.Evaluate(context.Background(), now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&incident, incident.ID).Error; err != nil {
		t.Fatal(err)
	}
	if incident.Status != monitorModels.AlertStatusResolved {
		t.Fatalf("success did not resolve incident = %#v", incident)
	}
	assertRuleOutboxCounts(t, db, incident.ID, 2, 2, 2)

	createPostgresAlertExecution(t, db, tenantID, taskID, commonExecution.ExecutionStatusFailed, commonModels.JSONMap{}, now.Add(7*time.Second))
	if err := alertService.Evaluate(context.Background(), now.Add(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	var reopened monitorModels.AlertIncident
	if err := db.Where("alert_rule_id = ? AND status = ?", rule.ID, monitorModels.AlertStatusOpen).First(&reopened).Error; err != nil {
		t.Fatal(err)
	}
	staleSignals, err := alertService.evaluateRuleSignals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := ruleService.Update(context.Background(), UpdateAlertRuleInput{
		TenantID: tenantID, ID: rule.ID, Enabled: &disabled,
	}, now.Add(9*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&reopened, reopened.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reopened.Status != monitorModels.AlertStatusResolved {
		t.Fatalf("disabling rule did not resolve incident = %#v", reopened)
	}
	assertRuleOutboxCounts(t, db, reopened.ID, 2, 2, 2)
	if err := alertService.reconcileSignals(context.Background(), staleSignals, now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	var activeIncidentCount int64
	if err := db.Model(&monitorModels.AlertIncident{}).
		Where("alert_rule_id = ? AND status IN ?", rule.ID, []string{monitorModels.AlertStatusOpen, monitorModels.AlertStatusAcknowledged}).
		Count(&activeIncidentCount).Error; err != nil {
		t.Fatal(err)
	}
	if activeIncidentCount != 0 {
		t.Fatalf("stale evaluator reopened %d incidents after disable", activeIncidentCount)
	}

	continuousTaskID := taskID + "-continuous"
	createPostgresAlertExecution(t, db, tenantID, continuousTaskID, commonExecution.ExecutionStatusFailed,
		commonModels.JSONMap{"continuous": map[string]interface{}{}}, now.Add(10*time.Second))
	_, err = ruleService.Create(context.Background(), CreateAlertRuleInput{
		TenantID: tenantID, Name: "invalid continuous", Module: commonExecution.ModuleDevelop,
		TaskType: commonExecution.TaskTypeWorkflow, SourceTaskID: continuousTaskID,
		RuleType: monitorModels.AlertRuleLastTerminalFailed, FailureThreshold: 1,
		Severity: monitorModels.AlertSeverityWarning, Enabled: true,
	})
	if !errors.Is(err, ErrAlertRuleInvalid) {
		t.Fatalf("continuous target create error = %v", err)
	}
}

func createPostgresAlertExecution(t *testing.T, db *gorm.DB, tenantID int, taskID, status string, metadata commonModels.JSONMap, now time.Time) {
	t.Helper()
	completedAt := now
	if status == commonExecution.ExecutionStatusPending || status == commonExecution.ExecutionStatusRunning {
		completedAt = time.Time{}
	}
	execution := commonExecution.TaskExecution{
		TenantID: tenantID, ExecutionID: uuid.NewString(), Module: commonExecution.ModuleDevelop,
		TaskType: commonExecution.TaskTypeWorkflow, Source: commonExecution.ModuleDevelop,
		SourceTaskID: &taskID, Status: status, TriggerType: commonExecution.TriggerTypeManual,
		Metadata: metadata, CreatedAt: now, UpdatedAt: now,
	}
	if !completedAt.IsZero() {
		execution.CompletedAt = &completedAt
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
}

func assertRuleOutboxCounts(t *testing.T, db *gorm.DB, incidentID uint, events, webhooks, emails int64) {
	t.Helper()
	for _, check := range []struct {
		name     string
		model    interface{}
		expected int64
	}{
		{name: "events", model: &monitorModels.AlertEvent{}, expected: events},
		{name: "webhooks", model: &monitorModels.WebhookDelivery{}, expected: webhooks},
		{name: "emails", model: &monitorModels.EmailDelivery{}, expected: emails},
	} {
		var count int64
		if err := db.Model(check.model).Where("incident_id = ?", incidentID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != check.expected {
			t.Fatalf("%s count = %d, want %d", check.name, count, check.expected)
		}
	}
}
