package service

import (
	"context"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	commonUtils "github.com/addp/common/utils"
	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAlertEvaluationOpensResolvesAndReopensIncident(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:monitor-alert-evaluator?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"common", "monitor"} {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS " + schema).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, execution_id TEXT NOT NULL UNIQUE,
		module TEXT NOT NULL, task_type TEXT NOT NULL, source TEXT NOT NULL, source_task_id TEXT,
		source_task_name TEXT, parent_execution_id TEXT,
		status TEXT NOT NULL, progress INTEGER DEFAULT 0, trigger_type TEXT NOT NULL,
		metadata JSON, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE monitor.alert_incidents (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, module TEXT NOT NULL,
		task_type TEXT NOT NULL, source_task_id TEXT NOT NULL, execution_id TEXT NOT NULL,
		alert_rule_id INTEGER, rule_id TEXT NOT NULL DEFAULT '', rule_name TEXT NOT NULL DEFAULT '',
		signal_code TEXT NOT NULL, fingerprint TEXT NOT NULL, severity TEXT NOT NULL, status TEXT NOT NULL,
		details JSON NOT NULL, opened_at DATETIME NOT NULL, last_observed_at DATETIME NOT NULL,
		acknowledged_at DATETIME, acknowledged_by TEXT, suppressed_until DATETIME, resolved_at DATETIME,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX monitor.uq_monitor_active_alert_fingerprint
		ON alert_incidents (fingerprint) WHERE status IN ('open', 'acknowledged')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE monitor.alert_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT, event_id TEXT NOT NULL UNIQUE, tenant_id INTEGER NOT NULL,
		incident_id INTEGER NOT NULL, event_type TEXT NOT NULL, from_severity TEXT, to_severity TEXT NOT NULL,
		occurred_at DATETIME NOT NULL, created_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE monitor.alert_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT, rule_id TEXT NOT NULL UNIQUE, tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL, module TEXT NOT NULL, task_type TEXT NOT NULL, source_task_id TEXT NOT NULL,
		source_task_name TEXT, rule_type TEXT NOT NULL, failure_threshold INTEGER NOT NULL,
		severity TEXT NOT NULL, enabled BOOLEAN NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE monitor.notification_routes (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, alert_rule_id INTEGER NOT NULL,
		channel TEXT NOT NULL, destination_id INTEGER NOT NULL, created_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE monitor.webhook_destinations (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, url TEXT NOT NULL,
		secret_ciphertext TEXT NOT NULL, enabled BOOLEAN NOT NULL, event_types JSON NOT NULL,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE monitor.webhook_deliveries (
		id INTEGER PRIMARY KEY AUTOINCREMENT, delivery_id TEXT NOT NULL UNIQUE, tenant_id INTEGER NOT NULL,
		destination_id INTEGER NOT NULL, destination_name TEXT NOT NULL, alert_event_id INTEGER NOT NULL,
		incident_id INTEGER NOT NULL, event_type TEXT NOT NULL, request_url TEXT NOT NULL,
		secret_ciphertext TEXT, payload JSON NOT NULL, status TEXT NOT NULL, attempt_count INTEGER NOT NULL DEFAULT 0,
		retry_base_attempt_count INTEGER NOT NULL DEFAULT 0, manual_retry_count INTEGER NOT NULL DEFAULT 0,
		next_attempt_at DATETIME, claimed_by TEXT, lease_expires_at DATETIME, last_http_status INTEGER,
		last_error TEXT, delivered_at DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	taskID := "43"
	metadata := commonModels.JSONMap{"continuous": map[string]interface{}{"diagnostics": map[string]interface{}{"health": "critical", "checkpoint_health": "healthy"}}}
	if err := db.Exec(`INSERT INTO common.task_executions
		(tenant_id, execution_id, module, task_type, source, source_task_id, status, trigger_type, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, 7, "alert-exec-1", commonExecution.ModuleTransfer,
		commonExecution.TaskTypeSync, commonExecution.ModuleTransfer, taskID, commonExecution.ExecutionStatusRunning,
		commonExecution.TriggerTypeManual, metadata, now, now).Error; err != nil {
		t.Fatal(err)
	}
	encryptionKey := []byte("addp-dev-encryption-key-2025!!!!")
	secretCiphertext, err := commonUtils.Encrypt("0123456789abcdef", encryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&monitorModels.WebhookDestination{
		TenantID: 7, Name: "test", URL: "http://127.0.0.1:18080/hook", SecretCiphertext: secretCiphertext,
		Enabled: true, EventTypes: monitorModels.StringList{monitorModels.AlertEventOpened, monitorModels.AlertEventResolved},
	}).Error; err != nil {
		t.Fatal(err)
	}
	webhookService := NewWebhookService(db, encryptionKey, true, "http://localhost:5170", nil)
	alertService := NewAlertService(db, NewNotificationService(webhookService, nil))
	if err := alertService.Evaluate(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	var first monitorModels.AlertIncident
	if err := db.First(&first).Error; err != nil {
		t.Fatal(err)
	}
	if first.SignalCode != "retention_critical" || first.Status != monitorModels.AlertStatusOpen {
		t.Fatalf("first alert = %#v", first)
	}
	assertAlertNotificationCount(t, db, monitorModels.AlertEventOpened, 1)

	metadata = commonModels.JSONMap{"continuous": map[string]interface{}{"diagnostics": map[string]interface{}{"health": "healthy", "checkpoint_health": "healthy"}}}
	if err := db.Exec("UPDATE common.task_executions SET metadata = ? WHERE execution_id = ?", metadata, "alert-exec-1").Error; err != nil {
		t.Fatal(err)
	}
	if err := alertService.Evaluate(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&first, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.Status != monitorModels.AlertStatusResolved || first.ResolvedAt == nil {
		t.Fatalf("resolved alert = %#v", first)
	}
	assertAlertNotificationCount(t, db, monitorModels.AlertEventResolved, 1)

	metadata = commonModels.JSONMap{"continuous": map[string]interface{}{"diagnostics": map[string]interface{}{"health": "critical", "checkpoint_health": "healthy"}}}
	if err := db.Exec("UPDATE common.task_executions SET metadata = ? WHERE execution_id = ?", metadata, "alert-exec-1").Error; err != nil {
		t.Fatal(err)
	}
	if err := alertService.Evaluate(context.Background(), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&monitorModels.AlertIncident{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("alert incident count = %d, want 2", count)
	}
	assertAlertNotificationCount(t, db, monitorModels.AlertEventOpened, 2)

	if err := db.Create(&monitorModels.AlertRule{
		RuleID: "rule-generic-failed", TenantID: 7, Name: "bounded failed", Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, SourceTaskID: "99", RuleType: monitorModels.AlertRuleLastTerminalFailed,
		FailureThreshold: 1, Severity: monitorModels.AlertSeverityWarning, Enabled: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	insertAlertExecution(t, db, 7, "bounded-failed", "99", commonExecution.ExecutionStatusFailed, commonModels.JSONMap{}, nil, now.Add(3*time.Minute))
	if err := alertService.Evaluate(context.Background(), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var generic monitorModels.AlertIncident
	if err := db.Where("rule_id = ?", "rule-generic-failed").First(&generic).Error; err != nil {
		t.Fatal(err)
	}
	if generic.Status != monitorModels.AlertStatusOpen || generic.AlertRuleID == nil || generic.ExecutionID != "bounded-failed" {
		t.Fatalf("generic alert = %#v", generic)
	}
	var genericDeliveryCount int64
	if err := db.Model(&monitorModels.WebhookDelivery{}).Where("incident_id = ?", generic.ID).Count(&genericDeliveryCount).Error; err != nil {
		t.Fatal(err)
	}
	if genericDeliveryCount != 0 {
		t.Fatalf("unrouted rule created %d deliveries", genericDeliveryCount)
	}

	insertAlertExecution(t, db, 7, "bounded-pending", "99", commonExecution.ExecutionStatusPending, commonModels.JSONMap{}, nil, now.Add(4*time.Minute))
	insertAlertExecution(t, db, 7, "bounded-cancelled", "99", commonExecution.ExecutionStatusCancelled, commonModels.JSONMap{}, nil, now.Add(5*time.Minute))
	if err := alertService.Evaluate(context.Background(), now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&generic, generic.ID).Error; err != nil {
		t.Fatal(err)
	}
	if generic.Status != monitorModels.AlertStatusOpen || generic.ExecutionID != "bounded-failed" {
		t.Fatalf("non-terminal recovery changed alert = %#v", generic)
	}

	insertAlertExecution(t, db, 7, "bounded-success", "99", commonExecution.ExecutionStatusSuccess, commonModels.JSONMap{}, nil, now.Add(6*time.Minute))
	if err := alertService.Evaluate(context.Background(), now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&generic, generic.ID).Error; err != nil {
		t.Fatal(err)
	}
	if generic.Status != monitorModels.AlertStatusResolved {
		t.Fatalf("success did not resolve generic alert = %#v", generic)
	}
	insertAlertExecution(t, db, 7, "bounded-failed-again", "99", commonExecution.ExecutionStatusFailed, commonModels.JSONMap{}, nil, now.Add(6*time.Minute+10*time.Second))
	if err := alertService.Evaluate(context.Background(), now.Add(6*time.Minute+20*time.Second)); err != nil {
		t.Fatal(err)
	}
	var reopenedGeneric monitorModels.AlertIncident
	if err := db.Where("rule_id = ? AND status = ?", "rule-generic-failed", monitorModels.AlertStatusOpen).First(&reopenedGeneric).Error; err != nil {
		t.Fatal(err)
	}
	insertAlertExecution(t, db, 7, "continuous-running", "99", commonExecution.ExecutionStatusRunning,
		commonModels.JSONMap{"continuous": map[string]interface{}{}}, nil, now.Add(6*time.Minute+30*time.Second))
	if err := alertService.Evaluate(context.Background(), now.Add(6*time.Minute+40*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&reopenedGeneric, reopenedGeneric.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reopenedGeneric.Status != monitorModels.AlertStatusResolved {
		t.Fatalf("continuous mode switch kept bounded alert active = %#v", reopenedGeneric)
	}

	for _, excluded := range []struct {
		ruleID, taskID, executionID string
		metadata                    commonModels.JSONMap
		parent                      *string
	}{
		{ruleID: "rule-continuous", taskID: "100", executionID: "continuous-failed", metadata: commonModels.JSONMap{"continuous": map[string]interface{}{}}, parent: nil},
		{ruleID: "rule-child", taskID: "101", executionID: "child-failed", metadata: commonModels.JSONMap{}, parent: stringPointer("parent-execution")},
	} {
		if err := db.Create(&monitorModels.AlertRule{
			RuleID: excluded.ruleID, TenantID: 7, Name: excluded.ruleID, Module: commonExecution.ModuleTransfer,
			TaskType: commonExecution.TaskTypeSync, SourceTaskID: excluded.taskID, RuleType: monitorModels.AlertRuleLastTerminalFailed,
			FailureThreshold: 1, Severity: monitorModels.AlertSeverityWarning, Enabled: true,
		}).Error; err != nil {
			t.Fatal(err)
		}
		insertAlertExecution(t, db, 7, excluded.executionID, excluded.taskID, commonExecution.ExecutionStatusFailed, excluded.metadata, excluded.parent, now.Add(7*time.Minute))
	}
	if err := alertService.Evaluate(context.Background(), now.Add(7*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var excludedCount int64
	if err := db.Model(&monitorModels.AlertIncident{}).Where("rule_id IN ?", []string{"rule-continuous", "rule-child"}).Count(&excludedCount).Error; err != nil {
		t.Fatal(err)
	}
	if excludedCount != 0 {
		t.Fatalf("excluded executions created %d incidents", excludedCount)
	}

	schemaMetadata := commonModels.JSONMap{"continuous": map[string]interface{}{"schema_change": map[string]interface{}{
		"request_id": 17, "status": "pending", "from_revision": 1, "to_revision": 2,
		"source_partition": "0", "source_offset": 23, "unexpected_fields": []interface{}{"new_field"},
	}}}
	insertAlertExecution(t, db, 7, "schema-blocked", "102", commonExecution.ExecutionStatusFailed, schemaMetadata, nil, now.Add(8*time.Minute))
	if err := alertService.Evaluate(context.Background(), now.Add(8*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var schemaIncident monitorModels.AlertIncident
	if err := db.Where("signal_code = ? AND source_task_id = ?", "schema_change_blocked", "102").First(&schemaIncident).Error; err != nil {
		t.Fatal(err)
	}
	if schemaIncident.Status != monitorModels.AlertStatusOpen || schemaIncident.Severity != monitorModels.AlertSeverityCritical ||
		schemaIncident.ExecutionID != "schema-blocked" || schemaIncident.Details["request_id"] != float64(17) {
		t.Fatalf("schema change incident = %#v", schemaIncident)
	}
	schemaMetadata["continuous"].(map[string]interface{})["schema_change"].(map[string]interface{})["status"] = "applied"
	if err := db.Exec("UPDATE common.task_executions SET metadata = ? WHERE execution_id = ?", schemaMetadata, "schema-blocked").Error; err != nil {
		t.Fatal(err)
	}
	if err := alertService.Evaluate(context.Background(), now.Add(9*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&schemaIncident, schemaIncident.ID).Error; err != nil {
		t.Fatal(err)
	}
	if schemaIncident.Status != monitorModels.AlertStatusResolved || schemaIncident.ResolvedAt == nil {
		t.Fatalf("approved schema change did not resolve incident = %#v", schemaIncident)
	}
}

func insertAlertExecution(t *testing.T, db *gorm.DB, tenantID int, executionID, taskID, status string, metadata commonModels.JSONMap, parent *string, now time.Time) {
	t.Helper()
	if err := db.Exec(`INSERT INTO common.task_executions
		(tenant_id, execution_id, module, task_type, source, source_task_id, parent_execution_id, status, trigger_type, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, tenantID, executionID, commonExecution.ModuleTransfer,
		commonExecution.TaskTypeSync, commonExecution.ModuleTransfer, taskID, parent, status,
		commonExecution.TriggerTypeManual, metadata, now, now).Error; err != nil {
		t.Fatal(err)
	}
}

func stringPointer(value string) *string { return &value }

func assertAlertNotificationCount(t *testing.T, db *gorm.DB, eventType string, expected int64) {
	t.Helper()
	var eventCount int64
	if err := db.Model(&monitorModels.AlertEvent{}).Where("event_type = ?", eventType).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != expected {
		t.Fatalf("%s event count = %d, want %d", eventType, eventCount, expected)
	}
	var deliveryCount int64
	if err := db.Model(&monitorModels.WebhookDelivery{}).Where("event_type = ?", eventType).Count(&deliveryCount).Error; err != nil {
		t.Fatal(err)
	}
	if deliveryCount != expected {
		t.Fatalf("%s delivery count = %d, want %d", eventType, deliveryCount, expected)
	}
}

func TestDeriveObservationSignalsUsesOwnerHealthFacts(t *testing.T) {
	taskID := "43"
	execution := commonExecution.TaskExecution{Status: commonExecution.ExecutionStatusPending, SourceTaskID: &taskID, Metadata: commonModels.JSONMap{
		"recovery_circuit_state": "open",
		"continuous":             map[string]interface{}{"diagnostics": map[string]interface{}{"health": "degraded", "checkpoint_health": "degraded"}},
	}}
	signals := deriveObservationSignals(execution, time.Now())
	if len(signals) != 3 || signals[0].Code != "recovery_circuit_open" || signals[0].Severity != monitorModels.AlertSeverityCritical {
		t.Fatalf("signals = %#v", signals)
	}
}

func TestDeriveObservationSignalsUsesPendingSchemaChangeProjection(t *testing.T) {
	taskID := "43"
	execution := commonExecution.TaskExecution{Status: commonExecution.ExecutionStatusFailed, SourceTaskID: &taskID, Metadata: commonModels.JSONMap{
		"continuous": map[string]interface{}{"schema_change": map[string]interface{}{
			"request_id": float64(9), "status": "pending", "unexpected_fields": []interface{}{"new_field"},
		}},
	}}
	signals := deriveObservationSignals(execution, time.Now())
	if len(signals) != 1 || signals[0].Code != "schema_change_blocked" ||
		signals[0].Severity != monitorModels.AlertSeverityCritical || signals[0].Details["request_id"] != float64(9) {
		t.Fatalf("signals = %#v", signals)
	}
	execution.Metadata["continuous"].(map[string]interface{})["schema_change"].(map[string]interface{})["status"] = "stopped"
	if signals := deriveObservationSignals(execution, time.Now()); len(signals) != 0 {
		t.Fatalf("stopped schema change signals = %#v", signals)
	}
}

func TestDeriveRuleSignalRequiresConfiguredConsecutiveTerminalFailures(t *testing.T) {
	rule := monitorModels.AlertRule{
		ID: 9, RuleID: "rule-consecutive", TenantID: 3, Name: "consecutive", Module: "develop",
		TaskType: commonExecution.TaskTypeWorkflow, SourceTaskID: "12", RuleType: monitorModels.AlertRuleConsecutiveFailures,
		FailureThreshold: 3, Severity: monitorModels.AlertSeverityCritical,
	}
	executions := []commonExecution.TaskExecution{
		{ExecutionID: "latest", Status: commonExecution.ExecutionStatusFailed},
		{ExecutionID: "middle", Status: commonExecution.ExecutionStatusTimeout},
		{ExecutionID: "oldest", Status: commonExecution.ExecutionStatusFailed},
	}
	signal, active := deriveRuleSignal(rule, executions)
	if !active || signal.ExecutionID != "latest" || signal.Details["failure_count"] != 3 {
		t.Fatalf("signal = %#v, active=%v", signal, active)
	}
	executions[2].Status = commonExecution.ExecutionStatusSuccess
	if _, active := deriveRuleSignal(rule, executions); active {
		t.Fatal("success did not interrupt consecutive failures")
	}
	if _, active := deriveRuleSignal(rule, executions[:2]); active {
		t.Fatal("rule activated below its threshold")
	}
}
