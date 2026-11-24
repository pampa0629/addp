package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/resourcequery"
)

func TestIntegrationMetricsAutomaticCollection(t *testing.T) {
	if os.Getenv("ADDP_PROCESS_RULE_LIFECYCLE_INTEGRATION") != "1" {
		t.Skip("requires standard monitor metrics process gate")
	}
	work := os.Getenv("METRICS_T2_WORK")
	owned := filepath.Join(work, "source", "process-records.json")
	if work == "" || owned != os.Getenv("ADDP_PROCESS_T2_RECORDS") || os.Getenv("ADDP_TEST_POSTGRES_DATABASE") != "addp_test" {
		t.Fatal("requires gate-owned source and database")
	}
	fixture := newLogRetryFixture(t, "webhook")
	var records []struct {
		Module   string    `json:"module_name"`
		Instance string    `json:"instance_id"`
		Role     string    `json:"role"`
		Address  string    `json:"instance"`
		Started  time.Time `json:"started_at"`
	}
	data, err := os.ReadFile(owned)
	if err != nil || json.Unmarshal(data, &records) != nil || len(records) != 3 {
		t.Fatal("invalid native records", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 160*time.Second)
	defer cancel()
	purposes := []string{"", "sync", "continuous_sync"}
	identities := &resourceRuleIdentity{disabled: true}
	var networks []netip.Prefix
	seenNetwork := map[netip.Prefix]bool{}
	var ports []uint16
	for i, record := range records {
		host, portText, err := net.SplitHostPort(record.Address)
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
		network := netip.PrefixFrom(ip, ip.BitLen())
		if !seenNetwork[network] {
			networks = append(networks, network)
			seenNetwork[network] = true
		}
		ports = append(ports, uint16(port))
		identities.processFacts = append(identities.processFacts, commonmodels.ObservabilityModuleIdentity{
			ModuleName: record.Module, Role: record.Role, RuntimeName: purposes[i], InstanceID: record.Instance,
			ProcessStartedAt: &record.Started, LeaseExpiresAt: time.Now().Add(4 * time.Minute),
			ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://" + record.Address + "/metrics"},
		})
	}
	ca, err := os.ReadFile(filepath.Join(work, "source", "source-ca.crt"))
	roots := x509.NewCertPool()
	if err != nil || !roots.AppendCertsFromPEM(ca) {
		t.Fatal("fixture source CA", err)
	}
	admission, err := tls.LoadX509KeyPair(filepath.Join(work, "admission.crt"), filepath.Join(work, "admission.key"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := metricsdiscovery.NewSourcePolicy(networks, ports, roots, admission, metricsdiscovery.DefaultExternalTargetLimit)
	if err != nil {
		t.Fatal(err)
	}
	store := repository.NewAutomaticCollectionRepository(fixture.db)
	backend := metricsIntegrationBackend(t)
	svc := NewMonitoringTargetService(&targetTestStore{}, &targetTestNodes{}, identities, true, policy).WithAutomaticCollection(store, backend)
	ordinary := metricsdiscovery.ProcessCollectionScope(identities.processFacts[1]).ID
	cdc := metricsdiscovery.ProcessCollectionScope(identities.processFacts[2]).ID
	t.Cleanup(func() {
		if err := fixture.db.Where("id = ?", ordinary).Delete(&models.AutomaticCollectionScope{}).Error; err != nil {
			t.Error(err)
		}
	})
	sourceFile := filepath.Join(work, "source", "discovery.json")
	original, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(body []byte) {
		t.Helper()
		pending := filepath.Join(work, "source", "discovery.automatic.pending")
		if err := os.WriteFile(pending, body, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(pending, sourceFile); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { publish(original) })
	project := func(expected int) {
		t.Helper()
		groups, err := svc.Discover(ctx)
		if err != nil || len(groups) != expected {
			t.Fatalf("authenticated discovery: expected %d, got %d, err=%v", expected, len(groups), err)
		}
		encoded, err := json.Marshal(groups)
		if err != nil {
			t.Fatal(err)
		}
		publish(encoded)
	}
	wait := func(want string) {
		t.Helper()
		deadline := time.Now().Add(50 * time.Second)
		for {
			response, err := svc.AutomaticScopes(ctx, "fixture-user")
			if err != nil {
				t.Fatal(err)
			}
			states := map[string]string{}
			for _, row := range response.Data {
				states[row.ID] = row.Observation
			}
			if states[ordinary] == want && states[cdc] == "collecting" {
				t.Log("ordinary Worker:", want, "; CDC Worker: collecting")
				return
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				t.Fatalf("want ordinary=%s, cdc=collecting, got %s/%s", want, states[ordinary], states[cdc])
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
			}
		}
	}
	project(3)
	wait("collecting")
	sample := func(at time.Time) string {
		t.Helper()
		r := records[1]
		scope := resourcequery.ProcessScope{ID: 2, ModuleName: r.Module, InstanceID: r.Instance, Role: r.Role, Instance: r.Address, StartedAt: r.Started}
		rows, err := backend.ProcessSummaries(ctx, []resourcequery.ProcessScope{scope}, at, resourcequery.DefaultBudget(), false)
		if err != nil {
			t.Fatal(err)
		}
		return rows[2].Collection.State
	}
	before := time.Now().UTC().Truncate(time.Second)
	if got := sample(before); got != "collecting" {
		t.Fatal("pre-pause sample", got)
	}
	enabled := false
	row, err := svc.SaveAutomaticScope(ctx, "fixture-user", ordinary, AutomaticCollectionInput{Enabled: &enabled, Version: 1})
	if err != nil || row.Enabled || row.Version != 2 {
		t.Fatal("pause intent", row, err)
	}
	wait("stopping") // Prometheus still holds the previous successful projection.
	project(2)
	wait("stopped")
	if got := sample(before); got != "collecting" {
		t.Fatal("pause erased retained history", got)
	}
	enabled = true
	row, err = svc.SaveAutomaticScope(ctx, "fixture-user", ordinary, AutomaticCollectionInput{Enabled: &enabled, Version: 2})
	if err != nil || !row.Enabled || row.Version != 3 {
		t.Fatal("resume intent", row, err)
	}
	wait("waiting") // success requires a new scrape after the saved resume.
	project(3)
	wait("collecting")
}
