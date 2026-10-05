package metricsdiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

type identityReaderFunc func(context.Context) (*models.ObservabilityIdentitySnapshot, error)

func (f identityReaderFunc) GetObservabilityIdentities(ctx context.Context) (*models.ObservabilityIdentitySnapshot, error) {
	return f(ctx)
}

func testNodeTarget() NodeTarget {
	return NodeTarget{ID: uuid.NewString(), Version: 1, Subject: NodeSubject{Kind: "node", NodeID: uuid.NewString()},
		MonitorKind: "host_resources", Source: NodeSource{Type: "node_exporter", Endpoint: "https://127.0.0.1:9443/metrics"}, Enabled: true}
}

func testSnapshot(targets ...NodeTarget) *models.ObservabilityIdentitySnapshot {
	snapshot := &models.ObservabilityIdentitySnapshot{ObservedAt: time.Now().UTC(), Nodes: []models.ObservabilityNodeIdentity{}, ModuleInstances: []models.ObservabilityModuleIdentity{}}
	for _, target := range targets {
		snapshot.Nodes = append(snapshot.Nodes, models.ObservabilityNodeIdentity{NodeID: target.Subject.NodeID, Version: 3})
	}
	return snapshot
}

func TestNodeTargetRejectsUnsupportedAttributionAndSourceKinds(t *testing.T) {
	for _, mutate := range []func(*NodeTarget){
		func(t *NodeTarget) { t.ID = uuid.Nil.String() }, func(t *NodeTarget) { t.Version = 0 },
		func(t *NodeTarget) { t.Subject.Kind = "engine" }, func(t *NodeTarget) { t.Subject.Kind = "module_instance" },
		func(t *NodeTarget) { t.Subject.NodeID = "node-name" }, func(t *NodeTarget) { t.Source.Type = "database_exporter" },
		func(t *NodeTarget) { t.MonitorKind = "process_resources" }, func(t *NodeTarget) { t.MonitorKind = "service_quality" },
		func(t *NodeTarget) { t.Source.Type = "cadvisor" },
		func(t *NodeTarget) { t.Source.Endpoint = "" }, func(t *NodeTarget) { t.Source.Endpoint = "https://source.test:9443/debug" },
	} {
		target := testNodeTarget()
		mutate(&target)
		if target.Validate() != ErrInvalidTarget {
			t.Fatalf("invalid attribution admitted: %s", target)
		}
	}
	target := testNodeTarget()
	target.Source.Type = "cadvisor"
	target.MonitorKind = "container_resources"
	if err := target.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNodeReservationsIncludeCenterAndRejectDuplicateOwnership(t *testing.T) {
	targets := make([]NodeTarget, 10)
	for i := range targets {
		targets[i] = testNodeTarget()
		targets[i].Source.Endpoint = fmt.Sprintf("https://127.0.0.%d:9443/metrics", i+1)
	}
	if err := CheckReservations(targets[:9]); err != nil {
		t.Fatalf("within reservation: %v", err)
	}
	if err := CheckReservations(targets); err != ErrBudgetExceeded {
		t.Fatalf("center reservation omitted: %v", err)
	}
	targets[9].Enabled = false
	if err := CheckReservations(targets); err != nil {
		t.Fatalf("disabled source consumes scrape reservation: %v", err)
	}
	targets[1].Subject = targets[0].Subject
	if CheckReservations(targets[:2]) != ErrInvalidTarget {
		t.Fatal("duplicate enabled subject and kind admitted")
	}
	targets[1].Enabled = false
	if err := CheckReservations(targets[:2]); err != nil {
		t.Fatal(err)
	}
	targets[1].Enabled = true
	targets[1].MonitorKind = "container_resources"
	targets[1].Source.Type = "cadvisor"
	if err := CheckReservations(targets[:2]); err != nil {
		t.Fatal("distinct node categories rejected")
	}
	targets[1].Source.Endpoint = targets[0].Source.Endpoint
	if CheckReservations(targets[:2]) != ErrInvalidTarget {
		t.Fatal("one physical source attributed twice")
	}
	targets[1].Enabled = false
	targets[1].ID = targets[0].ID
	if CheckReservations(targets[:2]) != ErrInvalidTarget {
		t.Fatal("duplicate configuration identity admitted")
	}
	disabled := make([]NodeTarget, TargetLimit+1)
	for i := range disabled {
		disabled[i] = testNodeTarget()
		disabled[i].Enabled = false
	}
	if CheckReservations(disabled) != ErrBudgetExceeded {
		t.Fatal("unbounded saved target input")
	}
}

func TestNodeProjectionUsesCurrentSystemNodeScopeAndControlledLabels(t *testing.T) {
	enabled, disabled, absent := testNodeTarget(), testNodeTarget(), testNodeTarget()
	disabled.Enabled = false
	disabled.Source.Endpoint = "https://disabled.test:9443/metrics"
	absent.Source.Endpoint = "https://absent.test:9443/metrics"
	enabled.Source.Endpoint = "https://source.test:9443/metrics"
	p := testPolicy(t, testSourcePKI(t, true), 9443)
	var lookups []string
	p.resolver = resolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		lookups = append(lookups, host)
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	snapshot := testSnapshot(enabled, disabled)
	projector := NewNodeProjector(true, p, identityReaderFunc(func(ctx context.Context) (*models.ObservabilityIdentitySnapshot, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > Timeout {
			t.Fatal("unbounded control-plane read")
		}
		return snapshot, nil
	}))
	groups, err := projector.Project(context.Background(), []NodeTarget{enabled, disabled, absent})
	if err != nil || len(groups) != 1 || !reflect.DeepEqual(lookups, []string{"source.test"}) {
		t.Fatalf("groups=%v err=%v lookups=%v", groups, err, lookups)
	}
	want := TargetGroup{Targets: []string{"127.0.0.1:9443"}, Labels: map[string]string{
		"__scheme__": "https", "__metrics_path__": "/metrics", "addp_node_id": enabled.Subject.NodeID,
		"addp_monitor_kind": "host_resources", "addp_source": "node_exporter", "__meta_addp_target_id": enabled.ID,
		"__meta_addp_target_version": "1", "__meta_addp_node_version": "3",
	}}
	if !reflect.DeepEqual(groups[0], want) {
		t.Fatalf("uncontrolled labels: %#v", groups[0])
	}
	encoded, err := json.Marshal(groups)
	if err != nil || strings.Contains(string(encoded), "source.test") || strings.Contains(string(encoded), "tenant") || strings.Contains(string(encoded), "instance") {
		t.Fatalf("unsupported attribution or DNS leaked into projection: %s", encoded)
	}
	enabled.Version++
	next, err := projector.Project(context.Background(), []NodeTarget{enabled})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range groups[0].Labels {
		if strings.HasPrefix(key, "__") {
			continue
		}
		if next[0].Labels[key] != value {
			t.Fatal("configuration save changed persisted metric identity")
		}
	}
	if next[0].Labels["__meta_addp_target_version"] != "2" {
		t.Fatal("missing configuration-version evidence")
	}
	snapshot.Nodes = []models.ObservabilityNodeIdentity{}
	groups, err = projector.Project(context.Background(), []NodeTarget{enabled})
	if err != nil || groups == nil || len(groups) != 0 || !enabled.Enabled {
		t.Fatalf("current scope did not remove target independently of saved intent: %v %v", groups, err)
	}
}

func TestNodeProjectionDoesNotReturnEmptySuccessOnControlPlaneFailure(t *testing.T) {
	target := testNodeTarget()
	for _, mode := range []string{"failure", "nil", "stale", "future", "missing nodes", "missing instances", "duplicate node", "invalid node", "invalid version", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			snapshot := testSnapshot(target)
			reader := identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) {
				switch mode {
				case "failure":
					return nil, errors.New("private System diagnostic")
				case "nil":
					return nil, nil
				case "stale":
					snapshot.ObservedAt = time.Now().Add(-time.Minute)
				case "future":
					snapshot.ObservedAt = time.Now().Add(time.Minute)
				case "missing nodes":
					snapshot.Nodes = nil
				case "missing instances":
					snapshot.ModuleInstances = nil
				case "duplicate node":
					snapshot.Nodes = append(snapshot.Nodes, snapshot.Nodes[0])
				case "invalid node":
					snapshot.Nodes[0].NodeID = "unregistered-name"
				case "invalid version":
					snapshot.Nodes[0].Version = 0
				case "oversized":
					snapshot.Nodes = make([]models.ObservabilityNodeIdentity, models.ObservabilityIdentityNodeLimit+1)
				}
				return snapshot, nil
			})
			projector := NewNodeProjector(true, testPolicy(t, testSourcePKI(t, true), 9443), reader)
			groups, err := projector.Project(context.Background(), []NodeTarget{target})
			if err != ErrIdentityUnavailable || groups != nil {
				t.Fatalf("control-plane failure cleared discovered targets: %v %v", groups, err)
			}
		})
	}
}

