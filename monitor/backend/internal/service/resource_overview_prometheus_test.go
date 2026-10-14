package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"net/http/httptrace"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
)

// The standard T2 gate owns these controlled inputs and the real Prometheus.
// In-memory Owner facts do not stand in for T4 User authorization or real hosts.
func TestIntegrationMetricsResourceOverview(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	read := func(path string) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	queryCert, err := tls.X509KeyPair(read(os.Getenv("MONITOR_PROMETHEUS_CLIENT_CERT_FILE")), read(os.Getenv("MONITOR_PROMETHEUS_CLIENT_KEY_FILE")))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := resourcequery.NewClient(os.Getenv("MONITOR_PROMETHEUS_URL"), read(os.Getenv("MONITOR_PROMETHEUS_CA_FILE")), queryCert)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	work := os.Getenv("METRICS_T2_WORK")
	admission, err := tls.X509KeyPair(read(filepath.Join(work, "admission.crt")), read(filepath.Join(work, "admission.key")))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(read(filepath.Join(os.Getenv("ADDP_METRICS_DEPLOYMENT_DIR"), "source-ca.crt"))) {
		t.Fatal("invalid owned source CA")
	}
	ip, err := netip.ParseAddr(os.Getenv("ADDP_METRICS_OVERVIEW_SOURCE_IP"))
	if err != nil || !ip.Is4() || !ip.IsPrivate() {
		t.Fatal("requires owned private IPv4 source")
	}
	ports := []uint16{19450, 19451, 19452, 19453, 19454, 19455}
	policy, err := metricsdiscovery.NewSourcePolicy([]netip.Prefix{netip.PrefixFrom(ip, 32)}, ports, roots, admission)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _ := overviewFixture(6)
	nodes := &overviewNodes{resourceNodes: resourceNodes{enabled: true}, rows: rows}
	targets := &targetTestStore{}
	for i, node := range rows {
		targets.rows = append(targets.rows, metricsdiscovery.NodeTarget{
			ID: uuid.NewString(), Version: 1, Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: node.NodeID},
			MonitorKind: "host_resources", Enabled: true,
			Source: metricsdiscovery.NodeSource{Type: "node_exporter", Endpoint: fmt.Sprintf("https://%s:%d/metrics", ip, ports[i])},
		})
	}
	policies := &resourcePolicies{row: models.ResourceQueryPolicy{Version: 2, Budget: resourcequery.DefaultBudget()}}
	svc := NewResourceObservationService(policies, targets, nodes, backend, true, policy, ProcessObservationDependencies{})
	var requests atomic.Int32
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			requests.Add(1)
		}
	}})
	observe := func() ResourceOverviewResponse {
		t.Helper()
		requests.Store(0)
		result, err := svc.Overview(ctx, "t2-user", "addp_at_current_user")
		if err != nil || requests.Load() != 2 {
			t.Fatalf("real summary batch: error=%v requests=%d", err, requests.Load())
		}
		return result
	}
	wait := func(limit time.Duration, ready func(ResourceOverviewResponse) bool) ResourceOverviewResponse {
		t.Helper()
		deadline := time.Now().Add(limit)
		for {
			result := observe()
			if ready(result) {
				return result
			}
			if time.Now().After(deadline) {
				t.Fatalf("native overview readiness: %+v", result)
			}
			time.Sleep(time.Second)
		}
	}
	assertRanks := func(rank ResourceRanking, indices []int, values []float64) {
		t.Helper()
		if len(rank.Top) != len(indices) {
			t.Fatalf("%s top cardinality: %+v", rank.MetricKey, rank)
		}
		for i, index := range indices {
			if rank.Top[i].Node.NodeID != rows[index].NodeID || math.Abs(rank.Top[i].Value-values[i]) > 1e-8 || rank.Top[i].SampledAt.IsZero() {
				t.Fatalf("%s rank %d identity/value/sample: %+v", rank.MetricKey, i, rank.Top[i])
			}
		}
	}
	initial := wait(100*time.Second, func(result ResourceOverviewResponse) bool {
		return result.Rankings[0].Coverage.Valid == 6 && result.Rankings[1].Coverage.Valid == 6
	})
	complete := ResourceCoverage{Expected: 6, Valid: 6, Complete: true}
	if initial.Inventory.Managed != 6 || initial.Inventory.Enabled != 6 || initial.Inventory.Connected != 6 || initial.Inventory.Collection["collecting"] != 6 || initial.Rankings[0].Coverage != complete || initial.Rankings[1].Coverage != complete {
		t.Fatalf("complete source identity/coverage: %+v", initial)
	}
	assertRanks(initial.Rankings[0], []int{0, 1, 2, 3, 4}, []float64{100, 100, 100, 100, 100})
	assertRanks(initial.Rankings[1], []int{5, 3, 4, 2, 1}, []float64{90, 60, 60, 20, 10})
	t.Log("six current source identities: two real batch requests, full-window CPU ties and last-input memory maximum")

	path := filepath.Join(work, "source", "overview-5.prom")
	original := read(path)
	write := func(data []byte) {
		t.Helper()
		if err := os.WriteFile(path+".next", data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".next", path); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { write(original) })
	write([]byte(strings.ReplaceAll(string(original), "node_memory_MemAvailable_bytes 10000000\n", "")))
	missing := wait(40*time.Second, func(result ResourceOverviewResponse) bool {
		return result.Rankings[0].Coverage == complete && result.Rankings[1].Coverage == (ResourceCoverage{Expected: 6, Valid: 5, NoData: 1})
	})
	if missing.Inventory.Collection["collecting"] != 6 {
		t.Fatal("one missing metric incorrectly disconnected the source", missing.Inventory)
	}
	assertRanks(missing.Rankings[1], []int{3, 4, 2, 1, 0}, []float64{60, 60, 20, 10, 0})
	// The source remains scraped: current Owner target intent must exclude its history.
	write(original)
	wait(40*time.Second, func(result ResourceOverviewResponse) bool {
		return result.Rankings[0].Coverage == complete && result.Rankings[1].Coverage == complete
	})
	targets.rows[5].Enabled = false
	targets.rows[5].Version++
	disabled := observe()
	coverage := ResourceCoverage{Expected: 6, Valid: 5, NotConnected: 1}
	if disabled.Inventory.Managed != 6 || disabled.Inventory.Enabled != 6 || disabled.Inventory.Connected != 5 || disabled.Inventory.NotConnected != 1 || disabled.Inventory.Collection["collecting"] != 5 || disabled.Inventory.Collection["not_connected"] != 1 || disabled.Rankings[0].Coverage != coverage || disabled.Rankings[1].Coverage != coverage {
		t.Fatalf("disabled target reused native history: %+v", disabled)
	}
	assertRanks(disabled.Rankings[0], []int{0, 1, 2, 3, 4}, []float64{100, 100, 100, 100, 100})
	assertRanks(disabled.Rankings[1], []int{3, 4, 2, 1, 0}, []float64{60, 60, 20, 10, 0})
	policies.row.MaxSeries = 10
	policies.row.Version++
	requests.Store(0)
	refused, err := svc.Overview(ctx, "t2-user", "addp_at_current_user")
	if !errors.Is(err, resourcequery.ErrBudget) || requests.Load() != 0 || !reflect.DeepEqual(refused, ResourceOverviewResponse{}) {
		t.Fatalf("whole inventory budget before queries: err=%v requests=%d result=%+v", err, requests.Load(), refused)
	}
	t.Log("independent missing-metric coverage, valid zero, current target exclusion, and whole-scope pre-query budget passed")
}
