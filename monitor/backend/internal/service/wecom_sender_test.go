package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/addp/monitor/internal/models"
)

const fixtureWeComKey = "12345678-1234-1234-1234-123456789abc"

type wecomRoundTripFunc func(*http.Request) (*http.Response, error)

func (f wecomRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWeComCredentialAcceptsOnlyOfficialEndpoint(t *testing.T) {
	valid := wecomEndpoint + "?key=" + fixtureWeComKey
	key, err := wecomKey(valid)
	if err != nil || key != fixtureWeComKey {
		t.Fatal("valid robot URL rejected")
	}
	for _, raw := range []string{
		"http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + fixtureWeComKey,
		"https://qyapi.weixin.qq.com.attacker.test/cgi-bin/webhook/send?key=" + fixtureWeComKey,
		"https://qyapi.weixin.qq.com:443/cgi-bin/webhook/send?key=" + fixtureWeComKey,
		"https://user@qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + fixtureWeComKey,
		valid + "#fragment", valid + "&key=second", valid + "&extra=1", wecomEndpoint + "?key=", wecomEndpoint + "?key=%ZZ",
		wecomEndpoint + "/extra?key=" + fixtureWeComKey, fixtureWeComKey,
	} {
		if _, err := wecomKey(raw); !errors.Is(err, ErrLogInvalid) {
			t.Errorf("unsafe credential accepted: %q", raw)
		}
	}
}

func TestWeComSenderChecksBusinessResultAndNeverLeaksCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		network    bool
		want       error
	}{
		{"accepted", `{"errcode":0,"errmsg":"ok"}`, 200, false, nil},
		{"rejected", `{"errcode":93000,"errmsg":"secret ` + fixtureWeComKey + `"}`, 200, false, ErrWeComRejected},
		{"limited", `{"errcode":45009}`, 200, false, ErrWeComRateLimited},
		{"missing", `{}`, 200, false, ErrWeComResponse},
		{"null", `{"errcode":null}`, 200, false, ErrWeComResponse},
		{"string", `{"errcode":"0"}`, 200, false, ErrWeComResponse},
		{"fraction", `{"errcode":0.5}`, 200, false, ErrWeComResponse},
		{"html", `<html>unavailable</html>`, 200, false, ErrWeComResponse},
		{"trailing", `{"errcode":0}garbage`, 200, false, ErrWeComResponse},
		{"oversize", `{"errcode":0,"extra":"` + strings.Repeat("a", 4096) + `"}`, 200, false, ErrWeComResponse},
		{"http", fixtureWeComKey, 503, false, ErrWeComHTTP},
		{"redirect", "", 302, false, ErrWeComHTTP},
		{"network", "", 0, true, ErrWeComNetwork},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := NewHTTPWebhookSender(time.Second, false)
			called := false
			sender.client.Transport = wecomRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.Method != "POST" || r.URL.Scheme != "https" || r.URL.Host != "qyapi.weixin.qq.com" || r.URL.Query().Get("key") != fixtureWeComKey || r.Header.Get("X-ADDP-Webhook-Signature") != "" || r.Header.Get("Content-Type") != "application/json" {
					t.Fatal("wrong robot request")
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["msgtype"] != "markdown" {
					t.Fatal("wrong message body")
				}
				if tc.network {
					return nil, errors.New("network error with " + fixtureWeComKey)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
			})
			payload, _ := wecomLogPayload(`{"schema":"addp.platform-log-notification-test/v1","test":true}`, "delivery-1")
			_, err := sender.SendWeComMessage(context.Background(), WebhookMessage{RequestURL: wecomEndpoint, Payload: payload}, fixtureWeComKey, time.Now())
			if !called || !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), fixtureWeComKey) {
				t.Fatal("credential leaked in error")
			}
			if err != nil && platformNotificationError(err) == "notification_send_failed" {
				t.Fatal("missing safe failure reason")
			}
		})
	}
}

func TestWeComMessagePreservesOriginalEventAndRejectsMarkdownInjection(t *testing.T) {
	for _, eventType := range []string{"opened", "resolved"} {
		body, _ := json.Marshal(platformLogMessage{Schema: "addp.platform-log-alert/v1", EventID: "event-1", EventType: eventType, IncidentID: 7, Node: "host\n<@all>", Signal: "receiver_loss", InstanceID: "instance-1", Severity: "critical", OccurredAt: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)})
		payload, err := wecomLogPayload(string(body), "delivery-1")
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(payload)
		for _, expected := range []string{"event-1", "delivery-1", "instance-1", "2026-10-03T00:00:00Z", "接收器新增丢弃或写入失败"} {
			if !strings.Contains(string(encoded), expected) {
				t.Errorf("missing %s", expected)
			}
		}
		content := payload.(map[string]any)["markdown"].(map[string]string)["content"]
		if strings.Contains(content, "<@all>") || strings.Contains(content, "host\n") {
			t.Fatal("untrusted node mentions group")
		}
	}
	unsupported, _ := json.Marshal(platformLogMessage{Schema: "addp.platform-log-alert/v1", EventID: "event-1", EventType: "escalated", IncidentID: 7, Node: "host", Signal: "source_quota", Severity: "critical", OccurredAt: time.Now().UTC()})
	if _, err := wecomLogPayload(string(unsupported), "d"); !errors.Is(err, ErrLogInvalid) {
		t.Fatal("platform escalation accepted")
	}
	if _, err := wecomLogPayload(`{"schema":"other","test":true}`, "d"); !errors.Is(err, ErrLogInvalid) {
		t.Fatal("unknown event accepted")
	}
}

func TestPlatformLogOutboxRejectsUnsupportedEvents(t *testing.T) {
	n := NewPlatformLogNotifications(nil, nil, false, nil, nil, 3, 0, 0, 0)
	if err := n.RecordTx(nil, models.PlatformLogEvent{Type: "escalated"}, models.PlatformLogIncident{}, time.Now()); !errors.Is(err, ErrLogInvalid) {
		t.Fatal("platform outbox accepted an unsupported lifecycle event")
	}
}