func TestNodeProjectionRechecksDNSAndRejectsPhysicalAliases(t *testing.T) {
	target, alias := testNodeTarget(), testNodeTarget()
	target.Source.Endpoint = "https://one.test:9443/metrics"
	alias.Source.Endpoint = "https://two.test:9443/metrics"
	p := testPolicy(t, testSourcePKI(t, true), 9443)
	address := netip.MustParseAddr("127.0.0.1")
	p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return []netip.Addr{address}, nil })
	projector := NewNodeProjector(true, p, identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) {
		return testSnapshot(target, alias), nil
	}))
	if _, err := projector.Project(context.Background(), []NodeTarget{target}); err != nil {
		t.Fatal(err)
	}
	groups, err := projector.Project(context.Background(), []NodeTarget{target, alias})
	if err != ErrInvalidTarget || groups != nil {
		t.Fatalf("same endpoint duplicated: %v %v", groups, err)
	}
	address = netip.MustParseAddr("169.254.169.254")
	groups, err = projector.Project(context.Background(), []NodeTarget{target})
	if err != ErrEndpointForbidden || groups != nil {
		t.Fatalf("DNS change was cached or converted into empty success: %v %v", groups, err)
	}
}

func TestNodeProjectionRejectsSnapshotThatExpiresDuringResolution(t *testing.T) {
	target := testNodeTarget()
	target.Source.Endpoint = "https://source.test:9443/metrics"
	p := testPolicy(t, testSourcePKI(t, true), 9443)
	now := time.Now().UTC()
	snapshot := testSnapshot(target)
	snapshot.ObservedAt = now
	projector := NewNodeProjector(true, p, identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) { return snapshot, nil }))
	projector.now = func() time.Time { return now }
	p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		now = now.Add(Timeout + time.Second)
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	if groups, err := projector.Project(context.Background(), []NodeTarget{target}); groups != nil || err != ErrIdentityUnavailable {
		t.Fatalf("expired snapshot published: %v %v", groups, err)
	}
}

func TestNodeProjectionOptionalStateDoesNotTouchDependencies(t *testing.T) {
	reader := identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) {
		t.Fatal("optional state called System")
		return nil, nil
	})
	for _, tc := range []struct {
		enabled bool
		policy  *SourcePolicy
		want    error
	}{
		{false, nil, ErrDisabled}, {true, nil, ErrUnconfigured},
	} {
		groups, err := NewNodeProjector(tc.enabled, tc.policy, reader).Project(context.Background(), nil)
		if groups != nil || err != tc.want {
			t.Fatalf("got %v %v", groups, err)
		}
	}
	var projector *NodeProjector
	if _, err := projector.Project(context.Background(), nil); err != ErrDisabled {
		t.Fatal(err)
	}
}
