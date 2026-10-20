package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type UserRuntimeInstanceReader interface {
	GetRuntimeInstancesForUser(context.Context, []uint, string) ([]client.RuntimeInstanceReference, error)
}
type ProcessResourceBackend interface {
	ProcessSummaries(context.Context, []resourcequery.ProcessScope, time.Time, resourcequery.Budget) (map[uint]resourcequery.Summary, error)
	ProcessTrend(context.Context, resourcequery.Plan, resourcequery.ProcessScope, resourcequery.Budget) ([]resourcequery.Series, error)
}

// ProcessObservationDependencies reuse the same System and Prometheus clients.
// Authorization, private identity projection and observations stay distinct.
type ProcessObservationDependencies struct {
	Users      UserRuntimeInstanceReader
	Identities metricsdiscovery.IdentityReader
	Backend    ProcessResourceBackend
}

type ProcessResourceSubject struct {
	Kind       string `json:"kind"`
	ID         uint   `json:"id"`
	ModuleName string `json:"module_name"`
	InstanceID string `json:"instance_id"`
	Role       string `json:"role"`
}
type ProcessCollection struct {
	State     string     `json:"state"`
	SampledAt *time.Time `json:"sampled_at"`
}
type ProcessResourceObservation struct {
	Subject         ProcessResourceSubject `json:"subject"`
	NodeID          string                 `json:"node_id"`
	Collection      ProcessCollection      `json:"collection"`
	PolicyVersion   uint64                 `json:"policy_version"`
	LookbackSeconds int64                  `json:"lookback_seconds"`
	QueriedAt       time.Time              `json:"queried_at"`
	Series          []resourcequery.Series `json:"series"`
}
type ProcessResourceSummaryResponse struct {
	Data []ProcessResourceObservation `json:"data"`
}

type ProcessResourceTrendResponse struct {
	Subject         ProcessResourceSubject `json:"subject"`
	NodeID          string                 `json:"node_id"`
	PolicyVersion   uint64                 `json:"policy_version"`
	LookbackSeconds int64                  `json:"lookback_seconds"`
	QueriedAt       time.Time              `json:"queried_at"`
	Start           time.Time              `json:"start"`
	End             time.Time              `json:"end"`
	StepSeconds     int64                  `json:"step_seconds"`
	Series          []resourcequery.Series `json:"series"`
}

func (s *ResourceObservationService) ProcessSummaries(ctx context.Context, principal, token string, ids []uint) (ProcessResourceSummaryResponse, error) {
	result, _, err := s.observeProcesses(ctx, principal, token, ids, time.Time{}, time.Time{}, false)
	return result, err
}

func (s *ResourceObservationService) ProcessTrend(ctx context.Context, principal, token string, id uint, start, end time.Time) (ProcessResourceTrendResponse, error) {
	result, plan, err := s.observeProcesses(ctx, principal, token, []uint{id}, start, end, true)
	if err != nil {
		return ProcessResourceTrendResponse{}, err
	}
	row := result.Data[0]
	return ProcessResourceTrendResponse{Subject: row.Subject, NodeID: row.NodeID, PolicyVersion: row.PolicyVersion, LookbackSeconds: row.LookbackSeconds, QueriedAt: row.QueriedAt, Start: plan.Start, End: plan.End, StepSeconds: plan.StepSeconds, Series: row.Series}, nil
}

// observeProcesses is the single owner authorization, identity, admission and
// completion-time lease check for current process resources.
func (s *ResourceObservationService) observeProcesses(ctx context.Context, principal, token string, ids []uint, start, end time.Time, trend bool) (ProcessResourceSummaryResponse, resourcequery.Plan, error) {
	return s.observeProcessesUsing(ctx, principal, token, ids, start, end, trend, nil, nil)
}

