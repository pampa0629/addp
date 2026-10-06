package service

import (
	"context"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/repository"
	"github.com/google/uuid"
)

type UserNodeReader interface {
	GetHostNodeForUser(context.Context, string, string) (*client.HostNodeReference, error)
	AuthorizeHostNodesForUser(context.Context, string) error
}
type TargetStore interface {
	Snapshot(context.Context) ([]metricsdiscovery.NodeTarget, error)
	Get(context.Context, string) (metricsdiscovery.NodeTarget, error)
	Mutate(context.Context, metricsdiscovery.NodeTarget, bool, bool, func(context.Context, []metricsdiscovery.NodeTarget) error) (metricsdiscovery.NodeTarget, error)
}
type MonitoringTargetInput struct {
	Version     int64                        `json:"version"`
	Subject     metricsdiscovery.NodeSubject `json:"subject"`
	MonitorKind string                       `json:"monitor_kind"`
	Source      metricsdiscovery.NodeSource  `json:"source"`
	Enabled     *bool                        `json:"enabled" binding:"required"`
}
type MonitoringTargetPage struct {
	Data       []metricsdiscovery.NodeTarget `json:"data"`
	Total      int                           `json:"total"`
	Page       int                           `json:"page"`
	PageSize   int                           `json:"page_size"`
	TotalPages int                           `json:"total_pages"`
}
type MonitoringTargetService struct {
	store     TargetStore
	nodes     UserNodeReader
	policy    *metricsdiscovery.SourcePolicy
	projector *metricsdiscovery.NodeProjector
	enabled   bool
}

func NewMonitoringTargetService(store TargetStore, nodes UserNodeReader, identities metricsdiscovery.IdentityReader, enabled bool, policy *metricsdiscovery.SourcePolicy) *MonitoringTargetService {
	return &MonitoringTargetService{store: store, nodes: nodes, policy: policy, enabled: enabled, projector: metricsdiscovery.NewNodeProjector(enabled, policy, identities)}
}
func (s *MonitoringTargetService) authorize(ctx context.Context, node, token string) error {
	if s == nil || s.nodes == nil {
		return metricsdiscovery.ErrIdentityUnavailable
	}
	ref, err := s.nodes.GetHostNodeForUser(ctx, node, token)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if ref == nil || ref.NodeID != node || ref.Version < 1 {
		return metricsdiscovery.ErrIdentityUnavailable
	}
	return nil
}
func (s *MonitoringTargetService) List(ctx context.Context, token string, page, size int) (*MonitoringTargetPage, error) {
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
	defer cancel()
	if s == nil || s.store == nil {
		return nil, metricsdiscovery.ErrUnconfigured
	}
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, metricsdiscovery.ErrInvalidTarget
	}
	if s.nodes == nil {
		return nil, metricsdiscovery.ErrIdentityUnavailable
	}
	if err := s.nodes.AuthorizeHostNodesForUser(ctx, token); err != nil {
		return nil, err
	}
	rows, err := s.store.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	start := (page - 1) * size
	if start > len(rows) {
		start = len(rows)
	}
	end := start + size
	if end > len(rows) {
		end = len(rows)
	}
	checked := make(map[string]bool)
	for _, row := range rows[start:end] {
		if !checked[row.Subject.NodeID] {
			if err := s.authorize(ctx, row.Subject.NodeID, token); err != nil {
				return nil, err
			}
			checked[row.Subject.NodeID] = true
		}
	}

	return &MonitoringTargetPage{Data: append(make([]metricsdiscovery.NodeTarget, 0, end-start), rows[start:end]...), Total: len(rows), Page: page, PageSize: size, TotalPages: (len(rows) + size - 1) / size}, nil
}

func (s *MonitoringTargetService) Get(ctx context.Context, id, token string) (metricsdiscovery.NodeTarget, error) {
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
	defer cancel()
	if s == nil || s.store == nil {
		return metricsdiscovery.NodeTarget{}, metricsdiscovery.ErrUnconfigured
	}
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return row, err
	}
	return row, s.authorize(ctx, row.Subject.NodeID, token)
}
func (s *MonitoringTargetService) Save(ctx context.Context, id, token string, input MonitoringTargetInput) (metricsdiscovery.NodeTarget, error) {
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
	defer cancel()
	create := id == ""
	if create {
		id = uuid.NewString()
	}
	target := metricsdiscovery.NodeTarget{ID: id, Version: input.Version, Subject: input.Subject, MonitorKind: input.MonitorKind, Source: input.Source}
	if input.Enabled == nil || (create && input.Version != 0) || (!create && input.Version < 1) {
		return target, metricsdiscovery.ErrInvalidTarget
	}
	if create {
		target.Version = 1
	}
	target.Enabled = *input.Enabled
	if err := target.Validate(); err != nil {
		return target, err
	}
	if s == nil || s.store == nil {
		return target, metricsdiscovery.ErrUnconfigured
	}
	if !create {
		old, err := s.store.Get(ctx, target.ID)
		if err != nil {
			return target, err
		}
		if err := s.authorize(ctx, old.Subject.NodeID, token); err != nil {
			return target, err
		}
		if old.Version != input.Version || old.Subject != target.Subject || old.MonitorKind != target.MonitorKind {
			return target, repository.ErrTargetConflict
		}
	}
	if create {
		if err := s.authorize(ctx, target.Subject.NodeID, token); err != nil {
			return target, err
		}
	}

	if target.Enabled {
		if !s.enabled {
			return target, metricsdiscovery.ErrDisabled
		}
		if s.policy == nil {
			return target, metricsdiscovery.ErrUnconfigured
		}
		if _, err := s.policy.Admit(ctx, target.Source.Endpoint); err != nil {
			if ctx.Err() != nil {
				return target, ctx.Err()
			}
			return target, err
		}
	}
	return s.store.Mutate(ctx, target, create, false, s.checkPhysicalEndpoints)
}
func (s *MonitoringTargetService) checkPhysicalEndpoints(ctx context.Context, targets []metricsdiscovery.NodeTarget) error {
	seen := make(map[string]bool)
	for _, target := range targets {
		if !target.Enabled {
			continue
		}
		resolved, err := s.policy.Resolve(ctx, target.Source.Endpoint)
		if err != nil {
			return err
		}
		if seen[resolved.Address] {
			return metricsdiscovery.ErrInvalidTarget
		}
		seen[resolved.Address] = true
	}
	return nil
}
func (s *MonitoringTargetService) Delete(ctx context.Context, id, token string, version int64) error {
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
	defer cancel()
	if version < 1 {
		return metricsdiscovery.ErrInvalidTarget
	}
	row, err := s.Get(ctx, id, token)
	if err != nil {
		return err
	}
	row.Version = version
	_, err = s.store.Mutate(ctx, row, false, true, nil)
	return err
}
func (s *MonitoringTargetService) Discover(ctx context.Context) ([]metricsdiscovery.TargetGroup, error) {
	ctx, cancel := context.WithTimeout(ctx, metricsdiscovery.Timeout)
	defer cancel()
	if s == nil || s.store == nil {
		return nil, metricsdiscovery.ErrUnconfigured
	}
	if !s.enabled {
		return nil, metricsdiscovery.ErrDisabled
	}
	if s.policy == nil {
		return nil, metricsdiscovery.ErrUnconfigured
	}
	rows, err := s.store.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := s.projector.Project(ctx, rows)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return groups, err
}
