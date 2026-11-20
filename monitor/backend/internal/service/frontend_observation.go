package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type FrontendObservationBackend interface {
	BusinessNFSExport(context.Context, string, string, string, time.Time, resourcequery.Budget) (*bool, error)
	FrontendSamples(context.Context, string, string, string, time.Time, resourcequery.Budget) ([]resourcequery.FrontendSample, error)
}

type FrontendObservation struct {
	Node        *client.HostNodeReference      `json:"node"`
	Data        []resourcequery.FrontendSample `json:"data"`
	SourceState string                         `json:"source_state"`
	QueriedAt   time.Time                      `json:"queried_at"`
}

type FrontendNode struct {
	NodeID      string `json:"node_id"`
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
}

type FrontendNodes struct {
	Data []FrontendNode `json:"data"`
}

func (s *ResourceObservationService) FrontendNodes(ctx context.Context, token string) (FrontendNodes, error) {
	result := FrontendNodes{Data: []FrontendNode{}}
	if s == nil || s.targets == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
	defer cancel()
	reader, ok := s.nodes.(UserHostNodeScopeReader)
	if !ok {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	nodes, err := reader.HostNodesForUser(ctx, token)
	if errors.Is(err, client.ErrHostNodeScopeBudget) {
		return result, resourcequery.ErrBudget
	}
	if err != nil {
		return result, err
	}
	targets, err := s.targets.Snapshot(ctx)
	if err != nil {
		return result, err
	}
	configured := make(map[string]bool)
	for _, target := range targets {
		if target.MonitorKind == "frontend_resources" {
			id := target.Subject.NodeID
			configured[id] = configured[id] || target.Enabled
		}
	}
	for _, node := range nodes {
		if enabled, exists := configured[node.NodeID]; exists {
			result.Data = append(result.Data, FrontendNode{NodeID: node.NodeID, DisplayName: node.DisplayName, Enabled: node.Enabled && enabled})
		}
	}
	sort.Slice(result.Data, func(i, j int) bool {
		left, right := result.Data[i], result.Data[j]
		if left.Enabled != right.Enabled {
			return left.Enabled
		}
		if left.DisplayName != right.DisplayName {
			return left.DisplayName < right.DisplayName
		}
		return left.NodeID < right.NodeID
	})
	return result, nil
}

func (s *ResourceObservationService) WithFrontend(backend FrontendObservationBackend) *ResourceObservationService {
	s.frontend = backend
	return s
}

type BusinessNFSObservation struct {
	Node            *client.HostNodeReference `json:"node"`
	ExportAvailable *bool                     `json:"export_available"`
	SourceState     string                    `json:"source_state"`
	QueriedAt       time.Time                 `json:"queried_at"`
}

func (s *ResourceObservationService) FrontendObservation(ctx context.Context, principal, token, nodeID string) (FrontendObservation, error) {
	data := []resourcequery.FrontendSample{}
	state, err := s.observeDeploymentNode(ctx, principal, token, nodeID, func(ctx context.Context, id, address, revision string, at time.Time, b resourcequery.Budget) (bool, error) {
		rows, err := s.frontend.FrontendSamples(ctx, id, address, revision, at, b)
		if err != nil {
			return false, err
		}
		data = rows
		for _, row := range rows {
			if row.EntryUp != nil {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return FrontendObservation{}, err
	}
	return FrontendObservation{Node: state.Node, SourceState: state.SourceState, QueriedAt: state.QueriedAt, Data: data}, nil
}

func (s *ResourceObservationService) BusinessNFSObservation(ctx context.Context, principal, token, nodeID string) (BusinessNFSObservation, error) {
	var available *bool
	state, err := s.observeDeploymentNode(ctx, principal, token, nodeID, func(ctx context.Context, id, address, revision string, at time.Time, b resourcequery.Budget) (bool, error) {
		value, err := s.frontend.BusinessNFSExport(ctx, id, address, revision, at, b)
		available = value
		return value != nil, err
	})
	if err != nil {
		return BusinessNFSObservation{}, err
	}
	return BusinessNFSObservation{Node: state.Node, SourceState: state.SourceState, QueriedAt: state.QueriedAt, ExportAvailable: available}, nil
}

type deploymentNodeState struct {
	Node        *client.HostNodeReference
	SourceState string
	QueriedAt   time.Time
}

func (s *ResourceObservationService) observeDeploymentNode(ctx context.Context, principal, token, nodeID string, sample func(context.Context, string, string, string, time.Time, resourcequery.Budget) (bool, error)) (deploymentNodeState, error) {
	var result deploymentNodeState
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
		collected, err := sample(queryCtx, nodeID, resolved.Address, target.FrontendRevision(node.Version), at, cfg.Budget)
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
		if collected {
			result.SourceState = "collecting"
		}
		return result, nil
	}
	return result, nil
}
