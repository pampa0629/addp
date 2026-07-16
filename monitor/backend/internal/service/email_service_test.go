package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	mail "github.com/wneessen/go-mail"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type recordingEmailSender struct {
	deliveries []monitorModels.EmailDelivery
	err        error
}

func (s *recordingEmailSender) Send(_ context.Context, delivery monitorModels.EmailDelivery, _ time.Time) error {
	s.deliveries = append(s.deliveries, delivery)
	return s.err
}

func TestEmailDestinationLifecycleAndNotificationOutbox(t *testing.T) {
	db := newEmailServiceTestDB(t)
	sender := &recordingEmailSender{}
	emailService := NewEmailService(db, "http://localhost:5170", sender)
	destination, err := emailService.CreateDestination(context.Background(), CreateEmailDestinationInput{
		TenantID: 7, Name: " on-call ", Recipients: []string{"ONCALL@example.com", "oncall@example.com"},
		Enabled: true, EventTypes: []string{monitorModels.AlertEventResolved, monitorModels.AlertEventOpened, monitorModels.AlertEventOpened},
	})
	if err != nil {
		t.Fatal(err)
	}
	if destination.Name != "on-call" || len(destination.Recipients) != 1 || len(destination.EventTypes) != 2 ||
		destination.EventTypes[0] != monitorModels.AlertEventOpened {
		t.Fatalf("destination = %#v", destination)
	}
	if _, err := emailService.CreateDestination(context.Background(), CreateEmailDestinationInput{
		TenantID: 7, Name: "on-call", Recipients: []string{"other@example.com"}, Enabled: true,
		EventTypes: []string{monitorModels.AlertEventOpened},
	}); !errors.Is(err, ErrEmailDestinationConflict) {
		t.Fatalf("duplicate destination error = %v", err)
	}
	if _, err := emailService.CreateDestination(context.Background(), CreateEmailDestinationInput{
		TenantID: 7, Name: "invalid", Recipients: []string{"Display <display@example.com>"}, Enabled: true,
		EventTypes: []string{monitorModels.AlertEventOpened},
	}); !errors.Is(err, ErrEmailDestinationInvalid) {
		t.Fatalf("display-name recipient error = %v", err)
	}

	now := time.Date(2026, 7, 16, 9, 30, 0, 0, time.UTC)
	testResult, err := emailService.TestDestination(context.Background(), 7, destination.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if testResult.DeliveryID == "" || testResult.Recipients != 1 || len(sender.deliveries) != 1 {
		t.Fatalf("test result = %#v, sends=%d", testResult, len(sender.deliveries))
	}
	var deliveryCount int64
	if err := db.Model(&monitorModels.EmailDelivery{}).Count(&deliveryCount).Error; err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 0 {
		t.Fatalf("test send persisted %d outbox rows", deliveryCount)
	}

	incident := monitorModels.AlertIncident{
		ID: 81, TenantID: 7, Module: "transfer", TaskType: "sync", SourceTaskID: "43",
		ExecutionID: "execution-81", SignalCode: "checkpoint_stalled", Severity: monitorModels.AlertSeverityWarning,
		Status: monitorModels.AlertStatusOpen, OpenedAt: now,
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return NewNotificationService(nil, emailService).
			RecordAlertEventTx(tx, incident, monitorModels.AlertEventOpened, "", now)
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
	var delivery monitorModels.EmailDelivery
	if err := db.Where("incident_id = ?", incident.ID).First(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.Status != monitorModels.EmailDeliveryPending || delivery.NextAttemptAt == nil ||
		!strings.Contains(delivery.Subject, "checkpoint_stalled") || strings.Contains(delivery.TextBody, "metadata") {
		t.Fatalf("email delivery = %#v", delivery)
	}

	enabled := false
	if _, err := emailService.UpdateDestination(context.Background(), UpdateEmailDestinationInput{
		TenantID: 7, ID: destination.ID, Enabled: &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	var cancelled monitorModels.EmailDelivery
	if err := db.First(&cancelled, delivery.ID).Error; err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != monitorModels.EmailDeliveryCancelled || cancelled.NextAttemptAt != nil {
		t.Fatalf("cancelled delivery = %#v", cancelled)
	}
}

func TestEmailSuppressionRetryAndDeletePreserveHistory(t *testing.T) {
	db := newEmailServiceTestDB(t)
	emailService := NewEmailService(db, "http://localhost:5170", &recordingEmailSender{})
	destination, err := emailService.CreateDestination(context.Background(), CreateEmailDestinationInput{
		TenantID: 17, Name: "operations", Recipients: []string{"ops@example.com"}, Enabled: true,
		EventTypes: []string{monitorModels.AlertEventResolved},
	})
	if err != nil {
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
		return NewNotificationService(nil, emailService).
			RecordAlertEventTx(tx, incident, monitorModels.AlertEventResolved, incident.Severity, now)
	}); err != nil {
		t.Fatal(err)
	}
	var suppressed monitorModels.EmailDelivery
	if err := db.Where("incident_id = ?", incident.ID).First(&suppressed).Error; err != nil {
		t.Fatal(err)
	}
	if suppressed.Status != monitorModels.EmailDeliverySuppressed || suppressed.NextAttemptAt != nil {
		t.Fatalf("suppressed delivery = %#v", suppressed)
	}

	deadID := uuid.NewString()
	dead := monitorModels.EmailDelivery{
		DeliveryID: deadID, TenantID: 17, DestinationID: destination.ID, DestinationName: destination.Name,
		AlertEventID: 72, IncidentID: 82, EventType: monitorModels.AlertEventResolved,
		Recipients: monitorModels.StringList{"old@example.com"}, Subject: "frozen subject",
		TextBody: "frozen text", HTMLBody: "<p>frozen html</p>", Status: monitorModels.EmailDeliveryDead, AttemptCount: 8,
	}
	if err := db.Create(&dead).Error; err != nil {
		t.Fatal(err)
	}
	newRecipients := []string{"new@example.com"}
	if _, err := emailService.UpdateDestination(context.Background(), UpdateEmailDestinationInput{
		TenantID: 17, ID: destination.ID, Recipients: &newRecipients,
	}); err != nil {
		t.Fatal(err)
	}
	requeued, err := emailService.RetryDelivery(context.Background(), 17, deadID, now)
	if err != nil {
		t.Fatal(err)
	}
	if requeued.DeliveryID != deadID || requeued.Subject != "frozen subject" || requeued.TextBody != "frozen text" ||
		len(requeued.Recipients) != 1 || requeued.Recipients[0] != "new@example.com" ||
		requeued.RetryBaseAttemptCount != 8 || requeued.ManualRetryCount != 1 {
		t.Fatalf("requeued delivery = %#v", requeued)
	}
	if err := emailService.DeleteDestination(context.Background(), 17, destination.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var cancelled monitorModels.EmailDelivery
	if err := db.First(&cancelled, requeued.ID).Error; err != nil {
		t.Fatalf("historical delivery missing after delete: %v", err)
	}
	if cancelled.Status != monitorModels.EmailDeliveryCancelled {
		t.Fatalf("pending retry was not cancelled: %#v", cancelled)
	}
	if _, err := emailService.RetryDelivery(context.Background(), 17, deadID, now); !errors.Is(err, ErrEmailDeliveryNotRetryable) {
		t.Fatalf("retry after delete error = %v", err)
	}
}

type recordingSMTPClient struct{ message *mail.Msg }

func (c *recordingSMTPClient) DialAndSendWithContext(_ context.Context, messages ...*mail.Msg) error {
	if len(messages) == 1 {
		c.message = messages[0]
	}
	return nil
}

func TestSMTPEmailSenderBuildsStableMessageIdentity(t *testing.T) {
	client := &recordingSMTPClient{}
	sender := &SMTPEmailSender{client: client, fromAddress: "monitor@example.com", fromName: "ADDP Monitor"}
	deliveryID := uuid.NewString()
	err := sender.Send(context.Background(), monitorModels.EmailDelivery{
		DeliveryID: deliveryID, Recipients: monitorModels.StringList{"ops@example.com"}, Subject: "subject",
		TextBody: "plain", HTMLBody: "<p>html</p>",
	}, time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if client.message == nil || client.message.GetMessageID() != "<"+deliveryID+"@monitor.addp.local>" {
		t.Fatalf("message ID = %q", client.message.GetMessageID())
	}
	var encoded bytes.Buffer
	if _, err := client.message.WriteTo(&encoded); err != nil {
		t.Fatal(err)
	}
	message := encoded.String()
	for _, expected := range []string{"monitor@example.com", "ops@example.com", "subject", "plain", "html"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message does not contain %q: %s", expected, message)
		}
	}
}

func TestEmailRetryBackoffIsBounded(t *testing.T) {
	dispatcher := NewEmailDispatcher(nil, nil, EmailDispatcherConfig{RetryInitial: 5 * time.Second, RetryMax: time.Minute})
	if got := dispatcher.retryBackoff(1); got != 5*time.Second {
		t.Fatalf("attempt 1 backoff = %s", got)
	}
	if got := dispatcher.retryBackoff(10); got != time.Minute {
		t.Fatalf("bounded backoff = %s", got)
	}
}

func newEmailServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:monitor-email-service-%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "-"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS monitor").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE monitor.notification_routes (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, alert_rule_id INTEGER NOT NULL,
			channel TEXT NOT NULL, destination_id INTEGER NOT NULL, created_at DATETIME NOT NULL)`,
		`CREATE TABLE monitor.email_destinations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
			recipients JSON NOT NULL, enabled BOOLEAN NOT NULL, event_types JSON NOT NULL,
			created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, UNIQUE (tenant_id, name))`,
		`CREATE TABLE monitor.alert_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT, event_id TEXT NOT NULL UNIQUE, tenant_id INTEGER NOT NULL,
			incident_id INTEGER NOT NULL, event_type TEXT NOT NULL, from_severity TEXT, to_severity TEXT NOT NULL,
			occurred_at DATETIME NOT NULL, created_at DATETIME NOT NULL)`,
		`CREATE TABLE monitor.email_deliveries (
			id INTEGER PRIMARY KEY AUTOINCREMENT, delivery_id TEXT NOT NULL UNIQUE, tenant_id INTEGER NOT NULL,
			destination_id INTEGER NOT NULL, destination_name TEXT NOT NULL, alert_event_id INTEGER NOT NULL,
			incident_id INTEGER NOT NULL, event_type TEXT NOT NULL, recipients JSON NOT NULL,
			subject TEXT NOT NULL, text_body TEXT NOT NULL, html_body TEXT NOT NULL, status TEXT NOT NULL,
			attempt_count INTEGER NOT NULL DEFAULT 0, retry_base_attempt_count INTEGER NOT NULL DEFAULT 0,
			manual_retry_count INTEGER NOT NULL DEFAULT 0, next_attempt_at DATETIME, claimed_by TEXT,
			lease_expires_at DATETIME, last_error TEXT, delivered_at DATETIME,
			created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
			UNIQUE (alert_event_id, destination_id))`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
