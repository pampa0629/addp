package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/google/uuid"
)

// The metrics gate owns PostgreSQL, the controlled source and real Prometheus.
// Run and the HTTP sender are production implementations; only System identity
// reads remain module fixtures. This is T2, not a full authenticated deployment.
func TestIntegrationMetricsResourceAlertLifecycle(t *testing.T) {
	testMetricsResourceAlertLifecycle(t, false)
}
func TestIntegrationMetricsFilesystemAlertLifecycle(t *testing.T) {
	testMetricsResourceAlertLifecycle(t, true)
}

func testMetricsResourceAlertLifecycle(t *testing.T, filesystem bool) {
	if os.Getenv("ADDP_RESOURCE_RULE_LIFECYCLE_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics lifecycle T2 gate")
	}
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" || os.Getenv("ADDP_TEST_POSTGRES_DATABASE") != "addp_test" {
		t.Fatal("requires the gate-owned addp_test database")
	}
	sourceFile := os.Getenv("ADDP_RESOURCE_RULE_SOURCE_FILE")
	work := os.Getenv("METRICS_T2_WORK")
	if work == "" || sourceFile != filepath.Join(work, "source", "overview-0.prom") {
		t.Fatal("source file must belong to this gate")
	}
	original, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatal(err)
	}
	writeSource := func(available string) {
		t.Helper()
		data := "node_uname_info{sysname=\"Linux\"} 1\nnode_exporter_build_info{version=\"1.12.1\"} 1\nnode_boot_time_seconds 1\nnode_memory_MemTotal_bytes 100000000\n"
		if available != "" {
			data += "node_memory_MemAvailable_bytes " + available + "\n"
		}
		if filesystem {
			data += "node_scrape_collector_success{collector=\"filesystem\"} 1\n"
			if available != "" {
				labels := `{device="/dev/data",mountpoint="/data",fstype="ext4"}`
				data += "node_filesystem_device_error" + labels + " 0\nnode_filesystem_size_bytes" + labels + " 100000000\nnode_filesystem_free_bytes" + labels + " " + available + "\nnode_filesystem_avail_bytes" + labels + " " + available + "\n"
			}
		}
		file, e := os.CreateTemp(filepath.Dir(sourceFile), ".rule-source-")
		if e != nil {
			t.Fatal(e)
		}
		temp := file.Name()
		defer os.Remove(temp)
		if _, e = file.WriteString(data); e != nil {
			file.Close()
			t.Fatal(e)
		}
		if e = file.Chmod(0644); e != nil {
			file.Close()
			t.Fatal(e)
		}
		if e = file.Close(); e != nil {
			t.Fatal(e)
		}
		if e = os.Rename(temp, sourceFile); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		if e := os.WriteFile(sourceFile, original, 0644); e != nil {
			t.Error(e)
		}
	})

	const secret = "metrics-lifecycle-fixture-secret"
	type receipt struct {
		delivery, event, kind string
		incident              uint
		evidence              models.ResourceAlertEvidence
	}
	var mu sync.Mutex
	var receipts []receipt
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		timestamp := r.Header.Get("X-ADDP-Webhook-Timestamp")
		seconds, timeErr := strconv.ParseInt(timestamp, 10, 64)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(timestamp + "."))
		mac.Write(body)
		signature := "v1=" + hex.EncodeToString(mac.Sum(nil))
		var payload struct {
			Schema   string                       `json:"schema"`
			Event    string                       `json:"event_id"`
			Kind     string                       `json:"event_type"`
			Incident uint                         `json:"incident_id"`
			Subject  string                       `json:"subject_id"`
			Evidence models.ResourceAlertEvidence `json:"evidence"`
		}
		if r.Method != http.MethodPost || e != nil || timeErr != nil || time.Since(time.Unix(seconds, 0)).Abs() > 5*time.Second || !hmac.Equal([]byte(signature), []byte(r.Header.Get("X-ADDP-Webhook-Signature"))) || json.Unmarshal(body, &payload) != nil || payload.Schema != "addp.platform-resource-alert/v1" || payload.Subject != os.Getenv("ADDP_METRICS_QUERY_NODE_ID") || strings.Contains(string(body), "https://") {
			t.Error("invalid signed resource notification")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		receipts = append(receipts, receipt{r.Header.Get("X-ADDP-Webhook-ID"), payload.Event, payload.Kind, payload.Incident, payload.Evidence})
		attempt := len(receipts)
		mu.Unlock()
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(receiver.Close)
	f := newLogRetryFixture(t, "webhook")
	sender := NewHTTPWebhookSender(2*time.Second, true)
	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())
	sender.client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
	t.Cleanup(sender.client.CloseIdleConnections)
	f.n.webhook = sender

	node, instance := os.Getenv("ADDP_METRICS_QUERY_NODE_ID"), os.Getenv("ADDP_METRICS_QUERY_INSTANCE")
	host, portText, err := net.SplitHostPort(instance)
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
	target := models.MonitoringTarget{ID: uuid.NewString(), NodeID: node, MonitorKind: "host_resources", SourceType: "node_exporter", Endpoint: "https://" + instance + "/metrics", Enabled: true, Version: 1}
	if err = f.db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, query := range []string{
			`DELETE FROM monitor.platform_alert_deliveries WHERE event_id IN (SELECT id FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?))`,
			`DELETE FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?)`,
			`DELETE FROM monitor.platform_alert_incidents WHERE subject_id=?`,
			`DELETE FROM monitor.resource_alert_rules WHERE node_id=?`,
			`DELETE FROM monitor.monitoring_targets WHERE node_id=?`,
		} {
			if e := f.db.Exec(query, node).Error; e != nil {
				t.Error(e)
			}
		}
		for _, model := range []any{&models.ResourceAlertRule{}, &models.MonitoringTarget{}} {
			var count int64
			if e := f.db.Model(model).Where("node_id=?", node).Count(&count).Error; e != nil || count != 0 {
				t.Errorf("rule/source cleanup count=%d err=%v", count, e)
			}
		}
	})
	ctx := context.Background()
	destination, err := f.n.Save(ctx, 0, PlatformAlertDestinationInput{Name: "metrics-" + uuid.NewString(), Channel: "webhook", URL: receiver.URL, Categories: []string{"resource"}, EventTypes: []string{"opened", "resolved"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := f.db.Where("destination_id=?", destination.ID).Delete(&models.PlatformAlertDelivery{}).Error; e != nil {
			t.Error(e)
		}
		if e := f.db.Delete(&models.PlatformAlertDestination{}, destination.ID).Error; e != nil {
			t.Error(e)
		}
	})
	destination, err = f.n.SetSecret(ctx, destination.ID, destination.Version, secret)
	if err != nil {
		t.Fatal(err)
	}
	destination, err = f.n.Save(ctx, destination.ID, PlatformAlertDestinationInput{Version: destination.Version, Name: destination.Name, Channel: "webhook", URL: receiver.URL, Categories: []string{"resource"}, EventTypes: []string{"opened", "resolved"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	backend := metricsIntegrationBackend(t)
	observations := NewResourceObservationService(repository.NewResourceQueryPolicyRepository(f.db), repository.NewMonitoringTargetRepository(f.db), &resourceNodes{enabled: true}, backend, true, resourceSourcePolicyWithNetwork(t, []netip.Prefix{netip.PrefixFrom(ip, ip.BitLen())}, []uint16{uint16(port)}), ProcessObservationDependencies{})
	rules := NewResourceAlertRuleService(f.db, observations, &resourceRuleIdentity{node: node, version: 3}, f.n)
	policy := models.ResourcePressurePolicy{TriggerPercent: 80, RecoveryPercent: 75, TriggerSeconds: 30, RecoverySeconds: 30}
	enabled := false
	input := ResourceAlertRuleInput{Name: "metrics-lifecycle", NodeID: node, MetricKey: "node.memory.used_percent", TriggerPercent: &policy.TriggerPercent, RecoveryPercent: &policy.RecoveryPercent, TriggerSeconds: &policy.TriggerSeconds, RecoverySeconds: &policy.RecoverySeconds, Enabled: &enabled}
	if filesystem {
		input.MetricKey = models.FilesystemCapacityMetric
		input.Dimensions = testFilesystemMount.Copy()
		policy.TriggerPercent, policy.RecoveryPercent = 85, 80
		writeSource("10000000")
		ready := false
		deadline := time.Now().Add(35 * time.Second)
		for time.Now().Before(deadline) {
			result, e := observations.Query(ctx, "fixture-admission", node, "module-fixture-user", []string{input.MetricKey}, time.Time{}, time.Time{}, false, input.Dimensions)
			if e == nil && result.Collection != nil && result.Collection.State == "collecting" && result.Collection.Filesystem == "available" && len(result.Series) == 1 && len(result.Series[0].Points) == 1 && validPressurePoint(result.Series[0].Points[0]) {
				ready = true
				break
			}
			time.Sleep(time.Second)
		}
		if !ready {
			t.Fatal("filesystem source did not publish current exact mount")
		}
	}
	rule, err := rules.Save(ctx, "", "module-fixture-user", input, time.Now().UTC())
	if err != nil || rule.Enabled {
		t.Fatal("create disabled rule", err)
	}
	writeSource("10000000") // 90% used, without allocating host memory.
	enabled = true
	input.Version = rule.Version
	rule, err = rules.Save(ctx, rule.ID, "module-fixture-user", input, time.Now().UTC())
	if err != nil || !rule.Enabled {
		t.Fatal("enable rule", err)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); rules.Run(runCtx) }()
	t.Cleanup(func() { stop(); <-done })
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(80 * time.Second)
		for !check() {
			if time.Now().After(deadline) {
				var state models.ResourceAlertRule
				f.db.First(&state, "id=?", rule.ID)
				t.Fatalf("timeout %s: verdict=%s reason=%s", label, state.LastVerdict, state.LastError)
			}
			time.Sleep(time.Second)
		}
		t.Log(label)
	}
	readIncident := func() models.PlatformAlertIncident {
		t.Helper()
		var rows []models.PlatformAlertIncident
		if e := f.db.Where("subject_id=?", node).Find(&rows).Error; e != nil || len(rows) > 1 {
			t.Fatal("incident read or duplicate", e)
		}
		if len(rows) == 0 {
			return models.PlatformAlertIncident{}
		}
		return rows[0]
	}
	wait("real high samples opened one incident", func() bool { return readIncident().Status == "open" })
	incident := readIncident()
	if incident.ResourceEvidence == nil || incident.ResourceEvidence.RuleID != rule.ID || incident.ResourceEvidence.RuleVersion != rule.Version || incident.ResourceEvidence.Policy != policy || incident.ResourceEvidence.Minimum <= 80 || incident.ResourceEvidence.SampledTo.Sub(incident.ResourceEvidence.SampledFrom) < 30*time.Second {
		t.Fatal("opening lacks real immutable rule evidence")
	}
	deliver := func(kind string) {
		t.Helper()
		wait("signed "+kind+" delivery received and persisted", func() bool {
			if _, e := f.n.DispatchOnce(ctx, time.Now().UTC()); e != nil {
				t.Fatal(e)
			}
			var count int64
			if e := f.db.Model(&models.PlatformAlertDelivery{}).Where("destination_id=? AND status='delivered'", destination.ID).Count(&count).Error; e != nil {
				t.Fatal(e)
			}
			want := int64(1)
			if kind == "resolved" {
				want = 2
			}
			return count == want
		})
	}
	deliver("opened") // Real HTTP 503, followed by the ordinary retry cycle.
	writeSource("")
	wait("missing source samples are unknown", func() bool {
		var state models.ResourceAlertRule
		if e := f.db.First(&state, "id=?", rule.ID).Error; e != nil {
			t.Fatal(e)
		}
		return state.LastError == "no_data" || state.LastError == "mount_missing"
	})
	for until := time.Now().Add(31 * time.Second); time.Now().Before(until); time.Sleep(time.Second) {
		if readIncident().Status != "open" {
			t.Fatal("missing samples recovered an active incident")
		}
	}
	t.Log("missing samples longer than the recovery period did not resolve")
	writeSource("90000000") // 10% used; recover with the original policy.
	wait("real low samples resolved the same incident", func() bool { return readIncident().Status == "resolved" })
	deliver("resolved")
	if processed, e := f.n.DispatchOnce(ctx, time.Now().UTC()); e != nil || processed {
		t.Fatal("completed notification resent", e)
	}
	var events []models.PlatformAlertEvent
	if e := f.db.Where("incident_id=?", incident.ID).Order("occurred_at").Find(&events).Error; e != nil || len(events) != 2 || events[0].Type != "opened" || events[1].Type != "resolved" {
		t.Fatal("lifecycle identity mismatch", e)
	}
	var deliveries []models.PlatformAlertDelivery
	if e := f.db.Where("event_id IN ?", []string{events[0].ID, events[1].ID}).Find(&deliveries).Error; e != nil || len(deliveries) != 2 {
		t.Fatal("resource routed to a log subscriber", e)
	}
	for _, d := range deliveries {
		if d.DestinationID != destination.ID || d.Status != "delivered" || d.SecretCiphertext != "" || d.ClaimID != "" {
			t.Fatal("delivery persisted incorrectly")
		}
	}
	mu.Lock()
	got := append([]receipt(nil), receipts...)
	mu.Unlock()
	if len(got) != 3 || !reflect.DeepEqual(got[0], got[1]) || got[0].kind != "opened" || got[2].kind != "resolved" || got[0].delivery == got[2].delivery || got[0].event == got[2].event {
		t.Fatal("retry or recovery delivery identity changed")
	}
	for _, r := range got {
		if r.incident != incident.ID || r.evidence.RuleID != rule.ID || r.evidence.RuleVersion != rule.Version || r.evidence.Policy != policy || r.evidence.Samples < 3 {
			t.Fatal("HTTP delivery lost original evidence")
		}
		index := 0
		if r.kind == "resolved" {
			index = 1
		}
		if r.event != events[index].ID || events[index].ResourceEvidence == nil || !reflect.DeepEqual(r.evidence, *events[index].ResourceEvidence) {
			t.Fatal("HTTP event identity or evidence differs from committed event")
		}
		matched := false
		for _, d := range deliveries {
			matched = matched || d.ID == r.delivery && d.EventID == r.event
		}
		if !matched {
			t.Fatal("HTTP delivery identity differs from committed outbox")
		}
	}
	if got[2].evidence.Maximum >= policy.RecoveryPercent || readIncident().ResourceEvidence.Latest != incident.ResourceEvidence.Latest {
		t.Fatal("recovery lost low window or overwrote opening evidence")
	}
	t.Log(fmt.Sprintf("lifecycle complete: events=%d delivered=%d HTTP attempts=%d", len(events), len(deliveries), len(got)))
}
