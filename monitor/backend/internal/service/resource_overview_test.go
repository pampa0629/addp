package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
)

type overviewNodes struct {
	resourceNodes
	rows       []client.UserHostNode
	scopeErr   error
	scopeCalls int
	scopeHook  func(context.Context)
}

func (n *overviewNodes) HostNodesForUser(ctx context.Context, token string) ([]client.UserHostNode, error) {
	n.scopeCalls++
	n.tokens = append(n.tokens, token)
	if n.scopeHook != nil {
		n.scopeHook(ctx)
	}
	return n.rows, n.scopeErr
}

func overviewFixture(count int) ([]client.UserHostNode, ResourceSummaryResponse, time.Time) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	nodes := []client.UserHostNode{}
	rows := ResourceSummaryResponse{Data: []ResourceObservationResponse{}}
	plan, _ := resourcequery.NewSummaryPlan(max(count, 1), now, resourcequery.DefaultBudget())
	for i := 0; i < count; i++ {
		node := client.UserHostNode{NodeID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), DisplayName: fmt.Sprintf("节点%d", i), NodeKind: "virtual", Version: 3, Enabled: true}
		nodes = append(nodes, node)
		row := ResourceObservationResponse{Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: node.NodeID}, NodeVersion: 3, PolicyVersion: 2, TargetID: uuid.NewString(), TargetSavedVersion: 1, End: now, QueriedAt: now, Collection: &resourcequery.Collection{State: "collecting", SampledAt: &now, Filesystem: "unknown", Network: "unknown"}, Series: resourcequery.Empty(plan, "valid")}
		for j := range row.Series {
			value := float64(i)
			row.Series[j].Points[0].Value = &value
			row.Series[j].Points[0].SampledAt = &now
		}
		rows.Data = append(rows.Data, row)
	}
	return nodes, rows, now
}
func TestResourceOverviewRanksAllNodesAndReportsMetricCoverage(t *testing.T) {
	nodes, rows, now := overviewFixture(9)
	// The highest values are beyond a typical five-row view. Zero remains valid.
	rows.Data[8].Series[0].Points[0].DataState = "stale"
	stamp := now.Add(-61 * time.Second)
	rows.Data[8].Series[0].Points[0].SampledAt = &stamp
	nodes[7].Enabled = false
	rows.Data[7].TargetID = ""
	rows.Data[7].Collection = &resourcequery.Collection{State: "not_connected", Filesystem: "unknown", Network: "unknown"}
	for j := range rows.Data[7].Series {
		rows.Data[7].Series[j].Points[0] = resourcequery.Point{EvaluatedAt: now, DataState: "not_connected"}
	}
	rows.Data[6].Series[0].Points[0] = resourcequery.Point{EvaluatedAt: now, DataState: "no_data"}
	// Equal values resolve by stable node ID.
	value := 4.0
	rows.Data[5].Series[0].Points[0].Value = &value
	result, err := aggregateResourceOverview(nodes, rows, now, now, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Inventory.Managed != 9 || result.Inventory.Disabled != 1 || result.Inventory.Connected != 8 || result.Inventory.Collection["not_connected"] != 1 {
		t.Fatal(result.Inventory)
	}
	cpu, memory := result.Rankings[0], result.Rankings[1]
	if cpu.Coverage != (ResourceCoverage{Expected: 9, Valid: 6, NoData: 1, Stale: 1, NotConnected: 1}) || len(cpu.Top) != 5 || cpu.Top[0].Node.NodeID != nodes[4].NodeID || cpu.Top[1].Node.NodeID != nodes[5].NodeID || memory.Top[0].Node.NodeID != nodes[8].NodeID || memory.Coverage.Valid != 8 {
		t.Fatalf("ranks=%+v", result.Rankings)
	}
	zeroNodes, zeroRows, now := overviewFixture(1)
	result, err = aggregateResourceOverview(zeroNodes, zeroRows, now, now, 2)
	if err != nil || result.Rankings[0].Top[0].Value != 0 || !result.Rankings[0].Coverage.Complete {
		t.Fatal("valid zero excluded", result, err)
	}
}
func TestResourceOverviewRefusesMalformedOrMixedIdentityResults(t *testing.T) {
	for _, tc := range []string{"version", "clock", "duplicate", "bad-value", "missing", "unknown-state", "wrong-metric", "disabled-value"} {
		nodes, rows, now := overviewFixture(2)
		switch tc {
		case "version":
			rows.Data[0].NodeVersion++
		case "clock":
			rows.Data[1].End = now.Add(time.Second)
		case "duplicate":
			nodes[1] = nodes[0]
		case "bad-value":
			v := 101.0
			rows.Data[0].Series[0].Points[0].Value = &v
		case "missing":
			rows.Data = rows.Data[:1]
		case "unknown-state":
			rows.Data[0].Series[0].Points[0].DataState = "healthy"
		case "wrong-metric":
			rows.Data[0].Series[0].MetricKey = "node.load.average_1m"
		case "disabled-value":
			nodes[0].Enabled = false
		}
		if _, err := aggregateResourceOverview(nodes, rows, now, now, 2); !errors.Is(err, resourcequery.ErrUnavailable) {
			t.Fatal(tc, err)
		}
	}
}
func TestResourceOverviewSharesAdmissionBudgetAuthorizationAndEmptyScope(t *testing.T) {
	nodes := &overviewNodes{resourceNodes: resourceNodes{enabled: true}}
	backend := &resourceBackend{}
	policies := &resourcePolicies{row: models.ResourceQueryPolicy{Version: 2, Budget: resourcequery.DefaultBudget()}}
	svc := NewResourceObservationService(policies, &targetTestStore{}, nodes, backend, true, resourceSourcePolicy(t), ProcessObservationDependencies{})
	query := func() (ResourceOverviewResponse, error) {
		return svc.Overview(context.Background(), "user", "addp_at_current_user")
	}
	result, err := query()
	if err != nil || result.Inventory.Managed != 0 || len(result.Rankings[0].Top) != 0 || backend.summaryCalls != 0 || nodes.scopeCalls != 1 {
		t.Fatal(result, err)
	}
	lease1, _ := svc.limiter.Acquire("user", resourcequery.DefaultBudget())
	lease2, _ := svc.limiter.Acquire("user", resourcequery.DefaultBudget())
	if _, err := query(); !errors.Is(err, resourcequery.ErrBusy) || nodes.scopeCalls != 1 {
		t.Fatal("inventory fetched before hard admission", err)
	}
	lease1()
	lease2()
	nodes.rows, _, _ = overviewFixture(50)
	result, err = query()
	if err != nil || result.Inventory.Managed != 50 || result.Inventory.NotConnected != 50 || result.Rankings[0].Coverage.NotConnected != 50 {
		t.Fatal("whole boundary not accepted", result, err)
	}
	extra := nodes.rows[0]
	extra.NodeID = uuid.NewString()
	nodes.rows = append(nodes.rows, extra)
	if _, err := query(); !errors.Is(err, resourcequery.ErrBudget) || backend.summaryCalls != 0 {
		t.Fatal("partial ranking over budget", err)
	}
	nodes.rows = nodes.rows[:1]
	policies.row.MaxSeries = 1
	if _, err := query(); !errors.Is(err, resourcequery.ErrBudget) {
		t.Fatal("hot budget ignored", err)
	}
	policies.row.MaxSeries = 100
	nodes.scopeErr = &client.SystemAPIError{StatusCode: 403}
	if _, err := query(); err == nil || backend.summaryCalls != 0 {
		t.Fatal("owner refusal bypass", err)
	}
	nodes.scopeErr = client.ErrHostNodeScopeBudget
	if _, err := query(); !errors.Is(err, resourcequery.ErrBudget) {
		t.Fatal(err)
	}
	nodes.scopeErr = nil
	nodes.rows[0].Version++
	if _, err := query(); !errors.Is(err, resourcequery.ErrUnavailable) {
		t.Fatal("mixed snapshot version published", err)
	}
	nodes.rows[0].Version--
	svc.enabled = false
	if _, err := query(); !errors.Is(err, metricsdiscovery.ErrDisabled) {
		t.Fatal("disabled capability hidden", err)
	}
	for _, token := range nodes.tokens {
		if token != "addp_at_current_user" {
			t.Fatal("machine identity substituted")
		}
	}
}
func TestResourceOverviewInventoryTimeConsumesWholeRequestTimeout(t *testing.T) {
	nodes := &overviewNodes{scopeHook: func(ctx context.Context) { <-ctx.Done() }}
	policies := &resourcePolicies{row: models.ResourceQueryPolicy{Version: 1, Budget: resourcequery.DefaultBudget()}}
	policies.row.TimeoutSeconds = 1
	svc := NewResourceObservationService(policies, &targetTestStore{}, nodes, &resourceBackend{}, true, resourceSourcePolicy(t), ProcessObservationDependencies{})
	start := time.Now()
	_, err := svc.Overview(context.Background(), "user", "addp_at_current_user")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatal("scope got a separate deadline", err, time.Since(start))
	}
}

