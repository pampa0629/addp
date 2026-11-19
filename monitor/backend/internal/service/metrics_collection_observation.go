package service

import (
	"context"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type CollectionHealthBackend interface {
	CollectionHealth(context.Context, time.Time, resourcequery.Budget) (resourcequery.MetricsCollectionHealth, error)
}

func (s *ResourceObservationService) WithCollectionHealth(backend CollectionHealthBackend) *ResourceObservationService {
	s.collectionHealth = backend
	return s
}

type CollectionHealthObservation struct {
	resourcequery.MetricsCollectionHealth
	TargetLimit int       `json:"target_limit"`
	QueriedAt   time.Time `json:"queried_at"`
}

func (s *ResourceObservationService) CollectionHealth(ctx context.Context, principal string) (CollectionHealthObservation, error) {
	var result CollectionHealthObservation
	if s == nil || s.collectionHealth == nil || s.policy == nil {
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
	at := s.now().UTC().Truncate(time.Second)
	health, err := s.collectionHealth.CollectionHealth(ctx, at, cfg.Budget)
	if err != nil {
		return result, err
	}
	return CollectionHealthObservation{MetricsCollectionHealth: health, TargetLimit: s.policy.ExternalTargetLimit(), QueriedAt: at}, nil
}
