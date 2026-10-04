package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/secretcipher"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestIntegrationPostgresPlatformLogWeComCredentialRetryAndRecovery(t *testing.T) {
	f := newLogRetryFixture(t, "wecom")
	ctx := context.Background()
	key, err := secretcipher.Decrypt(f.target.SecretCiphertext, f.n.key)
	if err != nil || key != fixtureWeComKey || strings.Contains(f.target.URL, "key=") {
		t.Fatal("robot token not isolated")
	}
	if _, err = f.n.Save(ctx, 0, LogDestinationInput{Name: uuid.NewString(), Channel: "wecom", EventTypes: []string{"opened"}, Enabled: true}); !errors.Is(err, ErrLogInvalid) {
		t.Fatal("enabled create accepted without credential")
	}
	if _, err = f.n.Save(ctx, 0, LogDestinationInput{Name: uuid.NewString(), Channel: "wecom", URL: wecomEndpoint + "?key=" + fixtureWeComKey, EventTypes: []string{"opened"}}); !errors.Is(err, ErrLogInvalid) {
		t.Fatal("credential accepted in ordinary target")
	}
	if _, err = f.n.SetSecret(ctx, f.target.ID, f.target.Version, wecomEndpoint+"?key="+fixtureWeComKey+"&extra=1"); !errors.Is(err, ErrLogInvalid) {
		t.Fatal("bad credential accepted")
	}
	if _, err = f.n.SetSecret(ctx, f.target.ID, f.target.Version-1, wecomEndpoint+"?key="+fixtureWeComKey); !errors.Is(err, ErrLogConflict) {
		t.Fatal("stale credential version accepted")
	}
	rotatedKey := "87654321-1234-1234-1234-123456789abc"
	f.target, err = f.n.SetSecret(ctx, f.target.ID, f.target.Version, wecomEndpoint+"?key="+rotatedKey)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := f.n.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(listed)
	if strings.Contains(string(encoded), rotatedKey) || strings.Contains(string(encoded), "secret_ciphertext") {
		t.Fatal("target read leaks key")
	}
	for _, target := range listed {
		if target.ID == f.target.ID && !target.SecretConfigured {
			t.Fatal("credential status missing")
		}
	}

	calls := 0
	var bodies []string
	sender := NewHTTPWebhookSender(time.Second, false)
	sender.client.Transport = wecomRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("key") != rotatedKey {
			t.Fatal("dispatcher did not use rotated key")
		}
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		response := `{"errcode":0,"errmsg":"ok"}`
		if calls == 1 {
			response = `{"errcode":45009,"errmsg":"secret ` + rotatedKey + `"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: http.Header{}}, nil
	})
	f.n.wecom = sender
	now := time.Now().UTC().Truncate(time.Microsecond)
	zero := 0
	if _, err = f.n.Retry(ctx, f.delivery.ID, LogDeliveryRetryInput{ExpectedManualRetryCount: &zero, DestinationVersion: f.target.Version}, now); err != nil {
		t.Fatal(err)
	}
	if processed, err := f.n.DispatchOnce(ctx, now); err != nil || !processed {
		t.Fatalf("dispatch %v %v", processed, err)
	}
	view := f.read(t)
	if view.Status != "pending" || view.LastError != "wecom_rate_limited" || view.SecretCiphertext == "" || view.NextAttemptAt == nil {
		t.Fatal("HTTP 200 business error was counted as delivered")
	}
	if processed, err := f.n.DispatchOnce(ctx, now.Add(2*time.Second)); err != nil || !processed {
		t.Fatalf("retry %v %v", processed, err)
	}
	view = f.read(t)
	if view.Status != "delivered" || view.SecretCiphertext != "" || view.ManualRetryCount != 1 || view.CycleAttemptCount != 2 || !view.OccurredAt.Equal(f.event.OccurredAt) {
		t.Fatal("retry lost original facts or retained credential")
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] || !strings.Contains(bodies[0], f.event.ID) {
		t.Fatal("retry changed robot payload identity")
	}
	if processed, err := f.n.DispatchOnce(ctx, now.Add(3*time.Second)); err != nil || processed {
		t.Fatal("completed delivery sent twice")
	}

	event := models.PlatformLogEvent{ID: uuid.NewString(), IncidentID: f.incident.ID, Type: "resolved", Severity: "critical", OccurredAt: now}
	t.Cleanup(func() {
		for _, model := range []any{&models.PlatformLogDelivery{}, &models.PlatformLogEvent{}} {
			field := "event_id"
			if _, ok := model.(*models.PlatformLogEvent); ok {
				field = "id"
			}
			if err := f.db.Where(field+"=?", event.ID).Delete(model).Error; err != nil {
				t.Error(err)
			}
			var remaining int64
			if err := f.db.Model(model).Where(field+"=?", event.ID).Count(&remaining).Error; err != nil || remaining != 0 {
				t.Errorf("recovery cleanup count=%d err=%v", remaining, err)
			}
		}
	})
	if err = f.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return f.n.RecordTx(tx, event, f.incident, now)
	}); err != nil {
		t.Fatal(err)
	}
	if processed, err := f.n.DispatchOnce(ctx, now.Add(4*time.Second)); err != nil || !processed {
		t.Fatal("recovery notification not sent")
	}
	if len(bodies) != 3 || !strings.Contains(bodies[2], "告警恢复") || !strings.Contains(bodies[2], event.ID) {
		t.Fatal("missing distinct recovery notification")
	}
	var countBefore, countAfter int64
	if err := f.db.Model(&models.PlatformLogDelivery{}).Where("destination_id=?", f.target.ID).Count(&countBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.n.Test(ctx, f.target.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&models.PlatformLogDelivery{}).Where("destination_id=?", f.target.ID).Count(&countAfter).Error; err != nil {
		t.Fatal(err)
	}
	if countBefore != countAfter || !strings.Contains(bodies[len(bodies)-1], "无需处理") {
		t.Fatal("test created formal delivery or used alert content")
	}
}

func TestIntegrationPostgresPlatformLogSubscriptionsRejectEscalation(t *testing.T) {
	for _, channel := range []string{"webhook", "wecom", "email"} {
		t.Run(channel, func(t *testing.T) {
			f := newLogRetryFixture(t, channel)
			input := LogDestinationInput{Version: f.target.Version, Name: f.target.Name, Channel: channel, EventTypes: []string{"opened", "escalated", "resolved"}, Enabled: true}
			if channel == "webhook" {
				input.URL = f.target.URL
			}
			if channel == "email" {
				input.Recipients = f.target.Recipients
			}
			if _, err := f.n.Save(context.Background(), f.target.ID, input); !errors.Is(err, ErrLogInvalid) {
				t.Fatalf("platform escalation subscription accepted: %v", err)
			}
			var target models.PlatformLogDestination
			if err := f.db.First(&target, f.target.ID).Error; err != nil {
				t.Fatal(err)
			}
			if target.Version != f.target.Version {
				t.Fatal("invalid subscription modified the target")
			}
		})
	}
}

func TestIntegrationPostgresPlatformLogSubscriptionConvergence(t *testing.T) {
	// Create every fixture before seeding obsolete configuration: startup itself
	// performs convergence, so later fixture creation must not alter the evidence.
	full := newLogRetryFixture(t, "wecom")
	only := newLogRetryFixture(t, "webhook")
	current := newLogRetryFixture(t, "wecom")
	obsolete := newLogRetryFixture(t, "wecom")
	history := newLogRetryFixture(t, "wecom")
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, f := range []*logRetryFixture{full, only, current, obsolete, history} {
		if err := f.db.Model(&f.delivery).Updates(map[string]any{"status": "pending", "secret_ciphertext": f.target.SecretCiphertext, "next_attempt_at": now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, seed := range []struct {
		f      *logRetryFixture
		events models.StringList
	}{{full, models.StringList{"opened", "escalated", "resolved"}}, {only, models.StringList{"escalated"}}} {
		if err := seed.f.db.Model(&seed.f.target).Update("event_types", seed.events).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []*logRetryFixture{obsolete, history} {
		if err := f.db.Model(&f.event).Update("type", "escalated").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := history.db.Model(&history.delivery).Updates(map[string]any{"status": "delivered", "secret_ciphertext": "", "next_attempt_at": nil, "delivered_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if err := EnsureMonitorStore(full.db); err != nil {
			t.Fatal(err)
		}
		for _, f := range []*logRetryFixture{full, only, current, obsolete, history} {
			var target models.PlatformLogDestination
			var delivery models.PlatformLogDelivery
			if err := f.db.First(&target, f.target.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.First(&delivery, "id=?", f.delivery.ID).Error; err != nil {
				t.Fatal(err)
			}
			wantVersion, wantEnabled := f.target.Version, true
			wantEvents := models.StringList{"opened", "resolved"}
			if f == full || f == only {
				wantVersion++
			}
			if f == only {
				wantEvents, wantEnabled = models.StringList{}, false
			}
			if target.Version != wantVersion || target.Enabled != wantEnabled || !reflect.DeepEqual(target.EventTypes, wantEvents) || target.SecretCiphertext != f.target.SecretCiphertext || target.URL != f.target.URL {
				t.Fatalf("convergence changed unsupported target facts or was not idempotent: run=%d target=%d", run, target.ID)
			}
			wantStatus := "pending"
			if f == only || f == obsolete {
				wantStatus = "cancelled"
			} else if f == history {
				wantStatus = "delivered"
			}
			if delivery.Status != wantStatus || delivery.Payload != f.delivery.Payload || delivery.AttemptCount != 3 {
				t.Fatal("convergence rewrote delivery history or left obsolete work pending")
			}
			if wantStatus == "pending" {
				if delivery.SecretCiphertext != f.target.SecretCiphertext || delivery.NextAttemptAt == nil || !delivery.NextAttemptAt.Equal(now) {
					t.Fatal("supported pending delivery was modified")
				}
			} else if delivery.SecretCiphertext != "" || delivery.NextAttemptAt != nil {
				t.Fatal("completed or cancelled delivery retains retry credentials")
			}
			if f == obsolete || f == history {
				var event models.PlatformLogEvent
				if err := f.db.First(&event, "id=?", f.event.ID).Error; err != nil || event.Type != "escalated" || !event.OccurredAt.Equal(f.event.OccurredAt) {
					t.Fatal("immutable historical event was rewritten")
				}
			}
		}
	}
}
