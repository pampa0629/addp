package service

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type UserHostNodeScopeReader interface {
	HostNodesForUser(context.Context, string) ([]client.UserHostNode, error)
}

type ResourceCoverage struct {
	Expected     int  `json:"expected"`
	Valid        int  `json:"valid"`
	NoData       int  `json:"no_data"`
	Stale        int  `json:"stale"`
	NotConnected int  `json:"not_connected"`
	Complete     bool `json:"complete"`
}
type ResourceRank struct {
	Node      client.UserHostNode `json:"node"`
	Value     float64             `json:"value"`
	SampledAt time.Time           `json:"sampled_at"`
}
type ResourceRanking struct {
	MetricKey     string           `json:"metric_key"`
	Unit          string           `json:"unit"`
	WindowSeconds int              `json:"window_seconds"`
	Coverage      ResourceCoverage `json:"coverage"`
	Top           []ResourceRank   `json:"top"`
}
type ResourceInventory struct {
	Managed      int            `json:"managed"`
	Enabled      int            `json:"enabled"`
	Disabled     int            `json:"disabled"`
	Connected    int            `json:"connected"`
	NotConnected int            `json:"not_connected"`
	Collection   map[string]int `json:"collection"`
}
type ResourceOverviewResponse struct {
	Scope           string            `json:"scope"`
	EvaluatedAt     time.Time         `json:"evaluated_at"`
	QueriedAt       time.Time         `json:"queried_at"`
	PolicyVersion   uint64            `json:"policy_version"`
	LookbackSeconds int64             `json:"lookback_seconds"`
	Inventory       ResourceInventory `json:"inventory"`
	Rankings        []ResourceRanking `json:"rankings"`
}

