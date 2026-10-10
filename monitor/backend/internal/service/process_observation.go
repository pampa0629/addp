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

func (s *ResourceObservationService) ProcessSummaries(ctx context.Context, principal, token string, ids []uint) (ProcessResourceSummaryResponse, error) {
	result := ProcessResourceSummaryResponse{}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	if _, err := commonmodels.ParseRuntimeInstanceIDs(strings.Join(parts, ",")); err != nil {
		return result, resourcequery.ErrInvalid
	}
	if s == nil || s.process.Users == nil {
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
	plan, err := resourcequery.NewProcessSummaryPlan(len(ids), now.Truncate(time.Second), cfg.Budget)
	if err != nil {
		return result, err
	}
	if err := s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, err
	}
	// The current human's owner read precedes all machine projections and data.
	references, err := s.process.Users.GetRuntimeInstancesForUser(ctx, ids, token)
	if err != nil {
		return result, err
	}
	if len(references) != len(ids) {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	wanted := map[uint]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	users := map[uint]client.RuntimeInstanceReference{}
	for _, r := range references {
		if !wanted[r.ID] || users[r.ID].ID != 0 || r.ProcessMetricsDeclared == nil {
			return result, metricsdiscovery.ErrIdentityUnavailable
		}
		users[r.ID] = r
	}
	if !s.enabled {
		return result, metricsdiscovery.ErrDisabled
	}
	if s.process.Identities == nil || s.policy == nil || s.process.Backend == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	snapshot, err := s.process.Identities.GetObservabilityIdentities(ctx)
	if err != nil || snapshot == nil || snapshot.ModuleInstances == nil || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.Before(now.Add(-metricsdiscovery.Timeout)) || snapshot.ObservedAt.After(s.now().UTC().Add(metricsdiscovery.Timeout)) || len(snapshot.ModuleInstances) > commonmodels.ObservabilityIdentityInstanceLimit {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	type identity struct{ module, instance string }
	current := map[identity]commonmodels.ObservabilityModuleIdentity{}
	for _, r := range snapshot.ModuleInstances {
		key := identity{r.ModuleName, r.InstanceID}
		if _, duplicate := current[key]; duplicate || !r.LeaseExpiresAt.After(snapshot.ObservedAt) {
			return result, metricsdiscovery.ErrIdentityUnavailable
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
					return result, metricsdiscovery.ErrIdentityUnavailable
				}
				resolved, err := s.policy.Resolve(ctx, owner.ProcessMetrics.Endpoint)
				if err != nil {
					return result, err
				}
				if physical[resolved.Address] {
					return result, resourcequery.ErrUnavailable
				}
				physical[resolved.Address] = true
				scope := resourcequery.ProcessScope{ID: id, ModuleName: r.ModuleName, InstanceID: r.InstanceID, Role: r.Role, StartedAt: *owner.ProcessStartedAt, Instance: resolved.Address}
				if scope.Validate() != nil {
					return result, metricsdiscovery.ErrIdentityUnavailable
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
		observed, err := s.process.Backend.ProcessSummaries(ctx, scopes, plan.End, cfg.Budget)
		if err != nil {
			return result, err
		}
		if len(observed) != len(scopes) {
			return result, resourcequery.ErrUnavailable
		}
		for i := range result.Data {
			row := &result.Data[i]
			if _, active := leases[row.Subject.ID]; !active {
				continue
			}
			data, ok := observed[row.Subject.ID]
			if !ok {
				return result, resourcequery.ErrUnavailable
			}
			row.Series = data.Series
			row.Collection = ProcessCollection{State: data.Collection.State, SampledAt: data.Collection.SampledAt}
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	completed := s.now().UTC()
	if completed.Before(now) || snapshot.ObservedAt.Before(completed.Add(-metricsdiscovery.Timeout)) {
		return result, resourcequery.ErrUnavailable
	}
	for i := range result.Data {
		row := &result.Data[i]
		row.QueriedAt = completed
		if lease, ok := leases[row.Subject.ID]; ok && !lease.After(completed) {
			row.Collection = ProcessCollection{State: "not_active"}
			row.Series = resourcequery.Empty(plan, "not_active")
			continue
		}
		// Reuse the same completion-time freshness check as host observations.
		adapted := ResourceObservationResponse{Series: row.Series, QueriedAt: completed, Collection: &resourcequery.Collection{State: row.Collection.State, SampledAt: row.Collection.SampledAt}}
		markCurrentFreshness(&adapted)
		row.Series = adapted.Series
		row.Collection.State = adapted.Collection.State

	}
	return result, nil
}
