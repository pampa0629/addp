package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
)

// Real HTTP requests feed Common's collector, Prometheus, the production rule
// loop, PostgreSQL outbox and a signed loopback TLS receiver. System's identity
// projection/current-user read is a module fixture; this remains T2 evidence.
func TestIntegrationMetricsHTTPAlertLifecycle(t *testing.T) {
	if os.Getenv("ADDP_HTTP_RULE_LIFECYCLE_INTEGRATION") != "1" {
		t.Skip("requires standard metrics HTTP lifecycle gate")
	}
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" || os.Getenv("ADDP_TEST_POSTGRES_DATABASE") != "addp_test" {
		t.Fatal("requires gate-owned addp_test")
	}
	work := os.Getenv("METRICS_T2_WORK")
	recordFile := os.Getenv("ADDP_PROCESS_T2_RECORDS")
	if work == "" || recordFile != filepath.Join(work, "source", "process-records.json") {
		t.Fatal("unowned HTTP identity record")
	}
	var records []struct {
		ID       string    `json:"id"`
		Module   string    `json:"module_name"`
		Instance string    `json:"instance_id"`
		Role     string    `json:"role"`
		Started  time.Time `json:"started_at"`
		Address  string    `json:"instance"`
	}
	data, e := os.ReadFile(recordFile)
	if e != nil || json.Unmarshal(data, &records) != nil || len(records) != 3 || records[0].Role != "backend" || records[1].Role != "worker" || records[2].Role != "worker" {
		t.Fatal("owned native source records", e)
	}
	r := records[0]
	id, e := strconv.ParseUint(r.ID, 10, 32)
	if e != nil {
		t.Fatal(e)
	}
	host, portText, e := net.SplitHostPort(r.Address)
	if e != nil {
		t.Fatal(e)
	}
	ip, e := netip.ParseAddr(host)
	if e != nil {
		t.Fatal(e)
	}
	port, e := strconv.ParseUint(portText, 10, 16)
	if e != nil {
		t.Fatal(e)
	}
	modeFile := filepath.Join(work, "source", "process-1.json.http-mode")
	t.Cleanup(func() { os.Remove(modeFile) })
	const secret = "http-quality-lifecycle-secret"
	type receipt struct {
		ID, Event, Kind string
		Body            string
	}
	var mu sync.Mutex
	receipts := []receipt{}
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, e := io.ReadAll(io.LimitReader(req.Body, 64<<10))
		stamp := req.Header.Get("X-ADDP-Webhook-Timestamp")
		seconds, timeErr := strconv.ParseInt(stamp, 10, 64)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stamp + "."))
		mac.Write(body)
		var payload struct {
			Schema   string                   `json:"schema"`
			Event    string                   `json:"event_id"`
			Kind     string                   `json:"event_type"`
			Subject  string                   `json:"subject_id"`
			Evidence models.HTTPAlertEvidence `json:"evidence"`
		}
		if e != nil || req.Method != "POST" || timeErr != nil || time.Since(time.Unix(seconds, 0)).Abs() > 5*time.Second || !hmac.Equal([]byte("v1="+hex.EncodeToString(mac.Sum(nil))), []byte(req.Header.Get("X-ADDP-Webhook-Signature"))) || json.Unmarshal(body, &payload) != nil || payload.Schema != "addp.platform-http-alert/v1" || payload.Subject != r.Module || payload.Evidence.Route == nil || *payload.Evidence.Route != "/failure" || strings.Contains(string(body), "https://") {
			t.Error("invalid signed HTTP quality notification")
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		receipts = append(receipts, receipt{req.Header.Get("X-ADDP-Webhook-ID"), payload.Event, payload.Kind, string(body)})
		attempt := len(receipts)
		mu.Unlock()
		if attempt == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(202)
		}
	}))
	t.Cleanup(receiver.Close)
	f := newLogRetryFixture(t, "webhook")
	sender := NewHTTPWebhookSender(2*time.Second, true)
	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())
	sender.client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
	t.Cleanup(sender.client.CloseIdleConnections)
	f.n.webhook = sender
	ctx := context.Background()
	d, e := f.n.Save(ctx, 0, PlatformAlertDestinationInput{Name: "http-quality-lifecycle", Channel: "webhook", URL: receiver.URL, Categories: []string{"http_quality"}, EventTypes: []string{"opened", "resolved"}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		f.db.Where("destination_id=?", d.ID).Delete(&models.PlatformAlertDelivery{})
		f.db.Delete(&models.PlatformAlertDestination{}, d.ID)
	})
	d, e = f.n.SetSecret(ctx, d.ID, d.Version, secret)
	if e != nil {
		t.Fatal(e)
	}
	d, e = f.n.Save(ctx, d.ID, PlatformAlertDestinationInput{Version: d.Version, Name: d.Name, Channel: d.Channel, URL: d.URL, Categories: []string{"http_quality"}, EventTypes: []string{"opened", "resolved"}, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	declared := true
	lease := time.Now().UTC().Add(10 * time.Minute)
	users := &processUsers{rows: []client.RuntimeInstanceReference{{ID: uint(id), ModuleName: r.Module, InstanceID: r.Instance, Role: r.Role, Status: "up", LeaseExpiresAt: lease, ProcessStartedAt: &r.Started, ProcessMetricsDeclared: &declared}}}
	identities := &resourceRuleIdentity{processFacts: []commonmodels.ObservabilityModuleIdentity{{ModuleName: r.Module, InstanceID: r.Instance, Role: r.Role, LeaseExpiresAt: lease, ProcessStartedAt: &r.Started, ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://" + r.Address + "/metrics"}}}}
	backend := metricsIntegrationBackend(t)
	observations := NewResourceObservationService(repository.NewResourceQueryPolicyRepository(f.db), nil, nil, nil, true, resourceSourcePolicyWithNetwork(t, []netip.Prefix{netip.PrefixFrom(ip, ip.BitLen())}, []uint16{uint16(port)}), ProcessObservationDependencies{Users: users, Identities: identities, Backend: backend})
	rules := NewHTTPAlertRuleService(f.db, observations, identities, f.n)
	route := "/failure"
	// The previous (not current) evaluation also needs a complete minute of
	// post-startup scrapes. Wait only for that natural history, never for errors.
	historyDeadline := time.Now().Add(45 * time.Second)
	for {
		trendEnd := time.Now().UTC().Truncate(time.Second).Add(-15 * time.Second)
		trend, err := observations.HTTP(ctx, "fixture-human", "module-fixture-user", HTTPObservationRequest{IDs: []uint{uint(id)}, WindowSeconds: 60, Route: &route, Method: "GET", Trend: true, Start: trendEnd.Add(-30 * time.Second), End: trendEnd})
		if err != nil || len(trend.Data) != 1 {
			t.Fatal("native authorized HTTP trend collection", err, "subjects", len(trend.Data))
		}
		if trend.Data[0].Collection.State != "collecting" || len(trend.Data[0].Observations) > 1 {
			t.Fatal("native authorized HTTP trend collection", "state", trend.Data[0].Collection.State, "groups", len(trend.Data[0].Observations))
		}
		valid := false
		for _, observation := range trend.Data[0].Observations {
			for _, line := range observation.Series {
				if line.MetricKey == "http.failures.percent" {
					for _, point := range line.Points {
						valid = valid || (point.DataState == "valid" && point.Value != nil && *point.Value > 50)
					}
				}
			}
		}
		if valid {
			t.Log("authorized historical HTTP trend retained real failure points")
			break
		}
		if time.Now().After(historyDeadline) {
			t.Fatal("native historical HTTP trend lost valid failure points", "groups", len(trend.Data[0].Observations))
		}
		t.Log("awaiting complete historical HTTP failure window", "groups", len(trend.Data[0].Observations))
		time.Sleep(time.Second)
	}
	off := false
	policy := models.HTTPThresholdPolicy{TriggerValue: 50, RecoveryValue: 10, TriggerSeconds: 15, RecoverySeconds: 15}
	input := HTTPAlertRuleInput{Name: "native HTTP failure", InstanceID: uint(id), Route: &route, Method: "GET", MetricKey: "http.failures.percent", TriggerValue: &policy.TriggerValue, RecoveryValue: &policy.RecoveryValue, TriggerSeconds: &policy.TriggerSeconds, RecoverySeconds: &policy.RecoverySeconds, Enabled: &off}
	rule, e := rules.Save(ctx, "", "module-fixture-user", input, time.Now().UTC())
	if e != nil || rule.Enabled {
		t.Fatal("disabled HTTP create", e)
	}
	t.Cleanup(func() {
		ids := f.db.Model(&models.PlatformAlertIncident{}).Select("id").Where("category='http_quality' AND signal=?", rule.ID)
		events := f.db.Model(&models.PlatformAlertEvent{}).Select("id").Where("incident_id IN (?)", ids)
		f.db.Where("event_id IN (?)", events).Delete(&models.PlatformAlertDelivery{})
		f.db.Where("incident_id IN (?)", ids).Delete(&models.PlatformAlertEvent{})
		f.db.Where("category='http_quality' AND signal=?", rule.ID).Delete(&models.PlatformAlertIncident{})
		f.db.Where("id=?", rule.ID).Delete(&models.HTTPAlertRule{})
	})
	on := true
	input.Enabled = &on
	input.Version = rule.Version
	rule, e = rules.Save(ctx, rule.ID, "module-fixture-user", input, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); rules.Run(runCtx) }()
	t.Cleanup(func() { stop(); <-done })
	read := func() models.PlatformAlertIncident {
		var row models.PlatformAlertIncident
		err := f.db.Where("category='http_quality' AND signal=?", rule.ID).Limit(1).Find(&row).Error
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(125 * time.Second)
		for !check() {
			if time.Now().After(deadline) {
				var saved models.HTTPAlertRule
				f.db.Where("id=?", rule.ID).Take(&saved)
				t.Fatalf("%s timed out: %s/%s", label, saved.LastVerdict, saved.LastError)
			}
			time.Sleep(time.Second)
		}
		t.Log(label)
	}
	wait("real HTTP 503 window opened an incident", func() bool { return read().Status == "open" })
	incident := read()
	if incident.HTTPEvidence == nil || incident.HTTPEvidence.RuleID != rule.ID || incident.HTTPEvidence.Minimum <= 50 {
		t.Fatal("missing real opening evidence")
	}
	deliver := func(want int64) {
		wait("signed HTTP delivery persisted", func() bool {
			if _, e := f.n.DispatchOnce(ctx, time.Now().UTC()); e != nil {
				t.Fatal(e)
			}
			var count int64
			f.db.Model(&models.PlatformAlertDelivery{}).Where("destination_id=? AND status='delivered'", d.ID).Count(&count)
			return count == want
		})
	}
	deliver(1)
	if e := os.WriteFile(modeFile, []byte("healthy"), 0644); e != nil {
		t.Fatal(e)
	}
	wait("new successful HTTP window recovered the same incident", func() bool { return read().Status == "resolved" })
	deliver(2)
	var events []models.PlatformAlertEvent
	f.db.Where("incident_id=?", incident.ID).Order("occurred_at").Find(&events)
	if len(events) != 2 || events[0].Type != "opened" || events[1].Type != "resolved" || events[1].HTTPEvidence == nil || events[1].HTTPEvidence.Maximum >= 10 {
		t.Fatal("HTTP lifecycle evidence mismatch")
	}
	mu.Lock()
	got := append([]receipt(nil), receipts...)
	mu.Unlock()
	if len(got) != 3 || got[0] != got[1] || got[0].Kind != "opened" || got[2].Kind != "resolved" || got[0].Event == got[2].Event {
		t.Fatal("HTTP outbox retry identity or recovery event mismatch")
	}
	for _, received := range got {
		matched := false
		for _, event := range events {
			if received.Event == event.ID {
				var payload struct {
					Evidence models.HTTPAlertEvidence `json:"evidence"`
				}
				json.Unmarshal([]byte(received.Body), &payload)
				left, _ := json.Marshal(payload.Evidence)
				right, _ := json.Marshal(event.HTTPEvidence)
				matched = string(left) == string(right)
			}
		}
		if !matched {
			t.Fatal("delivered evidence differs from committed event")
		}
	}
	t.Log("native requests, fixed Prometheus windows, event/outbox, HTTP 503 retry and recovery passed")
}
