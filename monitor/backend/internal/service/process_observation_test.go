package service

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
)

type processUsers struct {
	rows   []client.RuntimeInstanceReference
	err    error
	tokens []string
}

func (u *processUsers) GetRuntimeInstancesForUser(_ context.Context, _ []uint, token string) ([]client.RuntimeInstanceReference, error) {
	u.tokens = append(u.tokens, token)
	return u.rows, u.err
}

type processIdentities struct {
	snapshot *commonmodels.ObservabilityIdentitySnapshot
	calls    int
}

func (i *processIdentities) GetObservabilityIdentities(context.Context) (*commonmodels.ObservabilityIdentitySnapshot, error) {
	i.calls++
	return i.snapshot, nil
}

type processBackend struct {
	calls    int
	scopes   []resourcequery.ProcessScope
	complete func()
	omit     bool
}

func (b *processBackend) ProcessSummaries(_ context.Context, scopes []resourcequery.ProcessScope, at time.Time, budget resourcequery.Budget) (map[uint]resourcequery.Summary, error) {
	b.calls++
	b.scopes = append([]resourcequery.ProcessScope(nil), scopes...)
	if b.complete != nil {
		b.complete()
	}
	p, err := resourcequery.NewProcessSummaryPlan(len(scopes), at, budget)
	if err != nil {
		return nil, err
	}
	result := map[uint]resourcequery.Summary{}
	if b.omit {
		return result, nil
	}
	for _, s := range scopes {
		series := resourcequery.Empty(p, "no_data")
		for index := range series {
			value := float64(s.ID)
			if index == 0 {
				value = 0
			}
			series[index].Points[0].Value = &value
			series[index].Points[0].DataState = "valid"
			series[index].Points[0].SampledAt = &at
		}
		result[s.ID] = resourcequery.Summary{Series: series, Collection: resourcequery.Collection{State: "collecting", SampledAt: &at}}
	}
	return result, nil
}

func TestProcessSummaryAuthorizesBeforePrivateReadsAndNeedsNoHost(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-time.Minute)
	yes, no := true, false
	users := &processUsers{}
	identities := &processIdentities{snapshot: &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now, ModuleInstances: []commonmodels.ObservabilityModuleIdentity{}}}
	backend := &processBackend{}
	for index, endpoint := range []string{"https://127.0.0.1:9100/metrics", "https://127.0.0.2:9100/metrics"} {
		id := uint(index + 1)
		instance := []string{"native-a", "native-b"}[index]
		users.rows = append(users.rows, client.RuntimeInstanceReference{ID: id, ModuleName: "monitor", InstanceID: instance, Role: "worker", Status: "up", LeaseExpiresAt: now.Add(time.Minute), ProcessStartedAt: &start, ProcessMetricsDeclared: &yes})
		identities.snapshot.ModuleInstances = append(identities.snapshot.ModuleInstances, commonmodels.ObservabilityModuleIdentity{ModuleName: "monitor", InstanceID: instance, Role: "worker", LeaseExpiresAt: now.Add(time.Minute), ProcessStartedAt: &start, ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: endpoint}})
	}
	policies := &resourcePolicies{row: models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget(), Version: 2}}
	svc := NewResourceObservationService(policies, nil, nil, nil, true, resourceSourcePolicyWithNetwork(t, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, []uint16{9100}), ProcessObservationDependencies{Users: users, Identities: identities, Backend: backend})
	svc.now = func() time.Time { return now }
	query := func() (ProcessResourceSummaryResponse, error) {
		return svc.ProcessSummaries(context.Background(), "human", "addp_at_current_user", []uint{1, 2})
	}
	users.err = &client.SystemAPIError{StatusCode: 403}
	if _, err := query(); err == nil || identities.calls != 0 || backend.calls != 0 {
		t.Fatal("unauthorized private read", err)
	}
	users.err = nil
	result, err := query()
	if err != nil || len(result.Data) != 2 || backend.calls != 1 || len(backend.scopes) != 2 || result.Data[0].NodeID != "" || result.Data[0].Series[0].Points[0].Value == nil || *result.Data[0].Series[0].Points[0].Value != 0 {
		t.Fatalf("native resources %+v %v", result, err)
	}
	for _, token := range users.tokens {
		if token != "addp_at_current_user" {
			t.Fatal("user replaced")
		}
	}
	users.rows[0].Status = "down"
	users.rows[1].ProcessMetricsDeclared = &no
	result, err = query()
	if err != nil || backend.calls != 1 || result.Data[0].Collection.State != "not_active" || result.Data[1].Collection.State != "not_connected" {
		t.Fatal("offline/unconnected history reused", result, err)
	}
	users.rows[0].Status = "up"
	users.rows[1].ProcessMetricsDeclared = &yes
	policies.row.MaxSeries = 5
	if _, err = query(); !errors.Is(err, resourcequery.ErrBudget) || backend.calls != 1 {
		t.Fatal("joint budget bypass", err)
	}
	policies.row.MaxSeries = 100
	backend.omit = true
	if _, err = query(); !errors.Is(err, resourcequery.ErrUnavailable) {
		t.Fatal("partial backend accepted", err)
	}
	backend.omit = false
	policies.row.SubjectConcurrency = 1
	lease, err := svc.limiter.Acquire("human", resourcequery.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = query(); !errors.Is(err, resourcequery.ErrBusy) {
		t.Fatal("shared limiter bypass", err)
	}
	lease()
	backend.complete = func() { now = now.Add(61 * time.Second) }
	if _, err = query(); !errors.Is(err, resourcequery.ErrUnavailable) {
		t.Fatal("stale identity snapshot accepted", err)
	}
}