func TestResourceOverviewUsesOneBatchForCompleteAuthorizedConnectedScope(t *testing.T) {
	rows, _, now := overviewFixture(2)
	nodes := &overviewNodes{resourceNodes: resourceNodes{enabled: true}, rows: rows}
	age := time.Duration(0)
	backend := &resourceBackend{validAge: &age, evidence: &resourcequery.Collection{State: "collecting", SampledAt: &now, Filesystem: "unknown", Network: "unknown"}}
	target := metricsdiscovery.NodeTarget{ID: uuid.NewString(), Version: 2, Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: rows[0].NodeID}, MonitorKind: "host_resources", Source: metricsdiscovery.NodeSource{Type: "node_exporter", Endpoint: "https://127.0.0.1:9100/metrics"}, Enabled: true}
	policies := &resourcePolicies{row: models.ResourceQueryPolicy{Version: 2, Budget: resourcequery.DefaultBudget()}}
	svc := NewResourceObservationService(policies, &targetTestStore{rows: []metricsdiscovery.NodeTarget{target}}, nodes, backend, true, resourceSourcePolicy(t), ProcessObservationDependencies{})
	svc.now = func() time.Time { return now }
	nodes.denyID = rows[1].NodeID
	if _, err := svc.Overview(context.Background(), "user", "addp_at_current_user"); err == nil || backend.summaryCalls != 0 {
		t.Fatal("partial user authorization reached metrics", err)
	}
	nodes.denyID = ""
	result, err := svc.Overview(context.Background(), "user", "addp_at_current_user")
	if err != nil || backend.summaryCalls != 1 || len(backend.summaryScopes) != 1 || result.Inventory.Managed != 2 || result.Inventory.Connected != 1 || result.Rankings[0].Coverage.Valid != 1 || result.Rankings[0].Top[0].Node.NodeID != rows[0].NodeID || result.Rankings[1].Coverage.NoData != 1 || !result.EvaluatedAt.Equal(now) {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, backend.summaryCalls)
	}
	backend.err = resourcequery.ErrUnavailable
	if _, err := svc.Overview(context.Background(), "user", "addp_at_current_user"); !errors.Is(err, resourcequery.ErrUnavailable) {
		t.Fatal("backend failure became partial success", err)
	}
}
