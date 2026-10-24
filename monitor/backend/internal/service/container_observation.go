package service

import (
	"context"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
	"reflect"
	"time"
)

type ContainerResourceBackend interface {
	ContainerObservation(context.Context, resourcequery.ProcessScope, time.Time, resourcequery.Budget) (resourcequery.Summary, error)
}

// ContainerResourceResponse is deliberately free of deployment/private identity.
type ContainerResourceResponse struct {
	Observation       ProcessResourceObservation `json:"observation"`
	ProofState        string                     `json:"proof_state"`
	ProofObservedAt   *time.Time                 `json:"proof_observed_at"`
	CPUUpperCores     *float64                   `json:"cpu_upper_cores"`
	MemoryUpperBytes  *uint64                    `json:"memory_upper_bytes"`
	CPUWindowVerified bool                       `json:"cpu_window_verified"`
}

func (s *ResourceObservationService) ContainerObservation(ctx context.Context, principal, token string, id uint) (ContainerResourceResponse, error) {
	result := ContainerResourceResponse{ProofState: "unverified"}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	scopes := []resourcequery.ProcessScope{}
	read := func(ctx context.Context, plan resourcequery.Plan, current []resourcequery.ProcessScope, b resourcequery.Budget) (map[uint]resourcequery.Summary, error) {
		backend, ok := s.process.Backend.(ContainerResourceBackend)
		if !ok {
			return nil, metricsdiscovery.ErrUnconfigured
		}
		scopes = current
		scope := current[0]
		at := plan.End
		if scope.Container != nil {
			if scope.Container.Validate(plan.End) != nil || scope.Cgroup == nil || scope.Container.Identity != *scope.Cgroup {
				return nil, metricsdiscovery.ErrIdentityUnavailable
			}
			// Evaluate against the latest verified configuration instant, not against a
			// newer interval for which the deployment publisher has provided no proof.
			at = scope.Container.ObservedAt.Truncate(time.Second)
			p := scope.Container
			result.ProofState = "verified"
			result.ProofObservedAt = &p.ObservedAt
			result.CPUUpperCores = p.CPUCores
			result.MemoryUpperBytes = p.MemoryBytes
			result.CPUWindowVerified = p.CPUCores != nil && !p.StableSince.After(at.Add(-76*time.Second))
		}
		value, err := backend.ContainerObservation(ctx, scope, at, b)
		return map[uint]resourcequery.Summary{scope.ID: value}, err
	}
	rows, _, err := s.observeProcessesUsing(ctx, principal, token, []uint{id}, time.Time{}, time.Time{}, false, resourcequery.NewContainerPlan, read)
	if err != nil {
		return ContainerResourceResponse{}, err
	}
	if len(rows.Data) != 1 {
		return ContainerResourceResponse{}, resourcequery.ErrUnavailable
	}
	result.Observation = rows.Data[0]
	if len(scopes) == 0 {
		return result, nil
	}
	// Re-read the same System owner projection after querying. Never return a
	// ratio if its declaration, container/limit epoch or live lease changed.
	latest, err := s.process.Identities.GetObservabilityIdentities(ctx)
	if err != nil || latest == nil {
		return ContainerResourceResponse{}, metricsdiscovery.ErrIdentityUnavailable
	}
	at := s.now().UTC()
	scope := scopes[0]
	matched := 0
	for _, i := range latest.ModuleInstances {
		if i.ModuleName != scope.ModuleName || i.InstanceID != scope.InstanceID {
			continue
		}
		if i.Role != scope.Role || i.NodeID != result.Observation.NodeID || !i.LeaseExpiresAt.After(at) || i.ProcessStartedAt == nil || !i.ProcessStartedAt.Equal(scope.StartedAt) || !i.ProcessMetrics.Equal(scope.Declaration) || !reflect.DeepEqual(i.ProcessMetrics.Cgroup, scope.Cgroup) || !reflect.DeepEqual(i.Container, scope.Container) {
			return ContainerResourceResponse{}, metricsdiscovery.ErrIdentityUnavailable
		}
		if i.Container != nil && i.Container.Validate(at) != nil {
			return ContainerResourceResponse{}, metricsdiscovery.ErrIdentityUnavailable
		}
		matched++
	}
	if matched != 1 || ctx.Err() != nil || latest.ObservedAt.After(at.Add(metricsdiscovery.Timeout)) || latest.ObservedAt.Before(at.Add(-commonmodels.ObservabilityIdentityTimeout)) {
		return ContainerResourceResponse{}, metricsdiscovery.ErrIdentityUnavailable
	}
	result.Observation.QueriedAt = at
	adapted := ResourceObservationResponse{Series: result.Observation.Series, QueriedAt: at, Collection: &resourcequery.Collection{State: result.Observation.Collection.State, SampledAt: result.Observation.Collection.SampledAt}}
	markCurrentFreshness(&adapted)
	result.Observation.Series = adapted.Series
	result.Observation.Collection.State = adapted.Collection.State
	return result, nil
}
