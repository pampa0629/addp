package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type retryTestWebhook struct {
	fail     bool
	messages []WebhookMessage
	secrets  []string
}

func (s *retryTestWebhook) SendMessage(_ context.Context, m WebhookMessage, secret string, _ time.Time) (WebhookSendResult, error) {
	s.messages = append(s.messages, m)
	s.secrets = append(s.secrets, secret)
	if s.fail {
		return WebhookSendResult{HTTPStatus: 500}, errors.New("fixture unavailable")
	}
	return WebhookSendResult{HTTPStatus: 202}, nil
}

func (s *retryTestWebhook) SendWeComMessage(ctx context.Context, m WebhookMessage, secret string, now time.Time) (WebhookSendResult, error) {
	result, err := s.SendMessage(ctx, m, secret, now)
	if err != nil {
		return result, ErrWeComRejected
	}
	return result, nil
}

type retryTestEmail struct{ messages []EmailMessage }

func (s *retryTestEmail) SendMessage(_ context.Context, m EmailMessage, _ time.Time) error {
	s.messages = append(s.messages, m)
	return nil
}

type logRetryFixture struct {
	db       *gorm.DB
	n        *PlatformLogNotifications
	target   models.PlatformLogDestination
	incident models.PlatformLogIncident
	event    models.PlatformLogEvent
	delivery models.PlatformLogDelivery
	webhook  *retryTestWebhook
	email    *retryTestEmail
}

func newLogRetryFixture(t *testing.T, channel string) *logRetryFixture {
	t.Helper()
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
	f := &logRetryFixture{db: db, webhook: &retryTestWebhook{}, email: &retryTestEmail{}}
	f.n = NewPlatformLogNotifications(db, []byte("addp-dev-encryption-key-2025!!!!"), true, f.webhook, f.email, 3, 30*time.Second, time.Second, 5*time.Second)
	now := time.Now().UTC().Truncate(time.Microsecond)
	f.incident = models.PlatformLogIncident{Node: "retry-" + uuid.NewString(), Signal: "delivery_probe", Severity: "critical", Status: "resolved", OpenedAt: now.Add(-time.Hour), LastObservedAt: now, ResolvedAt: &now}
	f.event = models.PlatformLogEvent{ID: uuid.NewString(), Type: "opened", Severity: "critical", OccurredAt: f.incident.OpenedAt}
	t.Cleanup(func() {
		for _, q := range []struct {
			model any
			where string
			value any
		}{
			{&models.PlatformLogDelivery{}, "event_id=?", f.event.ID},
			{&models.PlatformLogEvent{}, "id=?", f.event.ID},
			{&models.PlatformLogIncident{}, "node=?", f.incident.Node},
			{&models.PlatformLogDestination{}, "id=?", f.target.ID},
		} {
			if err := db.Where(q.where, q.value).Delete(q.model).Error; err != nil {
				t.Error(err)
			}
			var count int64
			if err := db.Model(q.model).Where(q.where, q.value).Count(&count).Error; err != nil || count != 0 {
				t.Errorf("fixture cleanup count=%d err=%v", count, err)
			}
		}
	})
	if err := db.Create(&f.incident).Error; err != nil {
		t.Fatal(err)
	}
	f.event.IncidentID = f.incident.ID
	if err := db.Create(&f.event).Error; err != nil {
		t.Fatal(err)
	}
	input := LogDestinationInput{Name: f.incident.Node, Channel: channel, EventTypes: []string{"opened", "resolved"}}
	if channel == "webhook" {
		input.URL = "http://127.0.0.1:18080/original"
	} else if channel == "email" {
		input.Recipients = []string{"original@example.test"}
		input.Enabled = true
	}
	f.target, err = f.n.Save(context.Background(), 0, input)
	if err != nil {
		t.Fatal(err)
	}
	if channel != "email" {
		secret := "fixture-original-secret"
		if channel == "wecom" {
			secret = wecomEndpoint + "?key=" + fixtureWeComKey
		}
		f.target, err = f.n.SetSecret(context.Background(), f.target.ID, f.target.Version, secret)
		if err != nil {
			t.Fatal(err)
		}
		input.Version = f.target.Version
		input.Enabled = true
		f.target, err = f.n.Save(context.Background(), f.target.ID, input)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return f.n.RecordTx(tx, f.event, f.incident, f.event.OccurredAt) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("event_id=? AND destination_id=?", f.event.ID, f.target.ID).First(&f.delivery).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&f.delivery).Updates(map[string]any{"status": "dead", "attempt_count": 3, "secret_ciphertext": "", "next_attempt_at": nil, "last_error": "notification_send_failed"}).Error; err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *logRetryFixture) read(t *testing.T) LogDeliveryView {
	t.Helper()
	var view LogDeliveryView
	if err := logDeliveryQuery(f.db).Where("d.id=?", f.delivery.ID).Scan(&view).Error; err != nil {
		t.Fatal(err)
	}
	return view
}

