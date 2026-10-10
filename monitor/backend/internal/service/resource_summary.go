package service

import (
	"context"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type ResourceSummaryResponse struct {
	Data []ResourceObservationResponse `json:"data"`
}

func (s *ResourceObservationService) Summaries(ctx context.Context, principal, token string, ids []string) (ResourceSummaryResponse, error) {
	result := ResourceSummaryResponse{}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if len(ids) < 1 || len(ids) > 100 {
		return result, resourcequery.ErrInvalid
	}
	result.Data = make([]ResourceObservationResponse, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if (resourcequery.Scope{NodeID: id, Instance: "validation"}).Validate() != nil || seen[id] {
			return result, resourcequery.ErrInvalid
		}
		seen[id] = true
	}
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
	now := s.now().UTC()
	plan, err := resourcequery.NewSummaryPlan(len(ids), now.Truncate(time.Second), cfg.Budget)
	if err != nil {
		return result, err
	}
	if err := s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, err
	}
	if s.nodes == nil {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	enabled := map[string]bool{}
	for _, id := range ids {
		ref, err := s.nodes.GetHostNodeForUser(ctx, id, token)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, err
		}
		if ref == nil || ref.NodeID != id || ref.Version < 1 {
			return result, metricsdiscovery.ErrIdentityUnavailable
		}
		enabled[id] = ref.Enabled
		result.Data = append(result.Data, ResourceObservationResponse{
			Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: id}, NodeVersion: ref.Version,
			PolicyVersion: cfg.Version, LookbackSeconds: resourcequery.LookbackSeconds,
			Start: plan.Start, End: plan.End, StepSeconds: plan.StepSeconds,
			Collection: &resourcequery.Collection{State: "not_connected", Filesystem: "unknown", Network: "unknown"},
			Series:     resourcequery.Empty(plan, "not_connected"),
		})
	}
	if !s.enabled {
		return result, metricsdiscovery.ErrDisabled
	}
	if s.backend == nil || s.policy == nil || s.targets == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	rows := []metricsdiscovery.NodeTarget{}
	for _, active := range enabled {
		if active {
			rows, err = s.targets.Snapshot(ctx)
			if err != nil {
				return result, err
			}
			break
		}
	}
	if metricsdiscovery.CheckReservations(rows) != nil {
		return result, resourcequery.ErrUnavailable
	}
	targets := make(map[string]metricsdiscovery.NodeTarget)
	for _, row := range rows {
		if row.Enabled && row.MonitorKind == "host_resources" && enabled[row.Subject.NodeID] {
			targets[row.Subject.NodeID] = row
		}
	}
	scopes := []resourcequery.Scope{}
	for i := range result.Data {
		row := &result.Data[i]
		if target, ok := targets[row.Subject.NodeID]; ok {
			resolved, err := s.policy.Resolve(ctx, target.Source.Endpoint)
			if err != nil {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				return result, err
			}
			row.TargetID, row.TargetSavedVersion = target.ID, target.Version
			scopes = append(scopes, resourcequery.Scope{NodeID: row.Subject.NodeID, Instance: resolved.Address})
		}
	}
	if len(scopes) > 0 {
		observed, err := s.backend.Summaries(ctx, scopes, plan.End, cfg.Budget)
		if err != nil {
			return result, err
		}
		if len(observed) != len(scopes) {
			return result, resourcequery.ErrUnavailable
		}
		for i := range result.Data {
			row := &result.Data[i]
			if row.TargetID == "" {
				continue
			}
			value, ok := observed[row.Subject.NodeID]
			if !ok {
				return result, resourcequery.ErrUnavailable
			}
			row.Series = value.Series
			row.Collection = &value.Collection
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	completed := s.now().UTC()
	if completed.Before(now) {
		return result, resourcequery.ErrUnavailable
	}
	for i := range result.Data {
		result.Data[i].QueriedAt = completed
		markCurrentFreshness(&result.Data[i])
	}
	return result, nil
}
