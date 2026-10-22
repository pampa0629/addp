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
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/resourcequery"
)

// Native SDK counters and RSS, real Prometheus, production evaluator/outbox,
// and an independent signed TLS receiver. System identity remains a T2 fixture.
func TestIntegrationMetricsProcessAlertLifecycle(t *testing.T) {
	if os.Getenv("ADDP_PROCESS_RULE_LIFECYCLE_INTEGRATION") != "1" {
		t.Skip("requires standard metrics process alert lifecycle gate")
	}
	work := os.Getenv("METRICS_T2_WORK")
	recordFile := os.Getenv("ADDP_PROCESS_T2_RECORDS")
	if work == "" || recordFile != filepath.Join(work, "source", "process-records.json") || os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" || os.Getenv("ADDP_TEST_POSTGRES_DATABASE") != "addp_test" {
		t.Fatal("requires gate-owned native records and addp_test")
	}
	var records []struct {
		ID       string    `json:"id"`
		Module   string    `json:"module_name"`
		Instance string    `json:"instance_id"`
		Role     string    `json:"role"`
		Address  string    `json:"instance"`
		Started  time.Time `json:"started_at"`
	}
	data, err := os.ReadFile(recordFile)
	if err != nil || json.Unmarshal(data, &records) != nil || len(records) != 3 {
		t.Fatal("owned native source records", err)
	}
	r := records[2]
	if r.ID != "3" || r.Role != "worker" || records[1].Role != "worker" || records[1].Instance == r.Instance {
		t.Fatal("requires two independent same-module Workers")
	}
	host, portText, err := net.SplitHostPort(r.Address)
	if err != nil {
		t.Fatal(err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	modeFile := filepath.Join(work, "source", "process-3.json.resource-mode")
	t.Cleanup(func() { os.Remove(modeFile) })
	f := newLogRetryFixture(t, "webhook")
	backend := metricsIntegrationBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 270*time.Second)
	t.Cleanup(cancel)
	lease, declared := time.Now().UTC().Add(10*time.Minute), true
	users := &processUsers{rows: []client.RuntimeInstanceReference{{ID: 3, ModuleName: r.Module, InstanceID: r.Instance, Role: r.Role, Status: "up", LeaseExpiresAt: lease, ProcessStartedAt: &r.Started, ProcessMetricsDeclared: &declared}}}
	identities := &resourceRuleIdentity{processFacts: []commonmodels.ObservabilityModuleIdentity{{ModuleName: r.Module, InstanceID: r.Instance, Role: r.Role, LeaseExpiresAt: lease, ProcessStartedAt: &r.Started, ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://" + r.Address + "/metrics"}}}}
	observations := NewResourceObservationService(repository.NewResourceQueryPolicyRepository(f.db), nil, nil, nil, true, resourceSourcePolicyWithNetwork(t, []netip.Prefix{netip.PrefixFrom(ip, ip.BitLen())}, []uint16{uint16(port)}), ProcessObservationDependencies{Users: users, Identities: identities, Backend: backend})
	rules := NewProcessAlertRuleService(f.db, observations, identities, f.n)
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(100 * time.Second)
		for !check() {
			if time.Now().After(deadline) || ctx.Err() != nil {
				var saved []models.ProcessAlertRule
				f.db.Find(&saved)
				for _, rule := range saved {
					t.Log("rule", rule.MetricKey, rule.LastVerdict, rule.LastError, rule.LastValue)
				}
				t.Fatal(label, "timed out", ctx.Err())
			}
			time.Sleep(time.Second)
		}
		t.Log(label)
	}
	scope := resourcequery.ProcessScope{ID: 3, ModuleName: r.Module, InstanceID: r.Instance, Role: r.Role, StartedAt: r.Started, Instance: r.Address}
	baseline := map[string]float64{}
	wait("complete native idle CPU/RSS baseline", func() bool {
		rows, e := backend.ProcessSummaries(ctx, []resourcequery.ProcessScope{scope}, time.Now().UTC().Truncate(time.Second), resourcequery.DefaultBudget())
		if e != nil {
			t.Fatal(e)
		}
		baseline = map[string]float64{}
		for _, line := range rows[3].Series {
			if len(line.Points) == 1 && line.Points[0].DataState == "valid" && line.Points[0].Value != nil {
				baseline[line.MetricKey] = *line.Points[0].Value
			}
		}
		return rows[3].Collection.State == "collecting" && len(baseline) == 3
	})
	if baseline["process.cpu.core_equivalents"] >= .008 || baseline["process.memory.resident_bytes"] <= 0 {
		t.Fatal("native idle baseline exceeds fixed test policy", baseline)
	}
	t.Log("native idle baseline", baseline)
	const secret = "process-lifecycle-signing-secret"
	type receipt struct{ Delivery, Event, Kind, Body string }
	var mu sync.Mutex
	receipts := []receipt{}
	attempts := map[string]int{}
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, e := io.ReadAll(io.LimitReader(req.Body, 64<<10))
		stamp := req.Header.Get("X-ADDP-Webhook-Timestamp")
		seconds, timeErr := strconv.ParseInt(stamp, 10, 64)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stamp + "."))
		mac.Write(body)
		var payload struct {
			Schema   string                      `json:"schema"`
			Event    string                      `json:"event_id"`
			Kind     string                      `json:"event_type"`
			Subject  string                      `json:"subject_id"`
			Evidence models.ProcessAlertEvidence `json:"evidence"`
		}
		if e != nil || req.Method != "POST" || timeErr != nil || time.Since(time.Unix(seconds, 0)).Abs() > 5*time.Second || !hmac.Equal([]byte("v1="+hex.EncodeToString(mac.Sum(nil))), []byte(req.Header.Get("X-ADDP-Webhook-Signature"))) || json.Unmarshal(body, &payload) != nil || payload.Schema != "addp.platform-process-resource-alert/v1" || payload.Subject != "3" || payload.Evidence.RuntimeInstanceID != r.Instance || payload.Evidence.ProcessStartedAt != r.Started.Format(time.RFC3339Nano) || strings.Contains(string(body), "https://") {
			t.Error("invalid signed process notification")
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		receipts = append(receipts, receipt{req.Header.Get("X-ADDP-Webhook-ID"), payload.Event, payload.Kind, string(body)})
		attempts[payload.Event]++
		attempt := attempts[payload.Event]
		mu.Unlock()
		if attempt == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(202)
		}
	}))
	t.Cleanup(receiver.Close)
	sender := NewHTTPWebhookSender(2*time.Second, true)
	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())
	sender.client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
	t.Cleanup(sender.client.CloseIdleConnections)
	f.n.webhook = sender
	destInput := PlatformAlertDestinationInput{Name: "native process lifecycle", Channel: "webhook", URL: receiver.URL, Categories: []string{"process_resource"}, EventTypes: []string{"opened", "resolved"}}
	d, err := f.n.Save(ctx, 0, destInput)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.db.Where("destination_id=?", d.ID).Delete(&models.PlatformAlertDelivery{})
		f.db.Delete(&models.PlatformAlertDestination{}, d.ID)
	})
	d, err = f.n.SetSecret(ctx, d.ID, d.Version, secret)
	if err != nil {
		t.Fatal(err)
	}
	destInput.Version, destInput.Enabled = d.Version, true
	d, err = f.n.Save(ctx, d.ID, destInput)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	t.Cleanup(func() {
		incidents := f.db.Model(&models.PlatformAlertIncident{}).Select("id").Where("category='process_resource' AND signal IN ?", ids)
		events := f.db.Model(&models.PlatformAlertEvent{}).Select("id").Where("incident_id IN (?)", incidents)
		for _, result := range []error{
			f.db.Where("event_id IN (?)", events).Delete(&models.PlatformAlertDelivery{}).Error,
			f.db.Where("incident_id IN (?)", incidents).Delete(&models.PlatformAlertEvent{}).Error,
			f.db.Where("category='process_resource' AND signal IN ?", ids).Delete(&models.PlatformAlertIncident{}).Error,
			f.db.Where("id IN ?", ids).Delete(&models.ProcessAlertRule{}).Error,
		} {
			if result != nil {
				t.Error("owned process fixture cleanup", result)
			}
		}
		var count int64
		if e := f.db.Model(&models.ProcessAlertRule{}).Where("id IN ?", ids).Count(&count).Error; e != nil || count != 0 {
			t.Error("owned rule residue", count, e)
		}
	})
	for _, key := range []string{"process.cpu.core_equivalents", "process.memory.resident_bytes"} {
		trigger, recovery := .025, .008
		if key == "process.memory.resident_bytes" {
			trigger, recovery = baseline[key]+12*(1<<20), baseline[key]+8*(1<<20)
		}
		seconds, off, on := 15, false, true
		input := ProcessAlertRuleInput{Name: "native " + key, InstanceID: 3, MetricKey: key, TriggerValue: &trigger, RecoveryValue: &recovery, TriggerSeconds: &seconds, RecoverySeconds: &seconds, Enabled: &off}
		rule, e := rules.Save(ctx, "", "module-fixture-user", input, time.Now().UTC())
		if e != nil || rule.Enabled {
			t.Fatal("disabled native rule create", e)
		}
		ids = append(ids, rule.ID)
		input.Version, input.Enabled = rule.Version, &on
		if _, e = rules.Save(ctx, rule.ID, "module-fixture-user", input, time.Now().UTC()); e != nil {
			t.Fatal(e)
		}
		t.Log("fixed pre-work policy", key, trigger, recovery)
	}
	read := func() []models.PlatformAlertIncident {
		t.Helper()
		var rows []models.PlatformAlertIncident
		if e := f.db.Where("category='process_resource' AND signal IN ?", ids).Order("signal").Find(&rows).Error; e != nil {
			t.Fatal(e)
		}
		return rows
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); rules.Run(runCtx) }()
	t.Cleanup(func() { stop(); <-done })
	wait("idle native samples evaluated normal without incidents", func() bool {
		var count int64
		f.db.Model(&models.ProcessAlertRule{}).Where("id IN ? AND last_verdict='normal'", ids).Count(&count)
		return count == 2 && len(read()) == 0
	})
	started := time.Now().UTC()
	if err = os.WriteFile(modeFile, []byte("active"), 0644); err != nil {
		t.Fatal(err)
	}
	wait("real native CPU and RSS opened two isolated incidents", func() bool {
		rows := read()
		return len(rows) == 2 && rows[0].Status == "open" && rows[1].Status == "open"
	})
	opened := read()
	for _, row := range opened {
		e := row.ProcessEvidence
		if e == nil || e.AnchorInstanceID != 3 || e.RuntimeInstanceID != r.Instance || e.ProcessStartedAt != r.Started.Format(time.RFC3339Nano) || e.Minimum <= e.Policy.TriggerValue || e.SampledFrom.Before(started) {
			t.Fatal("opening lacks genuine advancing post-work samples", e)
		}
		t.Log("opened evidence", e.MetricKey, e.Minimum, e.Maximum, e.Samples, e.SampledFrom, e.SampledTo)
	}
	deliver := func(want int64) {
		wait("signed process delivery persisted", func() bool {
			if _, e := f.n.DispatchOnce(ctx, time.Now().UTC()); e != nil {
				t.Fatal(e)
			}
			var count int64
			if e := f.db.Model(&models.PlatformAlertDelivery{}).Where("destination_id=? AND status='delivered'", d.ID).Count(&count).Error; e != nil {
				t.Fatal(e)
			}
			return count == want
		})
	}
	deliver(2)
	released := time.Now().UTC()
	if err = os.Remove(modeFile); err != nil {
		t.Fatal(err)
	}
	wait("released native CPU/RSS resolved the same two incidents", func() bool {
		rows := read()
		return len(rows) == 2 && rows[0].ID == opened[0].ID && rows[1].ID == opened[1].ID && rows[0].Status == "resolved" && rows[1].Status == "resolved"
	})
	deliver(4)
	var events []models.PlatformAlertEvent
	if err = f.db.Where("incident_id IN ?", []uint{opened[0].ID, opened[1].ID}).Find(&events).Error; err != nil || len(events) != 4 {
		t.Fatal("exact opened/resolved lifecycle", err, len(events))
	}
	for _, event := range events {
		if event.ProcessEvidence == nil || (event.Type != "opened" && event.Type != "resolved") {
			t.Fatal("unexpected native lifecycle event")
		}
		if event.Type == "resolved" && (event.ProcessEvidence.Maximum >= event.ProcessEvidence.Policy.RecoveryValue || event.ProcessEvidence.SampledFrom.Before(released)) {
			t.Fatal("recovery must use new below-threshold samples")
		}
		if event.Type == "resolved" {
			e := event.ProcessEvidence
			t.Log("resolved evidence", e.MetricKey, e.Minimum, e.Maximum, e.Samples, e.SampledFrom, e.SampledTo)
		}
	}
	mu.Lock()
	got := append([]receipt(nil), receipts...)
	mu.Unlock()
	if len(got) != 8 {
		t.Fatal("each committed event must retry once", len(got))
	}
	for _, event := range events {
		matches := []receipt{}
		for _, received := range got {
			if received.Event == event.ID {
				var payload struct {
					Evidence models.ProcessAlertEvidence `json:"evidence"`
				}
				if json.Unmarshal([]byte(received.Body), &payload) != nil || !reflect.DeepEqual(&payload.Evidence, event.ProcessEvidence) || received.Kind != event.Type {
					t.Fatal("delivered evidence differs from committed process event")
				}
				matches = append(matches, received)
			}
		}
		if len(matches) != 2 || matches[0] != matches[1] {
			t.Fatal("retry changed delivery/event identity or body")
		}
	}
	var leaked int64
	if e := f.db.Model(&models.PlatformAlertDelivery{}).Where("destination_id=? AND event_id IN ?", f.target.ID, []string{events[0].ID, events[1].ID, events[2].ID, events[3].ID}).Count(&leaked).Error; e != nil || leaked != 0 {
		t.Fatal("old log subscription received process events", leaked, e)
	}
	peers := []resourcequery.ProcessScope{}
	for i, peer := range records[:2] {
		peers = append(peers, resourcequery.ProcessScope{ID: uint(i + 1), ModuleName: peer.Module, InstanceID: peer.Instance, Role: peer.Role, StartedAt: peer.Started, Instance: peer.Address})
	}
	values, err := backend.ProcessSummaries(ctx, peers, time.Now().UTC().Truncate(time.Second), resourcequery.DefaultBudget())
	if err != nil || len(values) != 2 {
		t.Fatal("peer native summaries", err)
	}
	for _, peer := range peers {
		if values[peer.ID].Collection.State != "collecting" || len(values[peer.ID].Series) != 3 {
			t.Fatal("peer lost collection during target workload")
		}
		for _, line := range values[peer.ID].Series {
			if len(line.Points) != 1 || line.Points[0].DataState != "valid" || line.Points[0].Value == nil {
				t.Fatal("peer native value was cleared")
			}
		}
	}
	t.Log("native CPU/RSS, full windows, isolated incidents, immutable event evidence and signed retries/recovery passed")
}
