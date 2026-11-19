package service

import (
	"context"
	"sort"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/resourcequery"
)

type AutomaticCollectionStore interface {
	Snapshot(context.Context) (map[string]models.AutomaticCollectionScope, error)
	Save(context.Context, metricsdiscovery.AutomaticScope, bool, int64) (models.AutomaticCollectionScope, error)
	Paused(context.Context) (map[string]bool, error)
}

type CollectionTargetReader interface {
	ActiveCollectionTargets(context.Context, time.Time, int) (resourcequery.ActiveCollectionTargets, error)
}

type AutomaticCollectionRow struct {
	models.AutomaticCollectionScope
	Observation   string `json:"observation"`
	ActiveTargets *int   `json:"active_targets,omitempty"`
}

type AutomaticCollectionList struct {
	Data             []AutomaticCollectionRow `json:"data"`
	CatalogAvailable bool                     `json:"catalog_available"`
}

type AutomaticCollectionInput struct {
	Enabled *bool `json:"enabled" binding:"required"`
	Version int64 `json:"version" binding:"required,min=1"`
}

func (s *MonitoringTargetService) WithAutomaticCollection(store AutomaticCollectionStore, reader CollectionTargetReader) *MonitoringTargetService {
	s.automatic, s.activeTargets = store, reader
	s.projector.WithCollectionPauses(store.Paused)
	return s
}

func (s *MonitoringTargetService) automaticCatalog(ctx context.Context) (map[string]metricsdiscovery.AutomaticScope, error) {
	result := map[string]metricsdiscovery.AutomaticScope{}
	now := time.Now().UTC()
	current, err := s.identities.GetObservabilityIdentities(ctx)
	if err != nil || current == nil || current.ObservedAt.Before(now.Add(-metricsdiscovery.Timeout)) || current.ObservedAt.After(now.Add(metricsdiscovery.Timeout)) {
		return nil, metricsdiscovery.ErrIdentityUnavailable
	}
	for _, instance := range current.ModuleInstances {
		if instance.ProcessMetrics != nil && instance.LeaseExpiresAt.After(now) {
			if scope := metricsdiscovery.ProcessCollectionScope(instance); scope.ID != "" {
				result[scope.ID] = scope
			}
		}
	}
	if components := current.InfrastructureComponents; components != nil {
		for _, component := range components.Items {
			if scope := metricsdiscovery.ComponentCollectionScope(component); scope.ID != "" {
				result[scope.ID] = scope
			}
		}
	}
	if geo := current.GeoPythonMetrics; geo != nil && geo.LeaseExpiresAt.After(now) {
		if scope := metricsdiscovery.RuntimeCollectionScope(geo); scope.ID != "" {
			result[scope.ID] = scope
		}
	}
	return result, nil
}

func (s *MonitoringTargetService) AutomaticScopes(ctx context.Context, token string) (AutomaticCollectionList, error) {
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
	defer cancel()
	var result AutomaticCollectionList
	if s == nil || s.automatic == nil || s.nodes == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	if err := s.nodes.AuthorizeHostNodesForUser(ctx, token); err != nil {
		return result, err
	}
	saved, err := s.automatic.Snapshot(ctx)
	if err != nil {
		return result, err
	}
	catalog, catalogErr := s.automaticCatalog(ctx)
	result.CatalogAvailable = catalogErr == nil
	for id, scope := range catalog {
		if _, ok := saved[id]; !ok {
			saved[id] = models.AutomaticCollectionScope{AutomaticScope: scope, Enabled: true, Version: 1}
		}
	}
	if len(saved) > metricsdiscovery.AutomaticScopeLimit {
		return result, metricsdiscovery.ErrBudgetExceeded
	}
	var targets resourcequery.ActiveCollectionTargets
	if s.enabled && s.policy != nil && s.activeTargets != nil && len(saved) > 0 {
		targets, _ = s.activeTargets.ActiveCollectionTargets(ctx, time.Now().UTC(), s.policy.ExternalTargetLimit())
	}
	result.Data = make([]AutomaticCollectionRow, 0, len(saved))
	for _, scope := range saved {
		row := AutomaticCollectionRow{AutomaticCollectionScope: scope, Observation: "unknown"}
		observations := targets.Scopes[scope.ID]
		if targets.Complete || len(observations) > 0 {
			count := len(observations)
			row.ActiveTargets = &count
			switch {
			case !scope.Enabled && count == 0:
				row.Observation = "stopped"
			case !scope.Enabled:
				row.Observation = "stopping"
			default:
				row.Observation = "waiting"
				if count > 0 {
					fresh := 0
					for _, sampled := range observations {
						if sampled.After(scope.ChangedAt) {
							fresh++
						}
					}
					if fresh == count {
						row.Observation = "collecting"
					} else if fresh > 0 {
						row.Observation = "partial"
					}
				}
			}
		}
		result.Data = append(result.Data, row)
	}
	sort.Slice(result.Data, func(i, j int) bool { return result.Data[i].ID < result.Data[j].ID })
	return result, nil
}

func (s *MonitoringTargetService) SaveAutomaticScope(ctx context.Context, token, id string, input AutomaticCollectionInput) (models.AutomaticCollectionScope, error) {
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
	defer cancel()
	var result models.AutomaticCollectionScope
	if input.Enabled == nil || input.Version < 1 || input.Version >= 9007199254740991 || len(id) > 128 {
		return result, metricsdiscovery.ErrInvalidTarget
	}
	if s == nil || s.automatic == nil || s.nodes == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	if err := s.nodes.AuthorizeHostNodesForUser(ctx, token); err != nil {
		return result, err
	}
	saved, err := s.automatic.Snapshot(ctx)
	if err != nil {
		return result, err
	}
	scope := saved[id].AutomaticScope
	if scope.ID == "" {
		catalog, err := s.automaticCatalog(ctx)
		if err != nil {
			return result, err
		}
		scope = catalog[id]
		if scope.ID == "" {
			return result, repository.ErrTargetNotFound
		}
	}
	return s.automatic.Save(ctx, scope, *input.Enabled, input.Version)
}