func (s *ResourceObservationService) Overview(ctx context.Context, principal, token string) (ResourceOverviewResponse, error) {
	var result ResourceOverviewResponse
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if s == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	release, err := s.limiter.Acquire(principal, resourcequery.DefaultBudget())
	if err != nil {
		return result, err
	}
	defer release()
	cfg, err := s.Policy(ctx)
	if err != nil {
		return result, err
	}
	ctx, budgetCancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer budgetCancel()
	if err := s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, err
	}
	reader, ok := s.nodes.(UserHostNodeScopeReader)
	if !ok {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	nodes, err := reader.HostNodesForUser(ctx, token)
	if errors.Is(err, client.ErrHostNodeScopeBudget) {
		return result, resourcequery.ErrBudget
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, err
	}
	ids := make([]string, len(nodes))
	for i, node := range nodes {
		ids[i] = node.NodeID
	}
	now := s.now().UTC()
	plan, err := resourcequery.NewSummaryPlan(max(len(ids), 1), resourcequery.PressureMetrics(), now.Truncate(time.Second), cfg.Budget)
	if err != nil {
		return result, err
	}
	summaries, err := s.observeNodeSummaries(ctx, cfg, now, token, ids, plan)
	if err != nil {
		return result, err
	}
	var completed time.Time
	// Empty inventories still have an evaluation/completion time and owner authorization.
	if len(summaries.Data) > 0 {
		completed = summaries.Data[0].QueriedAt
	} else {
		completed = s.now().UTC()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if completed.Before(now) {
		return result, resourcequery.ErrUnavailable
	}
	return aggregateResourceOverview(nodes, summaries, now.Truncate(time.Second), completed, cfg.Version)
}

func aggregateResourceOverview(nodes []client.UserHostNode, observations ResourceSummaryResponse, evaluated, completed time.Time, version uint64) (ResourceOverviewResponse, error) {
	result := ResourceOverviewResponse{Scope: "current_authorized_host_nodes", EvaluatedAt: evaluated, QueriedAt: completed, PolicyVersion: version, LookbackSeconds: resourcequery.LookbackSeconds,
		Inventory: ResourceInventory{Managed: len(nodes), Collection: map[string]int{"collecting": 0, "failed": 0, "stale": 0, "no_sample": 0, "not_connected": 0}}, Rankings: []ResourceRanking{}}
	fail := func() (ResourceOverviewResponse, error) {
		return ResourceOverviewResponse{}, resourcequery.ErrUnavailable
	}
	if len(observations.Data) != len(nodes) {
		return fail()
	}
	for _, key := range resourcequery.PressureMetrics() {
		for _, metric := range resourcequery.Catalog() {
			if metric.Key == key {
				result.Rankings = append(result.Rankings, ResourceRanking{MetricKey: key, Unit: metric.Unit, WindowSeconds: metric.WindowSeconds, Coverage: ResourceCoverage{Expected: len(nodes)}, Top: []ResourceRank{}})
				break
			}
		}
	}

	seen := map[string]bool{}
	for i, node := range nodes {
		row := observations.Data[i]
		if seen[node.NodeID] || row.Subject.Kind != "node" || row.Subject.NodeID != node.NodeID || row.NodeVersion != node.Version || row.PolicyVersion != version || !row.End.Equal(evaluated) || !row.QueriedAt.Equal(completed) || row.Collection == nil || len(row.Series) != len(result.Rankings) {
			return fail()
		}
		seen[node.NodeID] = true
		if node.Enabled {
			result.Inventory.Enabled++
		} else {
			result.Inventory.Disabled++
		}
		if row.TargetID != "" {
			if !node.Enabled {
				return fail()
			}
			result.Inventory.Connected++
		} else {
			result.Inventory.NotConnected++
		}
		if _, ok := result.Inventory.Collection[row.Collection.State]; !ok {
			return fail()
		}
		result.Inventory.Collection[row.Collection.State]++
		for j := range result.Rankings {
			rank := &result.Rankings[j]
			series := row.Series[j]
			if series.MetricKey != rank.MetricKey || series.Unit != rank.Unit || series.WindowSeconds != rank.WindowSeconds || len(series.Dimensions) != 0 || len(series.Points) != 1 {
				return fail()
			}
			point := series.Points[0]
			if !point.EvaluatedAt.Equal(evaluated) {
				return fail()
			}
			switch point.DataState {
			case "valid":
				if row.TargetID == "" || !node.Enabled || point.Value == nil || point.SampledAt == nil || math.IsNaN(*point.Value) || math.IsInf(*point.Value, 0) || *point.Value < 0 || *point.Value > 100 || point.SampledAt.After(evaluated) || completed.Sub(*point.SampledAt) > time.Duration(resourcequery.FreshnessSeconds)*time.Second {
					return fail()
				}
				rank.Coverage.Valid++
				rank.Top = append(rank.Top, ResourceRank{Node: node, Value: *point.Value, SampledAt: *point.SampledAt})
			case "no_data":
				if point.Value != nil || point.SampledAt != nil || row.TargetID == "" {
					return fail()
				}
				rank.Coverage.NoData++
			case "stale":
				if row.TargetID == "" || point.Value == nil || point.SampledAt == nil || math.IsNaN(*point.Value) || math.IsInf(*point.Value, 0) || *point.Value < 0 || *point.Value > 100 || completed.Sub(*point.SampledAt) <= time.Duration(resourcequery.FreshnessSeconds)*time.Second {
					return fail()
				}
				rank.Coverage.Stale++
			case "not_connected":
				if point.Value != nil || point.SampledAt != nil || row.TargetID != "" {
					return fail()
				}
				rank.Coverage.NotConnected++
			default:
				return fail()
			}
		}
	}
	for i := range result.Rankings {
		rank := &result.Rankings[i]
		rank.Coverage.Complete = rank.Coverage.Valid == rank.Coverage.Expected
		sort.Slice(rank.Top, func(i, j int) bool {
			if rank.Top[i].Value == rank.Top[j].Value {
				return rank.Top[i].Node.NodeID < rank.Top[j].Node.NodeID
			}
			return rank.Top[i].Value > rank.Top[j].Value
		})
		if len(rank.Top) > 5 {
			rank.Top = rank.Top[:5]
		}
	}
	return result, nil
}
