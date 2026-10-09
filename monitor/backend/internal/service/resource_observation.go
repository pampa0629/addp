package service

import (
	"context"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
)

type ResourceQueryPolicyStore interface {
	Get(context.Context) (models.ResourceQueryPolicy, error)
	Save(context.Context, models.ResourceQueryPolicy, uint64) (models.ResourceQueryPolicy, error)
}
type ResourceBackend interface {
	Collection(context.Context, resourcequery.Scope, time.Time, resourcequery.Budget) (resourcequery.Collection, error)
	Query(context.Context, resourcequery.Plan, resourcequery.Scope, resourcequery.Budget) ([]resourcequery.Series, error)
}
type ResourceQueryPolicyInput struct {
	Version uint64 `json:"version"`
	resourcequery.Budget
}
type ResourceQueryPolicyResponse struct {
	Version uint64 `json:"version"`
	resourcequery.Budget
	PendingRestart bool `json:"pending_restart"`
}
type ResourceObservationResponse struct {
	Collection         *resourcequery.Collection    `json:"collection,omitempty"`
	Subject            metricsdiscovery.NodeSubject `json:"subject"`
	LookbackSeconds    int64                        `json:"lookback_seconds"`
	NodeVersion        int64                        `json:"node_version"`
	TargetID           string                       `json:"target_id,omitempty"`
	TargetSavedVersion int64                        `json:"target_saved_version,omitempty"`
	PolicyVersion      uint64                       `json:"policy_version"`
	QueriedAt          time.Time                    `json:"queried_at"`
	Start              time.Time                    `json:"start"`
	End                time.Time                    `json:"end"`
	StepSeconds        int64                        `json:"step_seconds"`
	Series             []resourcequery.Series       `json:"series"`
}
type ResourceObservationService struct {
	policies ResourceQueryPolicyStore
	targets  TargetStore
	nodes    UserNodeReader
	backend  ResourceBackend
	policy   *metricsdiscovery.SourcePolicy
	enabled  bool
	limiter  resourcequery.Limiter
	now      func() time.Time
}

func NewResourceObservationService(policies ResourceQueryPolicyStore, targets TargetStore, nodes UserNodeReader, backend ResourceBackend, enabled bool, policy *metricsdiscovery.SourcePolicy) *ResourceObservationService {
	return &ResourceObservationService{policies: policies, targets: targets, nodes: nodes, backend: backend, enabled: enabled, policy: policy, now: time.Now}
}
func (s *ResourceObservationService) Policy(ctx context.Context) (ResourceQueryPolicyResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if s == nil || s.policies == nil {
		return ResourceQueryPolicyResponse{}, metricsdiscovery.ErrUnconfigured
	}
	row, err := s.policies.Get(ctx)
	if err != nil {
		return ResourceQueryPolicyResponse{}, err
	}
	if row.Budget.Validate() != nil {
		return ResourceQueryPolicyResponse{}, resourcequery.ErrUnavailable
	}
	return ResourceQueryPolicyResponse{Version: row.Version, Budget: row.Budget}, nil
}
func (s *ResourceObservationService) UpdatePolicy(ctx context.Context, input ResourceQueryPolicyInput, user uint) (ResourceQueryPolicyResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if input.Budget.Validate() != nil {
		return ResourceQueryPolicyResponse{}, resourcequery.ErrInvalid
	}
	if s == nil || s.policies == nil {
		return ResourceQueryPolicyResponse{}, metricsdiscovery.ErrUnconfigured
	}
	row, err := s.policies.Save(ctx, models.ResourceQueryPolicy{Budget: input.Budget, UpdatedBy: user}, input.Version)
	return ResourceQueryPolicyResponse{Version: row.Version, Budget: row.Budget}, err
}
func (s *ResourceObservationService) Query(ctx context.Context, principal, node, token string, keys []string, start, end time.Time, trend bool, dimensions resourcequery.Dimensions) (ResourceObservationResponse, error) {
	result := ResourceObservationResponse{Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: node}}
	// This timeout includes current-user authorization, target lookup and transport.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if (resourcequery.Scope{NodeID: node, Instance: "validation"}).Validate() != nil {
		return result, resourcequery.ErrInvalid
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
	if !trend {
		end = now.Truncate(time.Second)
	}
	plan, err := resourcequery.NewPlan(keys, start, end, now, trend, dimensions, cfg.Budget)
	if err != nil {
		return result, err
	}
	if err := s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, err
	}
	if s.nodes == nil {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	ref, err := s.nodes.GetHostNodeForUser(ctx, node, token)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, err
	}
	if ref == nil || ref.NodeID != node || ref.Version < 1 {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	result.LookbackSeconds = resourcequery.LookbackSeconds
	result.NodeVersion = ref.Version
	result.PolicyVersion = cfg.Version
	result.Start = plan.Start
	result.End = plan.End
	result.StepSeconds = plan.StepSeconds
	if !s.enabled {
		return result, metricsdiscovery.ErrDisabled
	}
	if s.backend == nil || s.policy == nil || s.targets == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	result.Series = resourcequery.Empty(plan, "not_connected")
	if !trend {
		result.Collection = &resourcequery.Collection{State: "not_connected", Filesystem: "unknown", Network: "unknown"}
	}
	if ref.Enabled {
		rows, err := s.targets.Snapshot(ctx)
		if err != nil {
			return result, err
		}
		if metricsdiscovery.CheckReservations(rows) != nil {
			return result, resourcequery.ErrUnavailable
		}
		var target *metricsdiscovery.NodeTarget
		for i := range rows {
			row := &rows[i]
			if row.Enabled && row.Subject.NodeID == node && row.MonitorKind == "host_resources" {
				target = row
				break
			}
		}
		if target != nil {
			resolved, err := s.policy.Resolve(ctx, target.Source.Endpoint)
			if err != nil {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				return result, err
			}
			result.TargetID, result.TargetSavedVersion = target.ID, target.Version
			scope := resourcequery.Scope{NodeID: node, Instance: resolved.Address}
			if !trend {
				collection, collectionErr := s.backend.Collection(ctx, scope, now.Truncate(time.Second), cfg.Budget)
				if collectionErr != nil {
					return result, collectionErr
				}
				result.Collection = &collection
			}
			result.Series, err = s.backend.Query(ctx, plan, scope, cfg.Budget)
			if err != nil {
				return result, err
			}
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	result.QueriedAt = s.now().UTC()
	if result.QueriedAt.Before(now) {
		return result, resourcequery.ErrUnavailable
	}
	if !trend {
		if result.Collection.SampledAt != nil && result.QueriedAt.Sub(*result.Collection.SampledAt) > time.Duration(resourcequery.FreshnessSeconds)*time.Second {
			result.Collection.State = "stale"
			result.Collection.Filesystem = "unknown"
			result.Collection.Network = "unknown"
		}
		for i := range result.Series {
			for j := range result.Series[i].Points {
				point := &result.Series[i].Points[j]
				if point.DataState == "valid" && point.SampledAt != nil && result.QueriedAt.Sub(*point.SampledAt) > time.Duration(resourcequery.FreshnessSeconds)*time.Second {
					point.DataState = "stale"
				}
			}
		}
	}
	return result, nil
}
