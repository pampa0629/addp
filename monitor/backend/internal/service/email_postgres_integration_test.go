package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresEmailOutboxClaimAndDispatch(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := EnsureMonitorStore(db); err != nil {
		t.Fatalf("ensure monitor store: %v", err)
	}
	var indexCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'monitor' AND indexname IN (?, ?)`,
		"idx_monitor_email_delivery_pending", "idx_monitor_email_delivery_expired_lease").
		Scan(&indexCount).Error; err != nil {
		t.Fatalf("query email delivery indexes: %v", err)
	}
	if indexCount != 2 {
		t.Fatalf("email delivery index count = %d, want 2", indexCount)
	}

	tenantID := int(time.Now().UnixNano()%100000000) + 700000000
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.EmailDelivery{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.WebhookDelivery{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.AlertEvent{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.EmailDestination{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.WebhookDestination{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.AlertIncident{}).Error
	})

	sender := &recordingEmailSender{}
	emailService := NewEmailService(db, "http://localhost:5170", sender)
	destination, err := emailService.CreateDestination(context.Background(), CreateEmailDestinationInput{
		TenantID: tenantID, Name: "postgres-email", Recipients: []string{"ops@example.com"}, Enabled: true,
		EventTypes: []string{monitorModels.AlertEventOpened},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emailService.CreateDestination(context.Background(), CreateEmailDestinationInput{
		TenantID: tenantID, Name: "postgres-email", Recipients: []string{"other@example.com"}, Enabled: true,
		EventTypes: []string{monitorModels.AlertEventOpened},
	}); !errors.Is(err, ErrEmailDestinationConflict) {
		t.Fatalf("duplicate destination error = %v", err)
	}
	testResult, err := emailService.TestDestination(context.Background(), tenantID, destination.ID, time.Now().UTC())
	if err != nil || testResult.Recipients != 1 {
		t.Fatalf("test destination result = %#v, err=%v", testResult, err)
	}
	var deliveryCount int64
	if err := db.Model(&monitorModels.EmailDelivery{}).Where("tenant_id = ?", tenantID).Count(&deliveryCount).Error; err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 0 {
		t.Fatalf("test email persisted %d outbox rows", deliveryCount)
	}

	now := time.Now().UTC()
	incident := monitorModels.AlertIncident{
		ID: uint(now.UnixNano()), TenantID: tenantID, Module: "transfer", TaskType: "sync", SourceTaskID: "43",
		ExecutionID: "email-postgres-execution", SignalCode: "retention_critical",
		Severity: monitorModels.AlertSeverityCritical, Status: monitorModels.AlertStatusOpen, OpenedAt: now,
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return NewNotificationService(nil, emailService).
			RecordAlertEventTx(tx, incident, monitorModels.AlertEventOpened, "", now)
	}); err != nil {
		t.Fatalf("create event and outbox: %v", err)
	}
	var eventCount int64
	if err := db.Model(&monitorModels.AlertEvent{}).Where("tenant_id = ?", tenantID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("event count = %d, want 1", eventCount)
	}
	dispatcher := NewEmailDispatcher(db, sender, EmailDispatcherConfig{
		WorkerID: "email-integration", LeaseDuration: 5 * time.Second, MaxAttempts: 1,
		RetryInitial: time.Second, RetryMax: time.Minute,
	})
	processed, err := dispatcher.DispatchOnce(context.Background(), now.Add(time.Second))
	if err != nil || !processed {
		t.Fatalf("dispatch success: processed=%v err=%v", processed, err)
	}
	var delivered monitorModels.EmailDelivery
	if err := db.Where("tenant_id = ?", tenantID).First(&delivered).Error; err != nil {
		t.Fatal(err)
	}
	if delivered.Status != monitorModels.EmailDeliveryDelivered || delivered.AttemptCount != 1 || delivered.DeliveredAt == nil {
		t.Fatalf("delivered email = %#v", delivered)
	}

	sender.err = errors.New("relay rejected message")
	failedIncident := incident
	failedIncident.ID++
	failedIncident.ExecutionID = "email-postgres-failure"
	if err := db.Transaction(func(tx *gorm.DB) error {
		return NewNotificationService(nil, emailService).
			RecordAlertEventTx(tx, failedIncident, monitorModels.AlertEventOpened, "", now.Add(2*time.Second))
	}); err != nil {
		t.Fatal(err)
	}
	processed, err = dispatcher.DispatchOnce(context.Background(), now.Add(3*time.Second))
	if err != nil || !processed {
		t.Fatalf("dispatch failure: processed=%v err=%v", processed, err)
	}
	var dead monitorModels.EmailDelivery
	if err := db.Where("tenant_id = ? AND status = ?", tenantID, monitorModels.EmailDeliveryDead).First(&dead).Error; err != nil {
		t.Fatal(err)
	}
	if dead.AttemptCount != 1 || dead.LastError == "" {
		t.Fatalf("dead email = %#v", dead)
	}
	requeued, err := emailService.RetryDelivery(context.Background(), tenantID, dead.DeliveryID, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if requeued.DeliveryID != dead.DeliveryID || requeued.RetryBaseAttemptCount != 1 || requeued.ManualRetryCount != 1 {
		t.Fatalf("requeued email = %#v", requeued)
	}
	if err := emailService.DeleteDestination(context.Background(), tenantID, destination.ID, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	var cancelled monitorModels.EmailDelivery
	if err := db.First(&cancelled, requeued.ID).Error; err != nil {
		t.Fatalf("historical email missing after destination delete: %v", err)
	}
	if cancelled.Status != monitorModels.EmailDeliveryCancelled {
		t.Fatalf("pending retry was not cancelled: %#v", cancelled)
	}
}
