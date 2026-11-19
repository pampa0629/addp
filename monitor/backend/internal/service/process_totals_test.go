package service

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
)

type totalModuleReader struct {
	tokens []string
	err    error
}

func (u *totalModuleReader) GetModulesForUser(_ context.Context, token string) ([]client.ModuleReference, error) {
	u.tokens = append(u.tokens, token)
	if u.err != nil {
		return nil, u.err
	}
	enabled := true
	return []client.ModuleReference{{ModuleName: "meta", Version: 1, Enabled: &enabled}, {ModuleName: "asset", Version: 2, Enabled: &enabled}}, nil
}

type totalMetricsReader func([]resourcequery.ProcessScope, int) (map[string]resourcequery.ProcessTotals, error)

func (f totalMetricsReader) ProcessResourceTotals(_ context.Context, scopes []resourcequery.ProcessScope, count int, _ time.Time, _ resourcequery.Budget) (map[string]resourcequery.ProcessTotals, error) {
	return f(scopes, count)
}

func TestServiceResourceTotalsTrackCurrentAuthorizedCoverage(t *testing.T) {
	now := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour)
	identities := &processIdentities{snapshot: &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now}}
	makeInstance := func(module, id, endpoint string) commonmodels.ObservabilityModuleIdentity {
		value := commonmodels.ObservabilityModuleIdentity{
			ModuleName: module, InstanceID: id, Role: "worker", LeaseExpiresAt: now.Add(time.Minute),
		}
		if endpoint != "" {
			value.ProcessStartedAt = &start
			value.ProcessMetrics = &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: endpoint}
		}
		return value
	}
	identities.snapshot.ModuleInstances = []commonmodels.ObservabilityModuleIdentity{
		makeInstance("meta", "first", "https://127.0.0.1:9100/metrics"),
		makeInstance("meta", "second", "https://127.0.0.2:9100/metrics"),
		makeInstance("asset", "pending", ""),
		makeInstance("foreign", "outside", "https://127.0.0.3:9100/metrics"),
	}
	expired := makeInstance("asset", "expired", "")
	expired.LeaseExpiresAt = now
	identities.snapshot.ModuleInstances = append(identities.snapshot.ModuleInstances, expired)
	users := &totalModuleReader{}
	stale := false
	reads := 0
	backend := totalMetricsReader(func(scopes []resourcequery.ProcessScope, modules int) (map[string]resourcequery.ProcessTotals, error) {
		reads++
		if modules != 2 || len(scopes) != 2 || scopes[0].ModuleName != "meta" || scopes[1].ModuleName != "meta" {
			t.Fatalf("scope leaked or omitted: %v", scopes)
		}
		if stale {
			identities.snapshot.ModuleInstances[1].LeaseExpiresAt = now
		}
		cpu, rss := 0.0, 512.0
		return map[string]resourcequery.ProcessTotals{"meta": {
			CPU: resourcequery.ProcessMetricTotal{Value: &cpu, Valid: 1},
			RSS: resourcequery.ProcessMetricTotal{Value: &rss, Valid: 2},
		}}, nil
	})
	policy := &resourcePolicies{row: models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget(), Version: 1}}
	s := NewResourceObservationService(policy, nil, nil, nil, true,
		resourceSourcePolicyWithNetwork(t, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, []uint16{9100}),
		ProcessObservationDependencies{Identities: identities}).WithProcessTotals(ProcessTotalDependencies{Users: users, Backend: backend})
	s.now = func() time.Time { return now }
	query := func() (ServiceResourceTotals, error) {
		return s.ProcessTotals(context.Background(), "human", "addp_at_current_user")
	}
	users.err = &client.SystemAPIError{StatusCode: 403}
	if _, err := query(); err == nil || reads != 0 || identities.calls != 0 {
		t.Fatal("unauthorized observation", err)
	}
	users.err = nil
	result, err := query()
	if err != nil || result.Total.Expected != 3 || len(result.Modules) != 2 || result.Modules[0].ModuleName != "asset" ||
		result.Modules[0].CPU.Value != nil || result.Total.CPU.Value == nil || *result.Total.CPU.Value != 0 ||
		result.Total.CPU.Valid != 1 || result.Total.RSS.Valid != 2 || result.Total.RSS.Value == nil || *result.Total.RSS.Value != 512 {
		t.Fatalf("incorrect totals %+v, %v", result, err)
	}
	stale = true
	if _, err := query(); !errors.Is(err, metricsdiscovery.ErrIdentityUnavailable) {
		t.Fatal("published a total after membership changed", err)
	}
	for _, token := range users.tokens {
		if token != "addp_at_current_user" {
			t.Fatal("owner read lost the current User")
		}
	}
}
