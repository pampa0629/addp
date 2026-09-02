package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	secretcipher "github.com/addp/common/secretcipher"
	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestHTTPWebhookSenderSignsStablePayload(t *testing.T) {
	secret := "0123456789abcdef"
	var receivedBody []byte
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		receivedHeaders = request.Header.Clone()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	now := time.Date(2026, 7, 15, 12, 30, 0, 0, time.UTC)
	delivery := monitorModels.WebhookDelivery{
		DeliveryID: "delivery-1", RequestURL: server.URL,
		Payload: commonModels.JSONMap{"schema_version": "monitor.alert.webhook/v1", "delivery_id": "delivery-1"},
	}
	result, err := NewHTTPWebhookSender(time.Second, true).Send(context.Background(), delivery, secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.HTTPStatus != http.StatusNoContent {
		t.Fatalf("HTTP status = %d", result.HTTPStatus)
	}
	if receivedHeaders.Get("X-ADDP-Webhook-ID") != delivery.DeliveryID {
		t.Fatalf("delivery header = %q", receivedHeaders.Get("X-ADDP-Webhook-ID"))
	}
	timestamp := receivedHeaders.Get("X-ADDP-Webhook-Timestamp")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(receivedBody)
	expectedSignature := "v1=" + hex.EncodeToString(mac.Sum(nil))
	if receivedHeaders.Get("X-ADDP-Webhook-Signature") != expectedSignature {
		t.Fatalf("signature = %q, want %q", receivedHeaders.Get("X-ADDP-Webhook-Signature"), expectedSignature)
	}
}

func TestValidateWebhookURLRejectsPrivateTargetsByDefault(t *testing.T) {
	for _, target := range []string{
		"http://example.com/hook",
		"https://localhost/hook",
		"https://127.0.0.1/hook",
		"https://169.254.169.254/latest/meta-data",
	} {
		if err := ValidateWebhookURL(context.Background(), target, false); err == nil {
			t.Fatalf("target %q was accepted", target)
		}
	}
	if err := ValidateWebhookURL(context.Background(), "http://127.0.0.1:18080/hook", true); err != nil {
		t.Fatalf("private development target rejected: %v", err)
	}
}

func TestWebhookDestinationEncryptsSecretAndDisablingCancelsPending(t *testing.T) {
	db := newWebhookServiceTestDB(t)
	key := []byte("addp-dev-encryption-key-2025!!!!")
	webhookService := NewWebhookService(db, key, true, "http://localhost:5170", NewHTTPWebhookSender(time.Second, true))
	destination, err := webhookService.CreateDestination(context.Background(), CreateWebhookDestinationInput{
		TenantID: 7, Name: " ops ", URL: "http://127.0.0.1:18080/hook", Secret: "0123456789abcdef",
		Enabled: true, EventTypes: []string{monitorModels.AlertEventResolved, monitorModels.AlertEventOpened, monitorModels.AlertEventOpened},
	})
	if err != nil {
		t.Fatal(err)
	}
	if destination.Name != "ops" || !destination.SecretConfigured {
		t.Fatalf("destination = %#v", destination)
	}
	if len(destination.EventTypes) != 2 || destination.EventTypes[0] != monitorModels.AlertEventOpened {
		t.Fatalf("event types = %#v", destination.EventTypes)
	}
	plaintext, err := secretcipher.Decrypt(destination.SecretCiphertext, key)
	if err != nil || plaintext != "0123456789abcdef" {
		t.Fatalf("decrypted secret = %q, err = %v", plaintext, err)
	}
	encoded, err := json.Marshal(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || jsonContainsKey(encoded, "secret_ciphertext") {
		t.Fatalf("destination JSON leaks secret: %s", encoded)
	}
	if _, err := webhookService.CreateDestination(context.Background(), CreateWebhookDestinationInput{
		TenantID: 7, Name: "ops", URL: "http://127.0.0.1:18080/duplicate", Secret: "0123456789abcdef",
		Enabled: true, EventTypes: []string{monitorModels.AlertEventOpened},
	}); !errors.Is(err, ErrWebhookDestinationConflict) {
		t.Fatalf("duplicate destination error = %v", err)
	}

	nextAttempt := time.Now()
	delivery := monitorModels.WebhookDelivery{
		DeliveryID: "delivery-disable", TenantID: 7, DestinationID: destination.ID, DestinationName: destination.Name,
		AlertEventID: 1, IncidentID: 1, EventType: monitorModels.AlertEventOpened, RequestURL: destination.URL,
		SecretCiphertext: destination.SecretCiphertext, Payload: commonModels.JSONMap{"delivery_id": "delivery-disable"},
		Status: monitorModels.WebhookDeliveryPending, NextAttemptAt: &nextAttempt,
	}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&monitorModels.WebhookDestination{}).Where("id = ?", destination.ID).
		Update("url", "https://does-not-resolve.invalid/hook").Error; err != nil {
		t.Fatal(err)
	}
	enabled := false
	updated, err := webhookService.UpdateDestination(context.Background(), UpdateWebhookDestinationInput{
		TenantID: 7, ID: destination.ID, Enabled: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("destination remains enabled")
	}
	var cancelled monitorModels.WebhookDelivery
	if err := db.First(&cancelled, delivery.ID).Error; err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != monitorModels.WebhookDeliveryCancelled || cancelled.SecretCiphertext != "" || cancelled.NextAttemptAt != nil {
		t.Fatalf("cancelled delivery = %#v", cancelled)
	}
	otherTenant, err := webhookService.CreateDestination(context.Background(), CreateWebhookDestinationInput{
		TenantID: 8, Name: "other-tenant", URL: "http://127.0.0.1:18080/other", Secret: "0123456789abcdef",
		Enabled: false, EventTypes: []string{monitorModels.AlertEventOpened},
	})
	if err != nil {
		t.Fatal(err)
	}
	if otherTenant.Enabled {
		t.Fatal("explicitly disabled destination was created as enabled")
	}
	tenantDestinations, err := webhookService.ListDestinations(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(tenantDestinations) != 1 || tenantDestinations[0].TenantID != 7 {
		t.Fatalf("tenant destinations = %#v", tenantDestinations)
	}
}

func TestRecordAlertEventKeepsEventButSuppressesDelivery(t *testing.T) {
	db := newWebhookServiceTestDB(t)
	key := []byte("addp-dev-encryption-key-2025!!!!")
	webhookService := NewWebhookService(db, key, true, "http://localhost:5170", NewHTTPWebhookSender(time.Second, true))
	if _, err := webhookService.CreateDestination(context.Background(), CreateWebhookDestinationInput{
		TenantID: 17, Name: "suppressed", URL: "http://127.0.0.1:18080/suppressed", Secret: "0123456789abcdef",
		Enabled: true, EventTypes: []string{monitorModels.AlertEventResolved},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	suppressedUntil := now.Add(time.Hour)
	resolvedAt := now
	incident := monitorModels.AlertIncident{
		ID: 91, TenantID: 17, Module: "transfer", TaskType: "sync", SourceTaskID: "43",
		ExecutionID: "execution-91", SignalCode: "retention_critical", Severity: monitorModels.AlertSeverityCritical,
		Status: monitorModels.AlertStatusResolved, OpenedAt: now.Add(-time.Hour), ResolvedAt: &resolvedAt,
		SuppressedUntil: &suppressedUntil,
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return NewNotificationService(webhookService, nil).
			RecordAlertEventTx(tx, incident, monitorModels.AlertEventResolved, incident.Severity, now)
	}); err != nil {
		t.Fatal(err)
	}
	var eventCount int64
	if err := db.Model(&monitorModels.AlertEvent{}).Where("incident_id = ?", incident.ID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("event count = %d", eventCount)
	}
	var delivery monitorModels.WebhookDelivery
	if err := db.Where("incident_id = ?", incident.ID).First(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.Status != monitorModels.WebhookDeliverySuppressed || delivery.SecretCiphertext != "" || delivery.NextAttemptAt != nil {
		t.Fatalf("suppressed delivery = %#v", delivery)
	}
}

func TestNotificationServiceCreatesOneEventForWebhookAndEmail(t *testing.T) {
	db := newWebhookServiceTestDB(t)
	key := []byte("addp-dev-encryption-key-2025!!!!")
	webhookService := NewWebhookService(db, key, true, "http://localhost:5170", nil)
	emailService := NewEmailService(db, "http://localhost:5170", nil)
	if _, err := webhookService.CreateDestination(context.Background(), CreateWebhookDestinationInput{
		TenantID: 27, Name: "webhook", URL: "http://127.0.0.1:18080/hook", Secret: "0123456789abcdef",
		Enabled: true, EventTypes: []string{monitorModels.AlertEventOpened},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := emailService.CreateDestination(context.Background(), CreateEmailDestinationInput{
		TenantID: 27, Name: "email", Recipients: []string{"ops@example.com"}, Enabled: true,
		EventTypes: []string{monitorModels.AlertEventOpened},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	incident := monitorModels.AlertIncident{
		ID: 101, TenantID: 27, Module: "transfer", TaskType: "sync", SourceTaskID: "43",
		ExecutionID: "execution-101", SignalCode: "retention_critical", Severity: monitorModels.AlertSeverityCritical,
		Status: monitorModels.AlertStatusOpen, OpenedAt: now,
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return NewNotificationService(webhookService, emailService).
			RecordAlertEventTx(tx, incident, monitorModels.AlertEventOpened, "", now)
	}); err != nil {
		t.Fatal(err)
	}
	var events []monitorModels.AlertEvent
	if err := db.Where("incident_id = ?", incident.ID).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	var webhookDelivery monitorModels.WebhookDelivery
	if err := db.Where("incident_id = ?", incident.ID).First(&webhookDelivery).Error; err != nil {
		t.Fatal(err)
	}
	var emailDelivery monitorModels.EmailDelivery
	if err := db.Where("incident_id = ?", incident.ID).First(&emailDelivery).Error; err != nil {
		t.Fatal(err)
	}
	if webhookDelivery.AlertEventID != events[0].ID || emailDelivery.AlertEventID != events[0].ID {
		t.Fatalf("outboxes reference different events: webhook=%d email=%d event=%d",
			webhookDelivery.AlertEventID, emailDelivery.AlertEventID, events[0].ID)
	}
}

func TestWebhookRetryBackoffIsBounded(t *testing.T) {
	dispatcher := NewWebhookDispatcher(nil, nil, WebhookDispatcherConfig{
		RetryInitial: 5 * time.Second,
		RetryMax:     time.Minute,
	})
	if got := dispatcher.retryBackoff(1); got != 5*time.Second {
		t.Fatalf("attempt 1 backoff = %s", got)
	}
	if got := dispatcher.retryBackoff(10); got != time.Minute {
		t.Fatalf("bounded backoff = %s", got)
	}
}

func TestWebhookOperationalActionsPreserveDeliveryIdentityAndHistory(t *testing.T) {
	db := newWebhookServiceTestDB(t)
	key := []byte("addp-dev-encryption-key-2025!!!!")
	var testPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/fail" {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&testPayload); err != nil {
			t.Errorf("decode test payload: %v", err)
		}
		writer.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	webhookService := NewWebhookService(db, key, true, "http://localhost:5170", NewHTTPWebhookSender(time.Second, true))
	destination, err := webhookService.CreateDestination(context.Background(), CreateWebhookDestinationInput{
		TenantID: 23, Name: "operations", URL: server.URL, Secret: "0123456789abcdef",
		Enabled: true, EventTypes: []string{monitorModels.AlertEventOpened},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	testResult, err := webhookService.TestDestination(context.Background(), 23, destination.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if testResult.HTTPStatus != http.StatusAccepted || testResult.DeliveryID == "" {
		t.Fatalf("test result = %#v", testResult)
	}
	if testPayload["schema_version"] != "monitor.webhook.test/v1" || testPayload["delivery_id"] != testResult.DeliveryID {
		t.Fatalf("test payload = %#v", testPayload)
	}
	var deliveryCount int64
	if err := db.Model(&monitorModels.WebhookDelivery{}).Count(&deliveryCount).Error; err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 0 {
		t.Fatalf("test delivery persisted %d outbox rows", deliveryCount)
	}
	if err := db.Model(&monitorModels.WebhookDestination{}).Where("id = ?", destination.ID).
		Update("url", server.URL+"/fail").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := webhookService.TestDestination(context.Background(), 23, destination.ID, now); !errors.Is(err, ErrWebhookTestFailed) {
		t.Fatalf("failed test delivery error = %v", err)
	}
	deadID := uuid.NewString()
	dead := monitorModels.WebhookDelivery{
		DeliveryID: deadID, TenantID: 23, DestinationID: destination.ID, DestinationName: destination.Name,
		AlertEventID: 71, IncidentID: 81, EventType: monitorModels.AlertEventOpened, RequestURL: server.URL,
		Payload: commonModels.JSONMap{"schema_version": "monitor.alert.webhook/v1", "delivery_id": deadID},
		Status:  monitorModels.WebhookDeliveryDead, AttemptCount: 3,
	}
	if err := db.Create(&dead).Error; err != nil {
		t.Fatal(err)
	}
	requeued, err := webhookService.RetryDelivery(context.Background(), 23, deadID, now)
	if err != nil {
		t.Fatal(err)
	}
	if requeued.DeliveryID != deadID || requeued.Status != monitorModels.WebhookDeliveryPending ||
		requeued.AttemptCount != 3 || requeued.RetryBaseAttemptCount != 3 || requeued.ManualRetryCount != 1 ||
		requeued.RequestURL != server.URL+"/fail" || requeued.SecretCiphertext == "" {
		t.Fatalf("requeued delivery = %#v", requeued)
	}
	historicalID := uuid.NewString()
	historical := monitorModels.WebhookDelivery{
		DeliveryID: historicalID, TenantID: 23, DestinationID: destination.ID, DestinationName: destination.Name,
		AlertEventID: 72, IncidentID: 82, EventType: monitorModels.AlertEventOpened, RequestURL: server.URL,
		Payload: commonModels.JSONMap{"schema_version": "monitor.alert.webhook/v1", "delivery_id": historicalID},
		Status:  monitorModels.WebhookDeliveryDead, AttemptCount: 8,
	}
	if err := db.Create(&historical).Error; err != nil {
		t.Fatal(err)
	}
	nextAttempt := now
	pendingID := uuid.NewString()
	pending := monitorModels.WebhookDelivery{
		DeliveryID: pendingID, TenantID: 23, DestinationID: destination.ID, DestinationName: destination.Name,
		AlertEventID: 73, IncidentID: 83, EventType: monitorModels.AlertEventOpened, RequestURL: server.URL,
		SecretCiphertext: destination.SecretCiphertext,
		Payload:          commonModels.JSONMap{"schema_version": "monitor.alert.webhook/v1", "delivery_id": pendingID},
		Status:           monitorModels.WebhookDeliveryPending, NextAttemptAt: &nextAttempt,
	}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := webhookService.DeleteDestination(context.Background(), 23, destination.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	var cancelled monitorModels.WebhookDelivery
	if err := db.First(&cancelled, pending.ID).Error; err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != monitorModels.WebhookDeliveryCancelled || cancelled.SecretCiphertext != "" || cancelled.NextAttemptAt != nil {
		t.Fatalf("pending delivery after delete = %#v", cancelled)
	}
	if err := db.First(&historical, historical.ID).Error; err != nil {
		t.Fatalf("historical dead delivery was deleted: %v", err)
	}
	var destinationCount int64
	if err := db.Model(&monitorModels.WebhookDestination{}).Where("id = ?", destination.ID).Count(&destinationCount).Error; err != nil {
		t.Fatal(err)
	}
	if destinationCount != 0 {
		t.Fatalf("destination count = %d", destinationCount)
	}
	if _, err := webhookService.RetryDelivery(context.Background(), 23, historicalID, now); !errors.Is(err, ErrWebhookDeliveryNotRetryable) {
		t.Fatalf("retry after destination delete error = %v", err)
	}
}

func newWebhookServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:monitor-webhook-service?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS monitor").Error; err != nil {
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
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
		UNIQUE (tenant_id, name)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE monitor.alert_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT, event_id TEXT NOT NULL UNIQUE, tenant_id INTEGER NOT NULL,
		incident_id INTEGER NOT NULL, event_type TEXT NOT NULL, from_severity TEXT, to_severity TEXT NOT NULL,
		occurred_at DATETIME NOT NULL, created_at DATETIME NOT NULL
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
	if err := db.Exec(`CREATE TABLE monitor.email_destinations (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		recipients JSON NOT NULL, enabled BOOLEAN NOT NULL, event_types JSON NOT NULL,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
		UNIQUE (tenant_id, name)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE monitor.email_deliveries (
		id INTEGER PRIMARY KEY AUTOINCREMENT, delivery_id TEXT NOT NULL UNIQUE, tenant_id INTEGER NOT NULL,
		destination_id INTEGER NOT NULL, destination_name TEXT NOT NULL, alert_event_id INTEGER NOT NULL,
		incident_id INTEGER NOT NULL, event_type TEXT NOT NULL, recipients JSON NOT NULL,
		subject TEXT NOT NULL, text_body TEXT NOT NULL, html_body TEXT NOT NULL, status TEXT NOT NULL,
		attempt_count INTEGER NOT NULL DEFAULT 0, retry_base_attempt_count INTEGER NOT NULL DEFAULT 0,
		manual_retry_count INTEGER NOT NULL DEFAULT 0, next_attempt_at DATETIME, claimed_by TEXT,
		lease_expires_at DATETIME, last_error TEXT, delivered_at DATETIME,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
		UNIQUE (alert_event_id, destination_id)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func jsonContainsKey(encoded []byte, key string) bool {
	var value map[string]interface{}
	if json.Unmarshal(encoded, &value) != nil {
		return false
	}
	_, exists := value[key]
	return exists
}
