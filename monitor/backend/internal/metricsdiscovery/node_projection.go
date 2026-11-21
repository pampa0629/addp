package metricsdiscovery

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/logger"
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

// FrontendRevision binds samples to the exact enabled configuration.
func (t NodeTarget) FrontendRevision(nodeVersion int64) string {
	return t.ID + ":" + strconv.FormatInt(t.Version, 10) + ":" + strconv.FormatInt(nodeVersion, 10)
}

func (t NodeTarget) Validate() error {
	if !validUUID(t.ID) || t.Version < 1 || t.Subject.Kind != "node" || !validUUID(t.Subject.NodeID) {
		return ErrInvalidTarget
	}
	if (t.Source.Type != "node_exporter" || t.MonitorKind != "host_resources") &&
		(t.Source.Type != "cadvisor" || t.MonitorKind != "container_resources") &&
		(t.Source.Type != "frontend_exporter" || t.MonitorKind != "frontend_resources") {
		return ErrInvalidTarget
	}
	if _, err := parseEndpoint(t.Source.Endpoint); err != nil {
		return ErrInvalidTarget
	}
	return nil
}

// CountTargetReservations validates the stored target set and counts enabled endpoints.
func CountTargetReservations(targets []NodeTarget) (int, error) {
	if len(targets) > TargetLimit {
		return 0, ErrBudgetExceeded
	}
	ids := make(map[string]bool)
	subjects := make(map[struct{ node, kind string }]bool)
	endpoints := make(map[string]bool)
	reserved := 0
	for _, target := range targets {
		if err := target.Validate(); err != nil {
			return 0, err
		}
		if ids[target.ID] {
			return 0, ErrInvalidTarget
		}
		ids[target.ID] = true
		if !target.Enabled {
			continue
		}
		key := struct{ node, kind string }{target.Subject.NodeID, target.MonitorKind}
		if subjects[key] || endpoints[target.Source.Endpoint] {
			return 0, ErrInvalidTarget
		}
		subjects[key], endpoints[target.Source.Endpoint] = true, true
		reserved++
	}
	return reserved, nil
}

// CheckReservations bounds endpoint reservations, independent of actual TSDB series.
func CheckReservations(targets []NodeTarget, targetLimit int) error {
	reserved, err := CountTargetReservations(targets)
	if err != nil {
		return err
	}
	if reserved > targetLimit {
		return ErrBudgetExceeded
	}
	return nil
}

type NodeProjector struct {
	enabled    bool
	policy     *SourcePolicy
	identities IdentityReader
	now        func() time.Time
	paused     func(context.Context) (map[string]bool, error)
}

func NewNodeProjector(enabled bool, policy *SourcePolicy, identities IdentityReader) *NodeProjector {
	return &NodeProjector{enabled: enabled, policy: policy, identities: identities, now: time.Now}
}

