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
	Expected          int                              `json:"expected"`
	ContainerExpected int                              `json:"container_expected"`
	WorkingSet        resourcequery.ProcessMetricTotal `json:"working_set"`
}

type ResourceRoles struct {
	Backend  ServiceResourceTotal `json:"backend"`
	Frontend ServiceResourceTotal `json:"frontend"`
	Worker   ServiceResourceTotal `json:"worker"`
}
type ModuleResourceTotal struct {
	ServiceResourceTotal
	ResourceRoles
	ModuleName string `json:"module_name"`
}

type ServiceResourceTotals struct {
	Total   ServiceResourceTotal  `json:"total"`
	Roles   ResourceRoles         `json:"roles"`
	Modules []ModuleResourceTotal `json:"modules"`
	// Omitted from the observation scope when the caller lacks host read access.
	UnknownFrontendNodes *int      `json:"unknown_frontend_nodes"`
	QueriedAt            time.Time `json:"queried_at"`
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

func (s *ResourceObservationService) ProcessTotals(ctx context.Context, principal, token string, includeFrontend bool) (ServiceResourceTotals, error) {
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
	moduleNames := map[string]bool{}
	physical := map[string]bool{}
	scopes := []resourcequery.ProcessScope{}
	declared := 0
	for _, instance := range members {
		moduleNames[instance.Module] = true
		expected[resourcequery.ProcessTotalGroup(instance.Module, instance.Role)]++
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
	frontends := map[string]*ServiceResourceTotal{}
	if includeFrontend {
		if s.frontend == nil {
			return result, metricsdiscovery.ErrUnconfigured
		}
		sources, err := s.frontendSources(ctx, token)
		if err != nil {
			return result, err
		}
		unknown := 0
		for id, source := range sources {
			if !source.Target.Enabled || !source.Node.Enabled {
				unknown++
				continue
			}
			resolved, err := s.policy.Resolve(ctx, source.Target.Source.Endpoint)
			if err != nil {
				return result, err
			}
			rows, err := s.frontend.FrontendSamples(ctx, id, resolved.Address, source.Target.FrontendRevision(source.Node.Version), at, cfg.Budget)
			if err != nil {
				return result, err
			}
			if len(rows) == 0 {
				unknown++
				continue
			}
			for _, sample := range rows {
				moduleNames[sample.Module] = true
				row := frontends[sample.Module]
				if row == nil {
					row = &ServiceResourceTotal{}
					frontends[sample.Module] = row
				}
				row.Expected++
				memory := &row.RSS
				if sample.Mode == "compose" {
					row.ContainerExpected++
					memory = &row.WorkingSet
				}
				addProcessTotal(&row.CPU, resourcequery.ProcessMetricTotal{Value: sample.CPUCores, Valid: 1})
				addProcessTotal(memory, resourcequery.ProcessMetricTotal{Value: sample.MemoryBytes, Valid: 1})
			}
		}
		latest, err := s.frontendSources(ctx, token)
		if err != nil {
			return result, err
		}
		if !maps.Equal(sources, latest) {
			return result, resourcequery.ErrConflict
		}
		result.UnknownFrontendNodes = &unknown
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
	result.Modules = []ModuleResourceTotal{}
	for _, module := range slices.Sorted(maps.Keys(moduleNames)) {
		row := ModuleResourceTotal{ModuleName: module}
		for role, part := range map[string]*ServiceResourceTotal{"backend": &row.Backend, "worker": &row.Worker} {
			key := resourcequery.ProcessTotalGroup(module, role)
			*part = ServiceResourceTotal{ProcessTotals: observed[key], Expected: expected[key]}
		}
		if frontend := frontends[module]; frontend != nil {
			row.Frontend = *frontend
		}
		row.merge(row.Backend)
		row.merge(row.Frontend)
		row.merge(row.Worker)
		result.Roles.Backend.merge(row.Backend)
		result.Roles.Frontend.merge(row.Frontend)
		result.Roles.Worker.merge(row.Worker)
		result.Modules = append(result.Modules, row)
		result.Total.merge(row.ServiceResourceTotal)
	}
	return result, nil
}

func (t *ServiceResourceTotal) merge(row ServiceResourceTotal) {
	t.Expected += row.Expected
	t.ContainerExpected += row.ContainerExpected
	addProcessTotal(&t.CPU, row.CPU)
	addProcessTotal(&t.RSS, row.RSS)
	addProcessTotal(&t.WorkingSet, row.WorkingSet)
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
