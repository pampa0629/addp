package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
)

func TestResourceSummaryAuthorizesWholeBatchAndExcludesStoppedHistory(t *testing.T) {
	ids := []string{uuid.NewString(), uuid.NewString()}
	nodes, backend := &resourceNodes{enabled: true}, &resourceBackend{}
	targets := &targetTestStore{}
	for i, id := range ids {
		targets.rows = append(targets.rows, metricsdiscovery.NodeTarget{ID: uuid.NewString(), Version: 2, Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: id}, MonitorKind: "host_resources", Source: metricsdiscovery.NodeSource{Type: "node_exporter", Endpoint: []string{"https://127.0.0.1:9100/metrics", "https://localhost:9100/metrics"}[i]}, Enabled: i == 0})
	}
	policies := &resourcePolicies{row: models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget(), Version: 1}}
	svc := NewResourceObservationService(policies, targets, nodes, backend, true, resourceSourcePolicy(t), ProcessObservationDependencies{})
	query := func() (ResourceSummaryResponse, error) {
		return svc.Summaries(context.Background(), "user", "addp_at_current_user", ids)
	}
	nodes.denyID = ids[1]
	if _, err := query(); err == nil || backend.summaryCalls != 0 {
		t.Fatal("partial authorization queried backend", err)
	}
	nodes.denyID = ""
	value, err := query()
	if err != nil || len(value.Data) != 2 || backend.summaryCalls != 1 || len(backend.summaryScopes) != 1 || backend.summaryScopes[0].NodeID != ids[0] || value.Data[1].Collection.State != "not_connected" || value.Data[1].Series[0].Points[0].DataState != "not_connected" || value.Data[0].NodeVersion != 3 || value.Data[0].TargetSavedVersion != 2 {
		t.Fatalf("summary=%+v error=%v", value, err)
	}
	for _, token := range nodes.tokens {
		if token != "addp_at_current_user" {
			t.Fatal("user token replaced")
		}
	}
	nodes.enabled = false
	value, err = query()
	if err != nil || backend.summaryCalls != 1 || value.Data[0].Collection.State != "not_connected" {
		t.Fatal("disabled node reused history", err)
	}
	nodes.enabled = true
	policies.row.MaxSeries = 3
	if _, err := query(); !errors.Is(err, resourcequery.ErrBudget) || backend.summaryCalls != 1 {
		t.Fatal("batch total budget bypass", err)
	}
	policies.row.MaxSeries = 100
	if _, err := svc.Summaries(context.Background(), "user", "token", []string{ids[0], ids[0]}); !errors.Is(err, resourcequery.ErrInvalid) {
		t.Fatal("duplicate node accepted", err)
	}
	lease, err := svc.limiter.Acquire("user", resourcequery.DefaultBudget())
	if err != nil {
		t.Fatal("failed requests retained leases", err)
	}
	lease()
	age := 59 * time.Second
	backend.validAge = &age
	clock := time.Now().UTC().Truncate(time.Second)
	backend.evidence = &resourcequery.Collection{State: "collecting", SampledAt: &clock, Filesystem: "available", Network: "available"}
	calls := 0
	svc.now = func() time.Time {
		calls++
		if calls%2 == 1 {
			return clock
		}
		return clock.Add(61 * time.Second)
	}
	value, err = query()
	if err != nil || value.Data[0].Series[0].Points[0].DataState != "stale" || value.Data[0].Collection.State != "stale" || value.Data[0].Collection.Network != "unknown" {
		t.Fatal("completion freshness ignored", value, err)
	}
}