func (p *NodeProjector) WithCollectionPauses(read func(context.Context) (map[string]bool, error)) {
	p.paused = read
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
	reserved, err := CountTargetReservations(targets)
	if err != nil {
		return nil, err
	}
	if reserved > p.policy.targetLimit {
		return nil, ErrBudgetExceeded
	}
	snapshot, nodes, err := readIdentitySnapshot(ctx, p.identities, p.now)
	if err != nil {
		return nil, err
	}
	var paused map[string]bool
	if p.paused != nil {
		paused, err = p.paused(ctx)
		if err != nil {
			return nil, err
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
		if paused[ProcessCollectionScope(instance).ID] {
			continue
		}
		reserved++
		if reserved > p.policy.targetLimit {
			return nil, ErrBudgetExceeded
		}
	}
	groups := make([]TargetGroup, 0)
	engines := make(map[string]bool)
	sources := make(map[models.ObservabilityEngineSourceKey]bool)
	if len(snapshot.EngineDeployments) > models.ObservabilityIdentityEngineLimit {
		return nil, ErrIdentityUnavailable
	}
	for _, engine := range snapshot.EngineDeployments {
		if engine.Validate() != nil || nodes[engine.NodeID] == 0 || engines[engine.DeploymentID] || sources[engine.SourceKey] ||
			(engine.Metrics != nil && engine.Metrics.StartedAt != nil && engine.Metrics.StartedAt.After(snapshot.ObservedAt)) {
			return nil, ErrIdentityUnavailable
		}
		engines[engine.DeploymentID], sources[engine.SourceKey] = true, true
		if engine.Metrics != nil && engine.Metrics.State == "available" {
			reserved++
			if reserved > p.policy.targetLimit {
				return nil, ErrBudgetExceeded
			}
		}
	}
	components := snapshot.InfrastructureComponents
	if components != nil {
		if components.Validate(snapshot.ObservedAt) != nil {
			return nil, ErrIdentityUnavailable
		}
		for _, component := range components.Items {
			if component.Metrics.State == "available" && !paused[ComponentCollectionScope(component).ID] {
				reserved++
			}
		}
		if reserved > p.policy.targetLimit {
			return nil, ErrBudgetExceeded
		}
	}
	var runtimes []models.RuntimeMetricsIdentity
	runtimeIDs := map[uint]bool{}
	for _, runtime := range snapshot.RuntimeMetrics {
		if !runtime.LeaseExpiresAt.After(p.now().UTC()) {
			continue
		}
		if runtime.EngineID == 0 || runtimeIDs[runtime.EngineID] || models.RuntimeMetricsEngineType(runtime.Kind) == "" ||
			runtime.RuntimeMetricsDeclaration.Validate(snapshot.ObservedAt) != nil ||
			runtime.LeaseExpiresAt.After(snapshot.ObservedAt.Add(models.RuntimeMetricsLeaseDuration)) {
			return nil, ErrIdentityUnavailable
		}
		runtimeIDs[runtime.EngineID] = true
		if paused[RuntimeCollectionScope(&runtime).ID] {
			continue
		}
		reserved++
		if reserved > p.policy.targetLimit {
			return nil, ErrBudgetExceeded
		}
		runtimes = append(runtimes, runtime)
	}
	physical := make(map[string]bool)
	declared := make(map[string]bool)
	dynamic := make(map[string]bool)
	resolve := func(endpoint string) (ResolvedEndpoint, bool, error) {
		if declared[endpoint] {
			return ResolvedEndpoint{}, false, ErrInvalidTarget
		}
		declared[endpoint] = true
		resolved, err := p.policy.Resolve(ctx, endpoint)
		if ctx.Err() != nil {
			return ResolvedEndpoint{}, false, ctx.Err()
		}
		if err != nil {
			if sourceAdmissionFailure(err) {
				return ResolvedEndpoint{}, false, nil
			}
			return ResolvedEndpoint{}, false, err
		}
		if physical[resolved.Address] {
			return ResolvedEndpoint{}, false, ErrInvalidTarget
		}
		physical[resolved.Address] = true
		return resolved, true, nil
	}

	for _, target := range targets {
		nodeVersion := nodes[target.Subject.NodeID]
		if !target.Enabled || nodeVersion == 0 {
			continue
		}
		resolved, accepted, err := resolve(target.Source.Endpoint)
		if err != nil {
			return nil, err
		}
		if !accepted {
			continue
		}
		labels := map[string]string{
			"__scheme__": "https", "__metrics_path__": "/metrics",
			"addp_node_id": target.Subject.NodeID, "addp_monitor_kind": target.MonitorKind, "addp_source": target.Source.Type,
			"__meta_addp_target_id": target.ID, "__meta_addp_target_version": strconv.FormatInt(target.Version, 10),
			"__meta_addp_node_version": strconv.FormatInt(nodeVersion, 10),
		}
		if target.MonitorKind == "frontend_resources" {
			labels["addp_frontend_revision"] = target.FrontendRevision(nodeVersion)
		}
		groups = append(groups, TargetGroup{Targets: []string{resolved.Address}, Labels: labels})
	}
	for _, instance := range snapshot.ModuleInstances {
		scope := ProcessCollectionScope(instance)
		if instance.ProcessMetrics == nil || paused[scope.ID] {
			continue
		}
		resolved, accepted, err := resolve(instance.ProcessMetrics.Endpoint)
		if err != nil {
			return nil, err
		}
		if !accepted {
			continue
		}
		dynamic[resolved.Address] = true
		groups = append(groups, TargetGroup{Targets: []string{resolved.Address}, Labels: map[string]string{
			"__scheme__": "https", "__metrics_path__": "/metrics",
			"addp_module_name": instance.ModuleName, "addp_instance_id": instance.InstanceID, "addp_runtime_role": instance.Role,
			"addp_monitor_kind": "process_resources", "addp_source": "application", CollectionScopeLabel: scope.DiscoveryLabel(),
		}})
	}
	for _, engine := range snapshot.EngineDeployments {
		if engine.Metrics == nil || engine.Metrics.State != "available" {
			continue
		}
		resolved, accepted, err := resolve(engine.Metrics.Endpoint)
		if err != nil {
			return nil, err
		}
		if !accepted {
			continue
		}
		dynamic[resolved.Address] = true
		groups = append(groups, TargetGroup{Targets: []string{resolved.Address}, Labels: map[string]string{
			"__scheme__": "https", "__metrics_path__": "/metrics",
			"addp_monitor_kind": "engine_resources", "addp_source": engine.EngineType,
			"addp_deployment_id": engine.DeploymentID, "addp_database_container_id": engine.Metrics.ContainerID,
			"addp_database_container_started_at": engine.Metrics.StartedAt.UTC().Format(time.RFC3339Nano),
		}})
	}
	if components != nil {
		for _, component := range components.Items {
			scope := ComponentCollectionScope(component)
			if component.Metrics.State != "available" || paused[scope.ID] {
				continue
			}
			resolved, accepted, err := resolve(component.Metrics.Endpoint)
			if err != nil {
				return nil, err
			}
			if !accepted {
				continue
			}
			monitorKind := "search_resources"
			if component.IsCDC() {
				monitorKind = "cdc_resources"
			}
			dynamic[resolved.Address] = true
			groups = append(groups, TargetGroup{Targets: []string{resolved.Address}, Labels: map[string]string{
				"__scheme__": "https", "__metrics_path__": "/metrics", "addp_monitor_kind": monitorKind, "addp_source": component.Kind,
				"addp_component_id": component.ID(), "addp_component_container_id": component.ContainerID,
				"addp_component_container_started_at": component.StartedAt.UTC().Format(time.RFC3339Nano), CollectionScopeLabel: scope.DiscoveryLabel(),
			}})
		}
	}
	for _, runtime := range runtimes {
		resolved, accepted, err := resolve(runtime.Endpoint)
		if err != nil {
			return nil, err
		}
		if accepted {
			dynamic[resolved.Address] = true
			groups = append(groups, TargetGroup{Targets: []string{resolved.Address}, Labels: map[string]string{
				"__scheme__": "https", "__metrics_path__": "/metrics", "addp_monitor_kind": "compute_resources", "addp_source": runtime.Kind,
				"addp_runtime_engine_id": strconv.FormatUint(uint64(runtime.EngineID), 10), "addp_instance_id": runtime.InstanceID, CollectionScopeLabel: RuntimeCollectionScope(&runtime).DiscoveryLabel(),
			}})
		}
	}
	admitted := make([]TargetGroup, 0, len(groups))
	for _, group := range groups {
		if dynamic[group.Targets[0]] {
			rejection := p.policy.admitResolved(ctx, ResolvedEndpoint{Address: group.Targets[0]})
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if rejection != nil {
				if group.Labels["addp_monitor_kind"] == "process_resources" {
					probe := "authenticated_metrics"
					if rejection.decision == ErrUnprotected {
						probe = "anonymous_denial"
					}
					logger.L().WarnContext(ctx, "metrics_discovery.process_source_excluded",
						"source_module", group.Labels["addp_module_name"], "source_instance", group.Labels["addp_instance_id"],
						"probe", probe, "reason", rejection.reason, "http_status", rejection.status)
				}
				continue
			}
		}
		admitted = append(admitted, group)
	}
	if ctx.Err() != nil || snapshot.ObservedAt.Before(p.now().UTC().Add(-Timeout)) {
		return nil, ErrIdentityUnavailable
	}
	return admitted, nil
}

// String is safe to log; it intentionally excludes addresses and credentials.
func (t NodeTarget) String() string {
	return fmt.Sprintf("node monitoring target %s version %d", t.ID, t.Version)
}

// CurrentNodeVersions reads the bounded System projection without admitting
// process endpoints. A process source outage must not suppress host evaluation.
func CurrentNodeVersions(ctx context.Context, reader IdentityReader) (map[string]int64, error) {
	if reader == nil {
		return nil, ErrIdentityUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	_, nodes, err := readIdentitySnapshot(ctx, reader, time.Now)
	return nodes, err
}
func readIdentitySnapshot(ctx context.Context, reader IdentityReader, now func() time.Time) (*models.ObservabilityIdentitySnapshot, map[string]int64, error) {
	started := now().UTC()
	snapshot, err := reader.GetObservabilityIdentities(ctx)
	if err != nil || snapshot == nil || ctx.Err() != nil || snapshot.Nodes == nil || snapshot.ModuleInstances == nil || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.Before(started.Add(-Timeout)) || snapshot.ObservedAt.After(now().UTC().Add(Timeout)) || len(snapshot.Nodes) > models.ObservabilityIdentityNodeLimit || len(snapshot.ModuleInstances) > models.ObservabilityIdentityInstanceLimit {
		return nil, nil, ErrIdentityUnavailable
	}
	nodes := make(map[string]int64, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if !validUUID(node.NodeID) || node.Version < 1 || nodes[node.NodeID] != 0 {
			return nil, nil, ErrIdentityUnavailable
		}
		nodes[node.NodeID] = node.Version
	}
	return snapshot, nodes, nil
}

// Only endpoint-specific admission failures are isolated. Shared control-plane errors remain fatal.
func sourceAdmissionFailure(err error) bool {
	return errors.Is(err, ErrAdmissionFailed) || errors.Is(err, ErrUnprotected) || errors.Is(err, ErrResolutionFailed) || errors.Is(err, ErrEndpointForbidden)
}
