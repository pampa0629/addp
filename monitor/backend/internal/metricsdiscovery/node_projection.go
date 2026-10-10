package metricsdiscovery

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

type NodeSubject struct {
	Kind   string `json:"kind"`
	NodeID string `json:"node_id"`
}

type NodeSource struct {
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

// NodeTarget is Monitor-owned configuration input, not another node register.
// Process sources come exclusively from System's private instance projection,
// never from this manual node configuration.
type NodeTarget struct {
	ID          string      `json:"id"`
	Version     int64       `json:"version"`
	Subject     NodeSubject `json:"subject"`
	MonitorKind string      `json:"monitor_kind"`
	Source      NodeSource  `json:"source"`
	Enabled     bool        `json:"enabled"`
}

type TargetGroup struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

type IdentityReader interface {
	GetObservabilityIdentities(context.Context) (*models.ObservabilityIdentitySnapshot, error)
}

func validUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func (t NodeTarget) Validate() error {
	if !validUUID(t.ID) || t.Version < 1 || t.Subject.Kind != "node" || !validUUID(t.Subject.NodeID) {
		return ErrInvalidTarget
	}
	if (t.Source.Type != "node_exporter" || t.MonitorKind != "host_resources") &&
		(t.Source.Type != "cadvisor" || t.MonitorKind != "container_resources") {
		return ErrInvalidTarget
	}
	if _, err := parseEndpoint(t.Source.Endpoint); err != nil {
		return ErrInvalidTarget
	}
	return nil
}

// CheckReservations must also run inside the future target write transaction.
// This is reservation of endpoint sample budgets, not a TSDB head-series cap.
func CheckReservations(targets []NodeTarget) error {
	if len(targets) > TargetLimit {
		return ErrBudgetExceeded
	}
	ids := make(map[string]bool)
	subjects := make(map[struct{ node, kind string }]bool)
	endpoints := make(map[string]bool)
	reservation := SelfSampleReservation
	for _, target := range targets {
		if err := target.Validate(); err != nil {
			return err
		}
		if ids[target.ID] {
			return ErrInvalidTarget
		}
		ids[target.ID] = true
		if !target.Enabled {
			continue
		}
		key := struct{ node, kind string }{target.Subject.NodeID, target.MonitorKind}
		if subjects[key] || endpoints[target.Source.Endpoint] {
			return ErrInvalidTarget
		}
		subjects[key], endpoints[target.Source.Endpoint] = true, true
		reservation += EndpointSampleLimit
		if reservation > TotalSampleReservation {
			return ErrBudgetExceeded
		}
	}
	return nil
}

type NodeProjector struct {
	enabled    bool
	policy     *SourcePolicy
	identities IdentityReader
	now        func() time.Time
}

func NewNodeProjector(enabled bool, policy *SourcePolicy, identities IdentityReader) *NodeProjector {
	return &NodeProjector{enabled: enabled, policy: policy, identities: identities, now: time.Now}
}

// Project fails closed for unavailable control-plane facts. Only a successfully
// verified current scope may produce an empty result; there is no cached fallback.
func (p *NodeProjector) Project(ctx context.Context, targets []NodeTarget) ([]TargetGroup, error) {
	if p == nil || !p.enabled {
		return nil, ErrDisabled
	}
	if p.policy == nil || p.identities == nil {
		return nil, ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	if err := CheckReservations(targets); err != nil {
		return nil, err
	}
	started := p.now().UTC()
	snapshot, err := p.identities.GetObservabilityIdentities(ctx)
	if err != nil || snapshot == nil || ctx.Err() != nil || snapshot.Nodes == nil || snapshot.ModuleInstances == nil ||
		snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.Before(started.Add(-Timeout)) || snapshot.ObservedAt.After(p.now().UTC().Add(Timeout)) ||
		len(snapshot.Nodes) > models.ObservabilityIdentityNodeLimit || len(snapshot.ModuleInstances) > models.ObservabilityIdentityInstanceLimit {
		return nil, ErrIdentityUnavailable
	}
	nodes := make(map[string]int64, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if !validUUID(node.NodeID) || node.Version < 1 || nodes[node.NodeID] != 0 {
			return nil, ErrIdentityUnavailable
		}
		nodes[node.NodeID] = node.Version
	}
	reservation := SelfSampleReservation
	for _, target := range targets {
		if target.Enabled {
			reservation += EndpointSampleLimit
		}
	}
	processes := map[struct{ module, instance string }]bool{}
	validName := func(s string, max int) bool {
		return s != "" && strings.TrimSpace(s) == s && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && strings.IndexFunc(s, unicode.IsControl) < 0
	}
	for _, instance := range snapshot.ModuleInstances {
		key := struct{ module, instance string }{instance.ModuleName, instance.InstanceID}
		if !validName(instance.ModuleName, 50) || !validName(instance.InstanceID, 100) || processes[key] || !instance.LeaseExpiresAt.After(snapshot.ObservedAt) || (instance.NodeID != "" && nodes[instance.NodeID] == 0) || (instance.NodeID == "" && instance.ProcessMetrics == nil) {
			return nil, ErrIdentityUnavailable
		}
		processes[key] = true
		switch instance.Role {
		case "backend", "worker", "scheduler", "ingress":
		default:
			return nil, ErrIdentityUnavailable
		}
		if instance.ProcessMetrics == nil {
			continue
		}
		if instance.ProcessMetrics.Validate() != nil || instance.ProcessStartedAt == nil || instance.ProcessStartedAt.IsZero() || instance.ProcessStartedAt.After(snapshot.ObservedAt) {
			return nil, ErrIdentityUnavailable
		}
		reservation += EndpointSampleLimit
		if reservation > TotalSampleReservation {
			return nil, ErrBudgetExceeded
		}
	}
	groups := make([]TargetGroup, 0)
	physical := make(map[string]bool)
	for _, target := range targets {
		nodeVersion := nodes[target.Subject.NodeID]
		if !target.Enabled || nodeVersion == 0 {
			continue
		}
		resolved, err := p.policy.Resolve(ctx, target.Source.Endpoint)
		if err != nil {
			return nil, err
		}
		if physical[resolved.Address] {
			return nil, ErrInvalidTarget
		}
		physical[resolved.Address] = true
		groups = append(groups, TargetGroup{
			Targets: []string{resolved.Address}, Labels: map[string]string{
				"__scheme__": "https", "__metrics_path__": "/metrics",
				"addp_node_id": target.Subject.NodeID, "addp_monitor_kind": target.MonitorKind, "addp_source": target.Source.Type,
				"__meta_addp_target_id": target.ID, "__meta_addp_target_version": strconv.FormatInt(target.Version, 10),
				"__meta_addp_node_version": strconv.FormatInt(nodeVersion, 10),
			},
		})
	}
	for _, instance := range snapshot.ModuleInstances {
		if instance.ProcessMetrics == nil {
			continue
		}
		resolved, err := p.policy.Admit(ctx, instance.ProcessMetrics.Endpoint)
		if err != nil {
			return nil, err
		}
		if physical[resolved.Address] {
			return nil, ErrInvalidTarget
		}
		physical[resolved.Address] = true
		if len(groups) >= TargetLimit {
			return nil, ErrBudgetExceeded
		}
		groups = append(groups, TargetGroup{Targets: []string{resolved.Address}, Labels: map[string]string{
			"__scheme__": "https", "__metrics_path__": "/metrics",
			"addp_module_name": instance.ModuleName, "addp_instance_id": instance.InstanceID, "addp_runtime_role": instance.Role,
			"addp_monitor_kind": "process_resources", "addp_source": "application",
		}})
	}
	if ctx.Err() != nil || snapshot.ObservedAt.Before(p.now().UTC().Add(-Timeout)) {
		return nil, ErrIdentityUnavailable
	}
	return groups, nil
}

// String is safe to log; it intentionally excludes addresses and credentials.
func (t NodeTarget) String() string {
	return fmt.Sprintf("node monitoring target %s version %d", t.ID, t.Version)
}
