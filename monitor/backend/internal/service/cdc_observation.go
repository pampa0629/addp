package service

import (
	"context"
	"reflect"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
)

type CDCResourceBackend interface {
	CDCQuery(context.Context, resourcequery.Plan, resourcequery.CDCScope, resourcequery.Budget) (resourcequery.Summary, error)
}
type CDCObservationDependencies struct {
	Identities metricsdiscovery.IdentityReader
	Backend    CDCResourceBackend
}

func (s *ResourceObservationService) WithCDC(deps CDCObservationDependencies) *ResourceObservationService {
	s.cdc = deps
	return s
}

type CDCResourceSubject struct {
	Kind          string    `json:"kind"`
	ComponentID   string    `json:"component_id"`
	ComponentType string    `json:"component_type"`
	StartedAt     time.Time `json:"started_at"`
}
type CDCResourceObservation struct {
	Subject    CDCResourceSubject     `json:"subject"`
	Collection ProcessCollection      `json:"collection"`
	Series     []resourcequery.Series `json:"series"`
}
type CDCResourceResponse struct {
	DiscoveryState  string                   `json:"discovery_state"`
	Data            []CDCResourceObservation `json:"data"`
	QueriedAt       time.Time                `json:"queried_at"`
	Start           time.Time                `json:"start"`
	End             time.Time                `json:"end"`
	StepSeconds     int64                    `json:"step_seconds"`
	PolicyVersion   uint64                   `json:"policy_version"`
	LookbackSeconds int64                    `json:"lookback_seconds"`
}

func (s *ResourceObservationService) currentCDCIdentities(ctx context.Context) (*commonmodels.ObservabilityComponentSnapshot, error) {
	snapshot, err := s.cdc.Identities.GetObservabilityIdentities(ctx)
	now := s.now().UTC()
	if err != nil || snapshot == nil || snapshot.ObservedAt.Before(now.Add(-metricsdiscovery.Timeout)) || snapshot.ObservedAt.After(now.Add(metricsdiscovery.Timeout)) {
		return nil, metricsdiscovery.ErrIdentityUnavailable
	}
	rows := snapshot.InfrastructureComponents
	if rows == nil || rows.Items == nil || len(rows.Items) > 2 || (rows.State != "available" && rows.State != "unavailable") || (rows.State == "unavailable" && len(rows.Items) > 0) {
		return nil, metricsdiscovery.ErrIdentityUnavailable
	}
	seen := map[string]bool{}
	for _, item := range rows.Items {
		if item.Validate() != nil || item.StartedAt.After(snapshot.ObservedAt) || seen[item.Kind] {
			return nil, metricsdiscovery.ErrIdentityUnavailable
		}
		seen[item.Kind] = true
	}
	return rows, nil
}

