package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	secretcipher "github.com/addp/common/secretcipher"
	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresWebhookDeliveryClaimAndDispatch(t *testing.T) {
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
	var dueIndexCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'monitor' AND indexname IN (?, ?)`,
		"idx_monitor_webhook_delivery_pending", "idx_monitor_webhook_delivery_expired_lease").
		Scan(&dueIndexCount).Error; err != nil {
		t.Fatalf("query delivery indexes: %v", err)
	}
	if dueIndexCount != 2 {
		t.Fatalf("delivery due index count = %d, want 2", dueIndexCount)
	}
	var retryColumnCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'monitor' AND table_name = 'webhook_deliveries'
		  AND column_name IN (?, ?)`, "retry_base_attempt_count", "manual_retry_count").
		Scan(&retryColumnCount).Error; err != nil {
		t.Fatalf("query retry columns: %v", err)
	}
	if retryColumnCount != 2 {
		t.Fatalf("retry column count = %d, want 2", retryColumnCount)
	}
	tenantID := int(time.Now().UnixNano()%100000000) + 800000000
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.WebhookDelivery{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.AlertEvent{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.WebhookDestination{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&monitorModels.AlertIncident{}).Error
	})

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/fail" {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	key := []byte("addp-dev-encryption-key-2025!!!!")
	sender := NewHTTPWebhookSender(2*time.Second, true)
	webhookService := NewWebhookService(db, key, true, "http://localhost:5170", sender)
	destination, err := webhookService.CreateDestination(context.Background(), CreateWebhookDestinationInput{
		TenantID: tenantID, Name: "postgres-integration", URL: server.URL, Secret: "0123456789abcdef",
		Enabled: false, EventTypes: []string{monitorModels.AlertEventOpened},
	})
	if err != nil {
		t.Fatalf("create destination: %v", err)
	}
	if destination.Enabled {
		t.Fatal("PostgreSQL persisted explicitly disabled destination as enabled")
	}
	testResult, err := webhookService.TestDestination(context.Background(), tenantID, destination.ID, time.Now().UTC())
	if err != nil || testResult.HTTPStatus != http.StatusAccepted {
		t.Fatalf("test destination result = %#v, err = %v", testResult, err)
	}
	if _, err := webhookService.CreateDestination(context.Background(), CreateWebhookDestinationInput{
		TenantID: tenantID, Name: "postgres-integration", URL: server.URL, Secret: "0123456789abcdef",
		Enabled: true, EventTypes: []string{monitorModels.AlertEventOpened},
	}); !errors.Is(err, ErrWebhookDestinationConflict) {
		t.Fatalf("PostgreSQL duplicate destination error = %v", err)
	}
	secret, err := secretcipher.Encrypt("0123456789abcdef", key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	deliveryID := uuid.NewString()
	identity := uint(now.UnixNano())
	delivery := monitorModels.WebhookDelivery{
		DeliveryID: deliveryID, TenantID: tenantID, DestinationID: destination.ID,
		DestinationName: "postgres-integration", AlertEventID: identity, IncidentID: identity,
		EventType: monitorModels.AlertEventOpened, RequestURL: server.URL, SecretCiphertext: secret,
		Payload: commonModels.JSONMap{"schema_version": "monitor.alert.webhook/v1", "delivery_id": deliveryID},
		Status:  monitorModels.WebhookDeliveryPending, NextAttemptAt: &now,
	}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery: %v", err)
	}
	dispatcher := NewWebhookDispatcher(db, NewHTTPWebhookSender(2*time.Second, true), WebhookDispatcherConfig{
		WorkerID: "integration-worker", LeaseDuration: 5 * time.Second, MaxAttempts: 3,
		RetryInitial: time.Second, RetryMax: time.Minute, EncryptionKey: key,
	})
	processed, err := dispatcher.DispatchOnce(context.Background(), now.Add(time.Second))
	if err != nil {
		t.Fatalf("dispatch once: %v", err)
	}
	if !processed {
		t.Fatal("due delivery was not claimed")
	}
	var stored monitorModels.WebhookDelivery
	if err := db.First(&stored, delivery.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != monitorModels.WebhookDeliveryDelivered || stored.AttemptCount != 1 || stored.DeliveredAt == nil {
		t.Fatalf("stored delivery = %#v", stored)
	}
	if stored.SecretCiphertext != "" {
		t.Fatal("terminal delivery retained encrypted secret")
	}

	failedDeliveryID := uuid.NewString()
	failedIdentity := identity + 1
	failedDelivery := monitorModels.WebhookDelivery{
		DeliveryID: failedDeliveryID, TenantID: tenantID, DestinationID: destination.ID,
		DestinationName: "postgres-integration-failure", AlertEventID: failedIdentity, IncidentID: failedIdentity,
		EventType: monitorModels.AlertEventOpened, RequestURL: server.URL + "/fail", SecretCiphertext: secret,
		Payload: commonModels.JSONMap{"schema_version": "monitor.alert.webhook/v1", "delivery_id": failedDeliveryID},
		Status:  monitorModels.WebhookDeliveryPending, NextAttemptAt: &now,
	}
	if err := db.Create(&failedDelivery).Error; err != nil {
		t.Fatalf("create failed delivery: %v", err)
	}
	deadDispatcher := NewWebhookDispatcher(db, NewHTTPWebhookSender(2*time.Second, true), WebhookDispatcherConfig{
		WorkerID: "integration-dead-worker", LeaseDuration: 5 * time.Second, MaxAttempts: 1,
		RetryInitial: time.Second, RetryMax: time.Minute, EncryptionKey: key,
	})
	processed, err = deadDispatcher.DispatchOnce(context.Background(), now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("dispatch terminal failure: %v", err)
	}
	if !processed {
		t.Fatal("failed delivery was not claimed")
	}
	var deadStored monitorModels.WebhookDelivery
	if err := db.First(&deadStored, failedDelivery.ID).Error; err != nil {
		t.Fatal(err)
	}
	if deadStored.Status != monitorModels.WebhookDeliveryDead || deadStored.AttemptCount != 1 || deadStored.LastHTTPStatus == nil || *deadStored.LastHTTPStatus != 500 {
		t.Fatalf("dead delivery = %#v", deadStored)
	}
	if deadStored.SecretCiphertext != "" || deadStored.LastError == "" {
		t.Fatalf("dead delivery terminal fields = %#v", deadStored)
	}
	enabled := true
	failureURL := server.URL + "/fail"
	if _, err := webhookService.UpdateDestination(context.Background(), UpdateWebhookDestinationInput{
		TenantID: tenantID, ID: destination.ID, Enabled: &enabled, URL: &failureURL,
	}); err != nil {
		t.Fatalf("enable failure destination: %v", err)
	}
	requeued, err := webhookService.RetryDelivery(context.Background(), tenantID, failedDeliveryID, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("manual retry: %v", err)
	}
	if requeued.DeliveryID != failedDeliveryID || requeued.AttemptCount != 1 ||
		requeued.RetryBaseAttemptCount != 1 || requeued.ManualRetryCount != 1 {
		t.Fatalf("requeued delivery = %#v", requeued)
	}
	processed, err = deadDispatcher.DispatchOnce(context.Background(), now.Add(4*time.Second))
	if err != nil || !processed {
		t.Fatalf("dispatch manual retry: processed=%v err=%v", processed, err)
	}
	if err := db.First(&deadStored, failedDelivery.ID).Error; err != nil {
		t.Fatal(err)
	}
	if deadStored.Status != monitorModels.WebhookDeliveryDead || deadStored.AttemptCount != 2 ||
		deadStored.RetryBaseAttemptCount != 1 || deadStored.ManualRetryCount != 1 {
		t.Fatalf("retried dead delivery = %#v", deadStored)
	}
	if err := webhookService.DeleteDestination(context.Background(), tenantID, destination.ID, now.Add(5*time.Second)); err != nil {
		t.Fatalf("delete destination: %v", err)
	}
	var destinationCount int64
	if err := db.Model(&monitorModels.WebhookDestination{}).Where("id = ?", destination.ID).Count(&destinationCount).Error; err != nil {
		t.Fatal(err)
	}
	if destinationCount != 0 {
		t.Fatalf("deleted destination count = %d", destinationCount)
	}
	if err := db.First(&deadStored, failedDelivery.ID).Error; err != nil {
		t.Fatalf("historical delivery missing after destination delete: %v", err)
	}
}

func webhookIntegrationDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		webhookIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		webhookIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		webhookIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		webhookIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		webhookIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
		webhookIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	)
}

func webhookIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
