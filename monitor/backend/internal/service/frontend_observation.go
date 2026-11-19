package service

import (
	"context"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type FrontendObservationBackend interface {
	FrontendSamples(context.Context, string, string, string, time.Time, resourcequery.Budget) ([]resourcequery.FrontendSample, error)
}

type FrontendObservation struct {
	Node        *client.HostNodeReference      `json:"node"`
	Data        []resourcequery.FrontendSample `json:"data"`
	SourceState string                         `json:"source_state"`
	QueriedAt   time.Time                      `json:"queried_at"`
}

func (s *ResourceObservationService) WithFrontend(backend FrontendObservationBackend) *ResourceObservationService {
	s.frontend = backend
	return s
}

func (s *ResourceObservationService) FrontendObservation(ctx context.Context, principal, token, nodeID string) (FrontendObservation, error) {
	var result FrontendObservation
	if s == nil || s.frontend == nil || s.policy == nil || s.nodes == nil || s.targets == nil {
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
	node, err := s.nodes.GetHostNodeForUser(ctx, nodeID, token)
	if err != nil {
		return result, err
	}
	if node == nil || node.NodeID != nodeID {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	cfg, err := s.Policy(ctx)
	if err != nil {
		return result, err
	}
	if err := s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, err
	}
	at := s.now().UTC().Truncate(time.Second)
	result.Node, result.QueriedAt, result.SourceState = node, at, "not_connected"
	result.Data = []resourcequery.FrontendSample{}
	targets, err := s.targets.Snapshot(ctx)
	if err != nil {
		return result, err
	}
	for _, target := range targets {
		if target.Subject.NodeID != nodeID || target.MonitorKind != "frontend_resources" {
			continue
		}
		if !target.Enabled || !node.Enabled {
			result.SourceState = "disabled"
			continue
		}
		resolved, err := s.policy.Resolve(ctx, target.Source.Endpoint)
		if err != nil {
			return result, err
		}
		queryCtx, done := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
		result.Data, err = s.frontend.FrontendSamples(queryCtx, nodeID, resolved.Address, target.FrontendRevision(node.Version), at, cfg.Budget)
		done()
		if err != nil {
			return result, err
		}
		current, err := s.nodes.GetHostNodeForUser(ctx, nodeID, token)
		if err != nil {
			return result, err
		}
		saved, err := s.targets.Get(ctx, target.ID)
		if err != nil {
			return result, err
		}
		if saved != target || current == nil || current.NodeID != nodeID || !current.Enabled || current.Version != node.Version {
			return result, resourcequery.ErrConflict
		}
		result.SourceState = "no_sample"
		for _, row := range result.Data {
			if row.EntryUp != nil {
				result.SourceState = "collecting"
				break
			}
		}
		return result, nil
	}
	return result, nil
}
