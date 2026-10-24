package service

import (
	"context"
	"errors"
	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type containerBackend struct {
	processBackend
	scope resourcequery.ProcessScope
	after func()
}

func (b *containerBackend) ContainerObservation(_ context.Context, scope resourcequery.ProcessScope, at time.Time, budget resourcequery.Budget) (resourcequery.Summary, error) {
	b.scope = scope
	b.calls++
	if b.after != nil {
		b.after()
	}
	plan, _ := resourcequery.NewContainerPlan(1, time.Time{}, time.Time{}, at, false, budget)
	return resourcequery.Summary{Series: resourcequery.Empty(plan, "no_data"), Collection: resourcequery.Collection{State: "collecting"}}, nil
}

func TestContainerReadAuthorizesCurrentUserAndRechecksProof(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	started := now.Add(-time.Hour)
	yes := true
	cpu := .5
	cg := commonmodels.CgroupIdentity{BootID: "11111111-1111-4111-8111-111111111111", CgroupID: "100", PIDNamespaceID: "200"}
	proof := &commonmodels.ContainerProof{Identity: cg, DockerEngineID: "engine", ContainerID: strings.Repeat("a", 64), ModuleName: "monitor", Role: "worker", StartedAt: now.Add(-2 * time.Hour), ObservedAt: now, StableSince: now.Add(-2 * time.Minute), HierarchyDigest: strings.Repeat("b", 64), CPUCores: &cpu}
	identities := &processIdentities{snapshot: &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now, ModuleInstances: []commonmodels.ObservabilityModuleIdentity{{ModuleName: "monitor", InstanceID: "actual", Role: "worker", LeaseExpiresAt: now.Add(time.Minute), ProcessStartedAt: &started, ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:9100/metrics", Cgroup: &cg}, Container: proof}}}}
	users := &processUsers{rows: []client.RuntimeInstanceReference{{ID: 7, ModuleName: "monitor", InstanceID: "actual", Role: "worker", Status: "up", LeaseExpiresAt: now.Add(time.Minute), ProcessStartedAt: &started, ProcessMetricsDeclared: &yes}}}
	backend := &containerBackend{}
	policies := &resourcePolicies{row: models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget(), Version: 2}}
	svc := NewResourceObservationService(policies, nil, nil, nil, true, resourceSourcePolicyWithNetwork(t, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, []uint16{9100}), ProcessObservationDependencies{Users: users, Identities: identities, Backend: backend})
	svc.now = func() time.Time { return now }
	users.err = errors.New("owner denied")
	if _, err := svc.ContainerObservation(context.Background(), "9", "human", 7); err == nil || backend.calls != 0 || identities.calls != 0 {
		t.Fatal("private read before current user authorization")
	}
	users.err = nil
	data, err := svc.ContainerObservation(context.Background(), "9", "human", 7)
	if err != nil || data.ProofState != "verified" || identities.calls != 2 || backend.scope.Cgroup == nil || data.CPUUpperCores == nil {
		t.Fatal("exact container read", data, err)
	}
	backend.after = func() {
		copy := *proof
		copy.HierarchyDigest = strings.Repeat("c", 64)
		identities.snapshot.ModuleInstances[0].Container = &copy
	}
	if _, err := svc.ContainerObservation(context.Background(), "9", "human", 7); err == nil {
		t.Fatal("proof epoch changed during query")
	}
	backend.after = nil
	identities.snapshot.ModuleInstances[0].Container = nil
	data, err = svc.ContainerObservation(context.Background(), "9", "human", 7)
	if err != nil || data.ProofState != "unverified" || data.CPUUpperCores != nil {
		t.Fatal("unverified boundary inferred a limit", data, err)
	}
}