func TestIntegrationPostgresPlatformLogManualRetryCyclesAndConcurrency(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	ctx, now := context.Background(), time.Now().UTC().Truncate(time.Microsecond)
	var err error
	f.target, err = f.n.SetSecret(ctx, f.target.ID, f.target.Version, "fixture-rotated-secret")
	if err != nil {
		t.Fatal(err)
	}
	f.target, err = f.n.Save(ctx, f.target.ID, LogDestinationInput{Version: f.target.Version, Name: f.target.Name, Channel: "webhook", URL: "http://127.0.0.1:18080/current", Enabled: true, EventTypes: []string{"opened", "resolved"}})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	input := LogDeliveryRetryInput{ExpectedManualRetryCount: &zero, DestinationVersion: f.target.Version}
	var success atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.n.Retry(ctx, f.delivery.ID, input, now)
			if err == nil {
				success.Add(1)
			} else if !errors.Is(err, ErrLogConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("concurrent requeues=%d", success.Load())
	}
	view := f.read(t)
	if view.Status != "pending" || view.ManualRetryCount != 1 || view.AttemptCount != 3 || view.CycleAttemptCount != 0 || view.IncidentStatus != "resolved" || !view.OccurredAt.Equal(f.event.OccurredAt) {
		t.Fatal("requeue reset history or altered incident facts")
	}
	f.webhook.fail = true
	for cycle := 1; cycle <= 3; cycle++ {
		now = now.Add(10 * time.Second)
		if processed, err := f.n.DispatchOnce(ctx, now); err != nil || !processed {
			t.Fatalf("dispatch=%v err=%v", processed, err)
		}
		view = f.read(t)
		if view.CycleAttemptCount != cycle || view.AttemptCount != 3+cycle {
			t.Fatal("incorrect cycle or total attempts")
		}
		if cycle == 1 && (view.NextAttemptAt == nil || !view.NextAttemptAt.Equal(now.Add(time.Second))) {
			t.Fatal("retry backoff uses total count instead of cycle count")
		}
	}
	if view.Status != "dead" || view.SecretCiphertext != "" || view.NextAttemptAt != nil {
		t.Fatal("retry cycle did not terminate safely")
	}
	if _, err := f.n.Retry(ctx, f.delivery.ID, input, now); !errors.Is(err, ErrLogConflict) {
		t.Fatal("delayed first-cycle request opened another cycle")
	}
	one := 1
	input.ExpectedManualRetryCount = &one
	if _, err := f.n.Retry(ctx, f.delivery.ID, input, now); err != nil {
		t.Fatal(err)
	}
	f.webhook.fail = false
	if processed, err := f.n.DispatchOnce(ctx, now); err != nil || !processed {
		t.Fatalf("dispatch=%v err=%v", processed, err)
	}
	view = f.read(t)
	if view.Status != "delivered" || view.ManualRetryCount != 2 || view.AttemptCount != 7 || view.CycleAttemptCount != 1 || view.SecretCiphertext != "" {
		t.Fatal("delivery did not finish the new bounded cycle")
	}
	var expected any
	if err := json.Unmarshal([]byte(f.delivery.Payload), &expected); err != nil {
		t.Fatal(err)
	}
	for i, m := range f.webhook.messages {
		if m.DeliveryID != f.delivery.ID || m.RequestURL != f.target.URL || !reflect.DeepEqual(m.Payload, expected) || f.webhook.secrets[i] != "fixture-rotated-secret" {
			t.Fatal("manual retry altered identity/content or used stale target credentials")
		}
	}
	var events int64
	if err := f.db.Model(&models.PlatformLogEvent{}).Where("incident_id=?", f.incident.ID).Count(&events).Error; err != nil || events != 1 {
		t.Fatal("retry manufactured lifecycle events")
	}
	if _, err := f.n.Retry(ctx, f.delivery.ID, LogDeliveryRetryInput{ExpectedManualRetryCount: &view.ManualRetryCount, DestinationVersion: f.target.Version}, now); !errors.Is(err, ErrLogConflict) {
		t.Fatal("delivered message accepted for retry")
	}
}

