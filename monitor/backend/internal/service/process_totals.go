package service

import (
	"context"
	"maps"
	"slices"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type ProcessTotalsBackend interface {
	ProcessResourceTotals(context.Context, []resourcequery.ProcessScope, int, time.Time, resourcequery.Budget) (map[string]resourcequery.ProcessTotals, error)
}

type ProcessTotalDependencies struct {
	Users   UserModuleReader
	Backend ProcessTotalsBackend
}

func (s *ResourceObservationService) WithProcessTotals(deps ProcessTotalDependencies) *ResourceObservationService {
	s.processTotals = deps
	return s
}

type ServiceResourceTotal struct {
	resourcequery.ProcessTotals
	Expected int `json:"expected"`
}

type ModuleResourceTotal struct {
	ServiceResourceTotal
	ModuleName string `json:"module_name"`
}

type ServiceResourceTotals struct {
	Total     ServiceResourceTotal  `json:"total"`
	Modules   []ModuleResourceTotal `json:"modules"`
	QueriedAt time.Time             `json:"queried_at"`
}

type processTotalsMembership struct {
	Module, Instance, Role, Endpoint string
	StartedAt                        time.Time
}

func (s *ResourceObservationService) currentProcessTotals(ctx context.Context, token string) (map[string]int64, map[string]processTotalsMembership, error) {
	modules, err := s.processTotals.Users.GetModulesForUser(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	versions := make(map[string]int64, len(modules))
	for _, module := range modules {
		versions[module.ModuleName] = module.Version
	}
	snapshot, err := s.process.Identities.GetObservabilityIdentities(ctx)
	now := s.now().UTC()
	if err != nil || snapshot == nil || snapshot.ModuleInstances == nil || len(snapshot.ModuleInstances) > commonmodels.ObservabilityIdentityInstanceLimit ||
		snapshot.ObservedAt.Before(now.Add(-metricsdiscovery.Timeout)) || snapshot.ObservedAt.After(now.Add(metricsdiscovery.Timeout)) {
		return nil, nil, metricsdiscovery.ErrIdentityUnavailable
	}
	result := make(map[string]processTotalsMembership)
	for _, instance := range snapshot.ModuleInstances {
		if _, allowed := versions[instance.ModuleName]; !allowed || !instance.LeaseExpiresAt.After(now) {
			continue
		}
		if instance.InstanceID == "" || result[instance.InstanceID].Instance != "" {
			return nil, nil, metricsdiscovery.ErrIdentityUnavailable
		}
		value := processTotalsMembership{Module: instance.ModuleName, Instance: instance.InstanceID, Role: instance.Role}
		if instance.ProcessMetrics != nil {
			if instance.ProcessMetrics.Validate() != nil || instance.ProcessStartedAt == nil || instance.ProcessStartedAt.IsZero() ||
				instance.ProcessStartedAt.After(snapshot.ObservedAt) {
				return nil, nil, metricsdiscovery.ErrIdentityUnavailable
			}
			value.Endpoint, value.StartedAt = instance.ProcessMetrics.Endpoint, instance.ProcessStartedAt.UTC()
		}
		result[instance.InstanceID] = value
	}
	return versions, result, nil
}

func (s *ResourceObservationService) ProcessTotals(ctx context.Context, principal, token string) (ServiceResourceTotals, error) {
	var result ServiceResourceTotals
	if s == nil || s.processTotals.Users == nil || s.processTotals.Backend == nil || s.process.Identities == nil || s.policy == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	if !s.enabled {
		return result, metricsdiscovery.ErrDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
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
	if err := s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, err
	}
	ctx, budgetCancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer budgetCancel()
	versions, members, err := s.currentProcessTotals(ctx, token)
	if err != nil {
		return result, err
	}
	expected := map[string]int{}
	physical := map[string]bool{}
	scopes := []resourcequery.ProcessScope{}
	declared := 0
	for _, instance := range members {
		expected[instance.Module]++
		if instance.Endpoint == "" {
			continue
		}
		declared++
		if declared > s.policy.ExternalTargetLimit() {
			return result, resourcequery.ErrBudget
		}
		resolved, err := s.policy.Resolve(ctx, instance.Endpoint)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			continue
		}
		if physical[resolved.Address] {
			return result, metricsdiscovery.ErrIdentityUnavailable
		}
		physical[resolved.Address] = true
		scopes = append(scopes, resourcequery.ProcessScope{ID: uint(declared), ModuleName: instance.Module, InstanceID: instance.Instance, Role: instance.Role, StartedAt: instance.StartedAt, Instance: resolved.Address})
	}
	at := s.now().UTC().Truncate(time.Second)
	observed, err := s.processTotals.Backend.ProcessResourceTotals(ctx, scopes, len(expected), at, cfg.Budget)
	if err != nil {
		return result, err
	}
	latestVersions, latestMembers, err := s.currentProcessTotals(ctx, token)
	if err != nil {
		return result, err
	}
	if !maps.Equal(versions, latestVersions) || !maps.Equal(members, latestMembers) {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	policy, err := s.Policy(ctx)
	if err != nil {
		return result, err
	}
	if policy.Version != cfg.Version {
		return result, resourcequery.ErrConflict
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	result.QueriedAt = at
	result.Modules = make([]ModuleResourceTotal, 0, len(expected))
	for _, module := range slices.Sorted(maps.Keys(expected)) {
		read := observed[module]
		row := ServiceResourceTotal{ProcessTotals: read, Expected: expected[module]}
		result.Modules = append(result.Modules, ModuleResourceTotal{ServiceResourceTotal: row, ModuleName: module})
		result.Total.Expected += row.Expected
		addProcessTotal(&result.Total.CPU, row.CPU)
		addProcessTotal(&result.Total.RSS, row.RSS)
	}
	return result, nil
}

func addProcessTotal(total *resourcequery.ProcessMetricTotal, value resourcequery.ProcessMetricTotal) {
	if value.Value == nil {
		return
	}
	if total.Value == nil {
		total.Value = new(float64)
	}
	*total.Value += *value.Value
	total.Valid += value.Valid
}