// observeProcessesUsing keeps current-user authorization, private identity,
// source admission, budgets and completion-time lease checks in one path for
// process resources and HTTP quality. Only fixed query planning/reading differs.
func (s *ResourceObservationService) observeProcessesUsing(ctx context.Context, principal, token string, ids []uint, start, end time.Time, trend bool,
	planFor func(int, time.Time, time.Time, time.Time, bool, resourcequery.Budget) (resourcequery.Plan, error),
	read func(context.Context, resourcequery.Plan, []resourcequery.ProcessScope, resourcequery.Budget) (map[uint]resourcequery.Summary, error),
) (ProcessResourceSummaryResponse, resourcequery.Plan, error) {
	result := ProcessResourceSummaryResponse{}
	var plan resourcequery.Plan
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	if _, err := commonmodels.ParseRuntimeInstanceIDs(strings.Join(parts, ",")); err != nil {
		return result, plan, resourcequery.ErrInvalid
	}
	if s == nil || s.process.Users == nil {
		return result, plan, metricsdiscovery.ErrUnconfigured
	}
	release, err := s.limiter.Acquire(principal, resourcequery.DefaultBudget())
	if err != nil {
		return result, plan, err
	}
	defer release()
	cfg, err := s.Policy(ctx)
	if err != nil {
		return result, plan, err
	}
	ctx, budgetCancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer budgetCancel()
	now := s.now().UTC()
	if planFor != nil {
		plan, err = planFor(len(ids), start, end, now, trend, cfg.Budget)
	} else if trend {
		if len(ids) != 1 {
			return result, plan, resourcequery.ErrInvalid
		}
		plan, err = resourcequery.NewProcessTrendPlan(start, end, now, cfg.Budget)
	} else {
		plan, err = resourcequery.NewProcessSummaryPlan(len(ids), now.Truncate(time.Second), cfg.Budget)
	}
	if err != nil {
		return result, plan, err
	}
	if err := s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, plan, err
	}
	// The current human's owner read precedes all machine projections and data.
	references, err := s.process.Users.GetRuntimeInstancesForUser(ctx, ids, token)
	if err != nil {
		return result, plan, err
	}
	if len(references) != len(ids) {
		return result, plan, metricsdiscovery.ErrIdentityUnavailable
	}
	wanted := map[uint]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	users := map[uint]client.RuntimeInstanceReference{}
	for _, r := range references {
		if !wanted[r.ID] || users[r.ID].ID != 0 || r.ProcessMetricsDeclared == nil {
			return result, plan, metricsdiscovery.ErrIdentityUnavailable
		}
		users[r.ID] = r
	}
	if !s.enabled {
		return result, plan, metricsdiscovery.ErrDisabled
	}
	if s.process.Identities == nil || s.policy == nil || s.process.Backend == nil {
		return result, plan, metricsdiscovery.ErrUnconfigured
	}
	snapshot, err := s.process.Identities.GetObservabilityIdentities(ctx)
	if err != nil || snapshot == nil || snapshot.ModuleInstances == nil || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.Before(now.Add(-metricsdiscovery.Timeout)) || snapshot.ObservedAt.After(s.now().UTC().Add(metricsdiscovery.Timeout)) || len(snapshot.ModuleInstances) > commonmodels.ObservabilityIdentityInstanceLimit {
		return result, plan, metricsdiscovery.ErrIdentityUnavailable
	}
	type identity struct{ module, instance string }
	current := map[identity]commonmodels.ObservabilityModuleIdentity{}
	for _, r := range snapshot.ModuleInstances {
		key := identity{r.ModuleName, r.InstanceID}
		if _, duplicate := current[key]; duplicate || !r.LeaseExpiresAt.After(snapshot.ObservedAt) {
			return result, plan, metricsdiscovery.ErrIdentityUnavailable
		}
		current[key] = r
	}
	scopes := []resourcequery.ProcessScope{}
	physical := map[string]bool{}
	leases := map[uint]time.Time{}
	result.Data = make([]ProcessResourceObservation, 0, len(ids))
	for _, id := range ids {
		r := users[id]
		row := ProcessResourceObservation{Subject: ProcessResourceSubject{Kind: "module_instance", ID: id, ModuleName: r.ModuleName, InstanceID: r.InstanceID, Role: r.Role}, NodeID: r.NodeID, PolicyVersion: cfg.Version, LookbackSeconds: resourcequery.LookbackSeconds, QueriedAt: now, Collection: ProcessCollection{State: "not_connected"}, Series: resourcequery.Empty(plan, "not_connected")}
		if r.Status != "up" || !r.LeaseExpiresAt.After(now) {
			row.Collection.State = "not_active"
			row.Series = resourcequery.Empty(plan, "not_active")
		} else if *r.ProcessMetricsDeclared {
			owner, active := current[identity{r.ModuleName, r.InstanceID}]
			if active && owner.ProcessMetrics != nil {
				if owner.ProcessMetrics.Validate() != nil || owner.Role != r.Role || owner.NodeID != r.NodeID || owner.ProcessStartedAt == nil || r.ProcessStartedAt == nil || owner.ProcessStartedAt.IsZero() || owner.ProcessStartedAt.After(snapshot.ObservedAt) || !owner.ProcessStartedAt.Truncate(time.Microsecond).Equal(r.ProcessStartedAt.Truncate(time.Microsecond)) {
					return result, plan, metricsdiscovery.ErrIdentityUnavailable
				}
				resolved, err := s.policy.Resolve(ctx, owner.ProcessMetrics.Endpoint)
				if err != nil {
					return result, plan, err
				}
				if physical[resolved.Address] {
					return result, plan, resourcequery.ErrUnavailable
				}
				physical[resolved.Address] = true
				scope := resourcequery.ProcessScope{ID: id, ModuleName: r.ModuleName, InstanceID: r.InstanceID, Role: r.Role, StartedAt: *owner.ProcessStartedAt, Instance: resolved.Address}
				if scope.Validate() != nil {
					return result, plan, metricsdiscovery.ErrIdentityUnavailable
				}
				if trend {
					plan = resourcequery.LimitTrendStart(plan, scope.StartedAt)
				}
				scopes = append(scopes, scope)
				lease := owner.LeaseExpiresAt
				if r.LeaseExpiresAt.Before(lease) {
					lease = r.LeaseExpiresAt
				}
				leases[id] = lease
			}
		}
		result.Data = append(result.Data, row)
	}
	if len(scopes) > 0 {
		var observed map[uint]resourcequery.Summary
		if read != nil {
			observed, err = read(ctx, plan, scopes, cfg.Budget)
		} else if trend {
			var series []resourcequery.Series
			series, err = s.process.Backend.ProcessTrend(ctx, plan, scopes[0], cfg.Budget)
			observed = map[uint]resourcequery.Summary{scopes[0].ID: {Series: series}}
		} else {
			observed, err = s.process.Backend.ProcessSummaries(ctx, scopes, plan.End, cfg.Budget)
		}
		if err != nil {
			return result, plan, err
		}
		if len(observed) != len(scopes) {
			return result, plan, resourcequery.ErrUnavailable
		}
		for i := range result.Data {
			row := &result.Data[i]
			if _, active := leases[row.Subject.ID]; !active {
				continue
			}
			data, ok := observed[row.Subject.ID]
			if !ok {
				return result, plan, resourcequery.ErrUnavailable
			}
			row.Series = data.Series
			if !trend || read != nil {
				row.Collection = ProcessCollection{State: data.Collection.State, SampledAt: data.Collection.SampledAt}
			}
		}
	}
	if ctx.Err() != nil {
		return result, plan, ctx.Err()
	}
	completed := s.now().UTC()
	if completed.Before(now) || snapshot.ObservedAt.Before(completed.Add(-metricsdiscovery.Timeout)) {
		return result, plan, resourcequery.ErrUnavailable
	}
	for i := range result.Data {
		row := &result.Data[i]
		row.QueriedAt = completed
		if lease, ok := leases[row.Subject.ID]; ok && !lease.After(completed) {
			row.Collection = ProcessCollection{State: "not_active"}
			row.Series = resourcequery.Empty(plan, "not_active")
			continue
		}
		if trend && read == nil {
			continue
		}
		// Reuse the same completion-time freshness check as host observations.
		adapted := ResourceObservationResponse{Series: row.Series, QueriedAt: completed, Collection: &resourcequery.Collection{State: row.Collection.State, SampledAt: row.Collection.SampledAt}}
		markCurrentFreshness(&adapted)
		row.Series = adapted.Series
		row.Collection.State = adapted.Collection.State

	}
	return result, plan, nil
}