func TestIntegrationPostgresPlatformLogManualRetryGuards(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	ctx, now := context.Background(), time.Now().UTC().Truncate(time.Microsecond)
	zero := 0
	input := LogDeliveryRetryInput{ExpectedManualRetryCount: &zero, DestinationVersion: f.target.Version}
	for _, status := range []string{"pending", "delivering", "delivered", "suppressed", "cancelled"} {
		if err := f.db.Model(&f.delivery).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := f.n.Retry(ctx, f.delivery.ID, input, now); !errors.Is(err, ErrLogConflict) {
			t.Fatalf("status %s accepted: %v", status, err)
		}
	}
	if err := f.db.Model(&f.delivery).Update("status", "dead").Error; err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []LogDeliveryRetryInput{{DestinationVersion: f.target.Version}, {ExpectedManualRetryCount: &zero}} {
		if _, err := f.n.Retry(ctx, f.delivery.ID, invalid, now); !errors.Is(err, ErrLogInvalid) {
			t.Fatal("missing concurrency input accepted")
		}
	}
	stale := input
	stale.DestinationVersion++
	if _, err := f.n.Retry(ctx, f.delivery.ID, stale, now); !errors.Is(err, ErrLogConflict) {
		t.Fatal("stale target accepted")
	}
	if _, err := f.n.Retry(ctx, uuid.NewString(), input, now); !errors.Is(err, ErrLogNotFound) {
		t.Fatal("missing delivery did not return not found")
	}
	for _, guard := range []struct {
		field          string
		value, restore any
	}{
		{"enabled", false, true}, {"secret_ciphertext", "", f.target.SecretCiphertext}, {"secret_ciphertext", "invalid-ciphertext", f.target.SecretCiphertext}, {"event_types", models.StringList{"resolved"}, f.target.EventTypes},
	} {
		if err := f.db.Model(&f.target).Update(guard.field, guard.value).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := f.n.Retry(ctx, f.delivery.ID, input, now); !errors.Is(err, ErrLogRetryUnavailable) {
			t.Fatalf("guard %s failed: %v", guard.field, err)
		}
		if err := f.db.Model(&f.target).Update(guard.field, guard.restore).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Model(&f.incident).Update("suppressed_until", now.Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.n.Retry(ctx, f.delivery.ID, input, now); !errors.Is(err, ErrLogRetryUnavailable) {
		t.Fatal("suppression bypassed")
	}
	if err := f.db.Model(&f.incident).Update("suppressed_until", nil).Error; err != nil {
		t.Fatal(err)
	}
	f.n.webhook = nil
	if _, err := f.n.Retry(ctx, f.delivery.ID, input, now); !errors.Is(err, ErrLogRetryUnavailable) {
		t.Fatal("missing sender accepted")
	}
	f.n.webhook = f.webhook
	if err := f.n.Delete(ctx, f.target.ID, f.target.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := f.n.Retry(ctx, f.delivery.ID, input, now); !errors.Is(err, ErrLogRetryUnavailable) {
		t.Fatal("deleted target accepted")
	}
	view := f.read(t)
	if view.DestinationVersion != 0 || view.DestinationName != "" || view.Status != "dead" || view.ManualRetryCount != 0 {
		t.Fatal("deleted target erased history or guards changed the delivery")
	}
}

func TestIntegrationPostgresPlatformLogRetryExpiredClaimAndEmail(t *testing.T) {
	t.Run("expired final claim ends only its new cycle", func(t *testing.T) {
		f := newLogRetryFixture(t, "webhook")
		zero := 0
		now := time.Now().UTC().Truncate(time.Microsecond)
		if _, err := f.n.Retry(context.Background(), f.delivery.ID, LogDeliveryRetryInput{ExpectedManualRetryCount: &zero, DestinationVersion: f.target.Version}, now); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Model(&f.delivery).Updates(map[string]any{"status": "delivering", "attempt_count": 6, "lease_expires_at": now.Add(-time.Second)}).Error; err != nil {
			t.Fatal(err)
		}
		if processed, err := f.n.DispatchOnce(context.Background(), now); err != nil || !processed {
			t.Fatalf("dispatch=%v err=%v", processed, err)
		}
		v := f.read(t)
		if v.Status != "dead" || v.LastError != "notification_attempt_limit" || v.SecretCiphertext != "" || v.ClaimID != "" || len(f.webhook.messages) != 0 {
			t.Fatal("expired final attempt sent another request or retained credentials")
		}
	})
	t.Run("email uses current recipients and requires a sender", func(t *testing.T) {
		f := newLogRetryFixture(t, "email")
		zero := 0
		now := time.Now().UTC().Truncate(time.Microsecond)
		ctx := context.Background()
		f.n.email = nil
		input := LogDeliveryRetryInput{ExpectedManualRetryCount: &zero, DestinationVersion: f.target.Version}
		if _, err := f.n.Retry(ctx, f.delivery.ID, input, now); !errors.Is(err, ErrLogRetryUnavailable) {
			t.Fatal("unconfigured SMTP accepted")
		}
		f.n.email = f.email
		var err error
		f.target, err = f.n.Save(ctx, f.target.ID, LogDestinationInput{Version: f.target.Version, Name: f.target.Name, Channel: "email", Enabled: true, Recipients: []string{"current@example.test"}, EventTypes: []string{"opened", "resolved"}})
		if err != nil {
			t.Fatal(err)
		}
		input.DestinationVersion = f.target.Version
		if _, err := f.n.Retry(ctx, f.delivery.ID, input, now); err != nil {
			t.Fatal(err)
		}
		if processed, err := f.n.DispatchOnce(ctx, now); err != nil || !processed {
			t.Fatalf("dispatch=%v err=%v", processed, err)
		}
		if len(f.email.messages) != 1 || f.email.messages[0].DeliveryID != f.delivery.ID || f.email.messages[0].TextBody != f.delivery.Payload || !reflect.DeepEqual(f.email.messages[0].Recipients, []string{"current@example.test"}) {
			t.Fatal("email retry changed content/identity or used stale recipients")
		}
	})
}
