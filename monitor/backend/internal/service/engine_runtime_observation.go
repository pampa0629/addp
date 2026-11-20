package service

import (
	"context"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type EngineRuntimeBackend interface {
	EngineRuntimeQuery(context.Context, resourcequery.Plan, resourcequery.RuntimeMetricSource, resourcequery.Budget) (resourcequery.Summary, error)
}

type EngineRuntimeDependencies struct {
	Identities metricsdiscovery.IdentityReader
	Backend    EngineRuntimeBackend
}

func (s *ResourceObservationService) WithEngineRuntime(deps EngineRuntimeDependencies) *ResourceObservationService {
	s.engineRuntime = deps
	return s
}

type EngineRuntimeObservation struct {
	Source         string                 `json:"source"`
	DiscoveryState string                 `json:"discovery_state"`
	ID             string                 `json:"id,omitempty"`
	StartedAt      *time.Time             `json:"started_at,omitempty"`
	Collection     ProcessCollection      `json:"collection"`
	Series         []resourcequery.Series `json:"series"`
	QueriedAt      time.Time              `json:"queried_at"`
	Start          time.Time              `json:"start"`
	End            time.Time              `json:"end"`
	StepSeconds    int64                  `json:"step_seconds"`
}

type engineRuntimeIdentity struct {
	Source    resourcequery.RuntimeMetricSource
	Endpoint  string
	Discovery string
	State     string
}

type RuntimeObservationSubject struct {
	Source   string `json:"source"`
	EngineID uint   `json:"engine_id"`
}

func (s *ResourceObservationService) RuntimeObservationSubjects(ctx context.Context) ([]RuntimeObservationSubject, error) {
	if s == nil || s.engineRuntime.Identities == nil {
		return nil, metricsdiscovery.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
	defer cancel()
	snapshot, err := s.engineRuntime.Identities.GetObservabilityIdentities(ctx)
	now := s.now().UTC()
	if err != nil || snapshot == nil || snapshot.ObservedAt.Before(now.Add(-metricsdiscovery.Timeout)) || snapshot.ObservedAt.After(now.Add(metricsdiscovery.Timeout)) {
		return nil, metricsdiscovery.ErrIdentityUnavailable
	}
	result := make([]RuntimeObservationSubject, 0, len(snapshot.RuntimeMetrics))
	for _, runtime := range snapshot.RuntimeMetrics {
		if runtime.LeaseExpiresAt.After(now) {
			result = append(result, RuntimeObservationSubject{runtime.Kind, runtime.EngineID})
		}
	}
	return result, nil
}

func (s *ResourceObservationService) engineRuntimeIdentity(ctx context.Context, source string, engineID uint) (engineRuntimeIdentity, error) {
	snapshot, err := s.engineRuntime.Identities.GetObservabilityIdentities(ctx)
	now := s.now().UTC()
	if err != nil || snapshot == nil || snapshot.ObservedAt.Before(now.Add(-metricsdiscovery.Timeout)) || snapshot.ObservedAt.After(now.Add(metricsdiscovery.Timeout)) {
		return engineRuntimeIdentity{}, metricsdiscovery.ErrIdentityUnavailable
	}
	result := engineRuntimeIdentity{Source: resourcequery.RuntimeMetricSource{Kind: source}, Discovery: "available", State: "not_connected"}
	if source == "meilisearch" {
		components := snapshot.InfrastructureComponents
		if components == nil {
			result.Discovery = "unavailable"
			return result, nil
		}
		if components.Validate(snapshot.ObservedAt) != nil {
			return engineRuntimeIdentity{}, metricsdiscovery.ErrIdentityUnavailable
		}
		result.Discovery = components.State
		for _, component := range components.Items {
			if component.Kind == source {
				result.Source.ID, result.Source.StartedAt = component.ID(), component.StartedAt.UTC()
				result.Source.ContainerID = component.ContainerID
				if component.Metrics.State == "available" {
					result.Endpoint = component.Metrics.Endpoint
					result.State = "no_sample"
				} else {
					result.State = "source_unavailable"
				}
			}
		}
	} else {
		for _, runtime := range snapshot.RuntimeMetrics {
			if runtime.Kind != source || runtime.EngineID != engineID || !runtime.LeaseExpiresAt.After(now) {
				continue
			}
			if result.Source.EngineID != 0 {
				return engineRuntimeIdentity{}, metricsdiscovery.ErrIdentityUnavailable
			}
			if runtime.EngineID == 0 || runtime.RuntimeMetricsDeclaration.Validate(snapshot.ObservedAt) != nil ||
				runtime.LeaseExpiresAt.After(snapshot.ObservedAt.Add(commonmodels.RuntimeMetricsLeaseDuration)) {
				return engineRuntimeIdentity{}, metricsdiscovery.ErrIdentityUnavailable
			}
			result.Source.ID, result.Source.EngineID, result.Source.StartedAt = runtime.InstanceID, runtime.EngineID, runtime.StartedAt.UTC()
			result.Endpoint = runtime.Endpoint
			result.State = "no_sample"
		}
	}
	return result, nil
}

func (s *ResourceObservationService) EngineRuntime(ctx context.Context, principal, source, id, metric string, engineID uint, start, end time.Time) (EngineRuntimeObservation, error) {
	var result EngineRuntimeObservation
	if s == nil || s.engineRuntime.Identities == nil || s.engineRuntime.Backend == nil || s.policy == nil {
		return result, metricsdiscovery.ErrUnconfigured
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
	ctx, budgetCancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer budgetCancel()
	now := s.now().UTC()
	plan, err := resourcequery.NewRuntimeMetricPlan(source, metric, start, end, now, cfg.Budget)
	if err != nil {
		return result, err
	}
	if err := s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, err
	}
	if !s.enabled {
		return result, metricsdiscovery.ErrDisabled
	}
	current, err := s.engineRuntimeIdentity(ctx, source, engineID)
	if err != nil {
		return result, err
	}
	if plan.Trend {
		if current.Source.ID != id {
			return result, resourcequery.ErrInvalid
		}
		plan = resourcequery.LimitTrendStart(plan, current.Source.StartedAt)
	}
	result = EngineRuntimeObservation{
		Source: source, DiscoveryState: current.Discovery, ID: current.Source.ID,
		Collection: ProcessCollection{State: current.State}, Series: []resourcequery.Series{},
		Start: plan.Start, End: plan.End, StepSeconds: plan.StepSeconds,
	}
	if current.Source.ID != "" {
		result.StartedAt = &current.Source.StartedAt
		result.Series = resourcequery.Empty(plan, current.State)
	}
	if current.Endpoint != "" {
		resolved, err := s.policy.Resolve(ctx, current.Endpoint)
		if err != nil {
			if ctx.Err() != nil {
				return EngineRuntimeObservation{}, ctx.Err()
			}
			result.Collection.State = "source_unavailable"
			result.Series = resourcequery.Empty(plan, result.Collection.State)
		} else if !plan.End.Before(current.Source.StartedAt) {
			scope := current.Source
			scope.Instance = resolved.Address
			metrics, err := s.engineRuntime.Backend.EngineRuntimeQuery(ctx, plan, scope, cfg.Budget)
			if err != nil {
				return EngineRuntimeObservation{}, err
			}
			result.Series = metrics.Series
			if !plan.Trend {
				result.Collection = ProcessCollection{State: metrics.Collection.State, SampledAt: metrics.Collection.SampledAt}
			}
		}
	}
	latest, err := s.engineRuntimeIdentity(ctx, source, engineID)
	if err != nil {
		return EngineRuntimeObservation{}, err
	}
	if current != latest {
		return EngineRuntimeObservation{}, metricsdiscovery.ErrIdentityUnavailable
	}
	policy, err := s.Policy(ctx)
	if err != nil {
		return EngineRuntimeObservation{}, err
	}
	if policy.Version != cfg.Version {
		return EngineRuntimeObservation{}, resourcequery.ErrConflict
	}
	if ctx.Err() != nil {
		return EngineRuntimeObservation{}, ctx.Err()
	}
	result.QueriedAt = s.now().UTC()
	if result.QueriedAt.Before(now) {
		return EngineRuntimeObservation{}, resourcequery.ErrUnavailable
	}
	if !plan.Trend {
		collection := resourcequery.Collection{State: result.Collection.State, SampledAt: result.Collection.SampledAt}
		adapted := ResourceObservationResponse{Series: result.Series, Collection: &collection, QueriedAt: result.QueriedAt}
		markCurrentFreshness(&adapted)
		result.Series = adapted.Series
		result.Collection = ProcessCollection{State: collection.State, SampledAt: collection.SampledAt}
	}
	return result, nil
}
