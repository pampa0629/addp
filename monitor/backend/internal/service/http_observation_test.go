package service

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
)

type httpBackend struct {
	processBackend
	httpCalls int
	complete  func()
	state     string
}

func (b *httpBackend) HTTPCollection(_ context.Context, _ resourcequery.ProcessScope, at time.Time, _ resourcequery.Budget) (resourcequery.Collection, error) {
	state := b.state
	if state == "" {
		state = "collecting"
	}
	return resourcequery.Collection{State: state, SampledAt: &at}, nil
}
func (b *httpBackend) HTTP(_ context.Context, p resourcequery.HTTPPlan, _ resourcequery.ProcessScope, _ resourcequery.Budget) ([]resourcequery.HTTPObservation, error) {
	b.httpCalls++
	if b.complete != nil {
		b.complete()
	}
	return []resourcequery.HTTPObservation{{Series: resourcequery.Empty(p.Plan, "no_data")}}, nil
}

func TestHTTPUsesCurrentUserSourceAndCompletionLeaseWithoutProcessQueries(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-time.Minute)
	yes := true
	users := &processUsers{rows: []client.RuntimeInstanceReference{{ID: 1, ModuleName: "gateway", InstanceID: "ingress-one", Role: "ingress", Status: "up", LeaseExpiresAt: now.Add(time.Minute), ProcessStartedAt: &start, ProcessMetricsDeclared: &yes}}}
	identities := &processIdentities{snapshot: &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now, ModuleInstances: []commonmodels.ObservabilityModuleIdentity{{ModuleName: "gateway", InstanceID: "ingress-one", Role: "ingress", LeaseExpiresAt: now.Add(time.Minute), ProcessStartedAt: &start, ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:9100/metrics"}}}}}
	backend := &httpBackend{}
	svc := NewResourceObservationService(&resourcePolicies{row: models.ResourceQueryPolicy{Version: 1, Budget: resourcequery.DefaultBudget()}}, nil, nil, nil, true, resourceSourcePolicyWithNetwork(t, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, []uint16{9100}), ProcessObservationDependencies{Users: users, Identities: identities, Backend: backend})
	svc.now = func() time.Time { return now }
	query := func() (HTTPObservationResponse, error) {
		return svc.HTTP(context.Background(), "human", "addp_at_current_user", HTTPObservationRequest{IDs: []uint{1}, WindowSeconds: 60})
	}
	users.err = &client.SystemAPIError{StatusCode: 403}
	if _, err := query(); err == nil || identities.calls != 0 || backend.httpCalls != 0 {
		t.Fatal("HTTP owner denial bypassed", err)
	}
	users.err = nil
	result, err := query()
	if err != nil || len(result.Data) != 1 || backend.calls != 0 || backend.httpCalls != 1 || result.Data[0].Subject.Role != "ingress" {
		t.Fatal(result, err)
	}
	for _, token := range users.tokens {
		if token != "addp_at_current_user" {
			t.Fatal("current user replaced")
		}
	}
	backend.state = "failed"
	result, err = query()
	if err != nil || len(result.Data[0].Observations) != 0 || backend.httpCalls != 1 {
		t.Fatal("failed collection reused values", result, err)
	}
	backend.state = ""
	trend, err := svc.HTTP(context.Background(), "human", "addp_at_current_user", HTTPObservationRequest{IDs: []uint{1}, WindowSeconds: 60, Trend: true, Start: now.Add(-5 * time.Minute), End: now.Add(-15 * time.Second)})
	if err != nil || !trend.Start.Equal(start) || len(trend.Data) != 1 || trend.Data[0].Collection.State != "collecting" || trend.Data[0].Collection.SampledAt == nil || !trend.Data[0].Collection.SampledAt.Equal(now) || len(trend.Data[0].Observations) != 1 || len(trend.Data[0].Observations[0].Series[0].Points) != 4 {
		t.Fatal("current collection evidence must preserve authorized historical HTTP points", trend, err)
	}
	backend.complete = func() { now = now.Add(61 * time.Second) }
	if result, err = query(); err == nil && len(result.Data[0].Observations) != 0 {
		t.Fatal("expired lease retained HTTP values", result)
	}
}