// CDCResources reads only current infrastructure facts. Platform permission is
// enforced by the public route; no engine or host grant is inferred here.
func (s *ResourceObservationService) CDCResources(ctx context.Context, principal, id string, keys []string, start, end time.Time, trend bool) (CDCResourceResponse, error) {
	result := CDCResourceResponse{Data: []CDCResourceObservation{}}
	if trend {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil || parsed.String() != id {
			return result, resourcequery.ErrInvalid
		}
	}
	if s == nil || s.cdc.Identities == nil || s.cdc.Backend == nil || s.policy == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
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
	if err = s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, err
	}
	if !s.enabled {
		return result, metricsdiscovery.ErrDisabled
	}
	current, err := s.currentCDCIdentities(ctx)
	if err != nil {
		return result, err
	}
	now := s.now().UTC()
	if !trend {
		start = now.Truncate(time.Second)
		end = start
	}
	result.DiscoveryState = current.State
	result.Start, result.End, result.StepSeconds, result.PolicyVersion, result.LookbackSeconds = start, end, resourcequery.MinimumStepSeconds, cfg.Version, resourcequery.LookbackSeconds
	plans := []resourcequery.Plan{}
	items := []commonmodels.ObservabilityComponentIdentity{}
	total := 0
	for _, item := range current.Items {
		if trend && item.ID() != id {
			continue
		}
		selected := keys
		if !trend {
			selected = []string{}
			for _, d := range resourcequery.CDCCatalog(item.Kind) {
				selected = append(selected, d.Key)
			}
		}
		plan, err := resourcequery.NewCDCPlan(item.Kind, selected, start, end, now, trend, cfg.Budget)
		if err != nil {
			return CDCResourceResponse{}, err
		}
		if trend {
			plan = resourcequery.LimitTrendStart(plan, item.StartedAt)
		}
		total += plan.SeriesUpperBound * plan.Points
		if total > cfg.MaxTotalPoints || (!trend && total > cfg.MaxSeries) {
			return CDCResourceResponse{}, resourcequery.ErrBudget
		}
		plans = append(plans, plan)
		items = append(items, item)
	}
	if trend && len(items) != 1 {
		return CDCResourceResponse{}, resourcequery.ErrInvalid
	}
	physical := map[string]bool{}
	for i, item := range items {
		plan := plans[i]
		state := "not_connected"
		var scope *resourcequery.CDCScope
		if item.Metrics.State == "unavailable" {
			state = "source_unavailable"
		}
		if item.Metrics.State == "available" {
			resolved, e := s.policy.Resolve(ctx, item.Metrics.Endpoint)
			if e != nil {
				if ctx.Err() != nil {
					return CDCResourceResponse{}, ctx.Err()
				}
				state = "source_unavailable"
			} else {
				if physical[resolved.Address] {
					return CDCResourceResponse{}, metricsdiscovery.ErrIdentityUnavailable
				}
				physical[resolved.Address] = true
				scope = &resourcequery.CDCScope{Component: item, Instance: resolved.Address}
				state = "no_sample"
			}
		}
		row := CDCResourceObservation{Subject: CDCResourceSubject{Kind: "infrastructure_component", ComponentID: item.ID(), ComponentType: item.Kind, StartedAt: item.StartedAt}, Collection: ProcessCollection{State: state}, Series: resourcequery.Empty(plan, state)}
		if scope != nil {
			data, e := s.cdc.Backend.CDCQuery(ctx, plan, *scope, cfg.Budget)
			if e != nil {
				return CDCResourceResponse{}, e
			}
			row.Series = data.Series
			if !trend {
				row.Collection = ProcessCollection{State: data.Collection.State, SampledAt: data.Collection.SampledAt}
			}
		}
		result.Data = append(result.Data, row)
		if trend {
			result.Start, result.End, result.StepSeconds = plan.Start, plan.End, plan.StepSeconds
		}
	}
	latest, err := s.currentCDCIdentities(ctx)
	if err != nil {
		return CDCResourceResponse{}, err
	}
	// Compare unordered current items, including nonqueried peers and unavailable state.
	currentItems, latestItems := map[string]commonmodels.ObservabilityComponentIdentity{}, map[string]commonmodels.ObservabilityComponentIdentity{}
	for _, item := range current.Items {
		currentItems[item.ID()] = item
	}
	for _, item := range latest.Items {
		latestItems[item.ID()] = item
	}
	if current.State != latest.State || !reflect.DeepEqual(currentItems, latestItems) {
		return CDCResourceResponse{}, metricsdiscovery.ErrIdentityUnavailable
	}
	policy, err := s.Policy(ctx)
	if err != nil {
		return CDCResourceResponse{}, err
	}
	if policy.Version != cfg.Version {
		return CDCResourceResponse{}, resourcequery.ErrConflict
	}
	if ctx.Err() != nil {
		return CDCResourceResponse{}, ctx.Err()
	}
	result.QueriedAt = s.now().UTC()
	if result.QueriedAt.Before(now) {
		return CDCResourceResponse{}, resourcequery.ErrUnavailable
	}
	// Account for time spent reading a peer: freshness is measured at completion.
	if !trend {
		for i := range result.Data {
			row := &result.Data[i]
			collection := resourcequery.Collection{State: row.Collection.State, SampledAt: row.Collection.SampledAt}
			adapted := ResourceObservationResponse{Series: row.Series, Collection: &collection, QueriedAt: result.QueriedAt}
			markCurrentFreshness(&adapted)
			row.Series = adapted.Series
			row.Collection = ProcessCollection{State: collection.State, SampledAt: collection.SampledAt}
		}
	}
	return result, nil
}
