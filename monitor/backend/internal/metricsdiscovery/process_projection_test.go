package metricsdiscovery

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func testProcessIdentity(endpoint string) models.ObservabilityModuleIdentity {
	started := time.Now().UTC().Add(-time.Minute)
	return models.ObservabilityModuleIdentity{ModuleName: "monitor", InstanceID: "native-worker", Role: "worker", LeaseExpiresAt: time.Now().UTC().Add(time.Minute), ProcessStartedAt: &started,
		ProcessMetrics: &models.ProcessMetricsDeclaration{SchemaVersion: models.ProcessMetricsSchema, Endpoint: endpoint}}
}

func TestProcessProjectionAutomaticallyAdmitsUnboundSource(t *testing.T) {
	pki := testSourcePKI(t, true)
	server, port := testSourceServer(t, pki, tls.RequireAndVerifyClientCert, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "process_resident_memory_bytes 4096\n")
	}))
	defer server.Close()
	policy := testPolicy(t, pki, port)
	instance := testProcessIdentity(server.URL + "/metrics")
	snapshot := testSnapshot()
	snapshot.ModuleInstances = []models.ObservabilityModuleIdentity{instance}
	projector := NewNodeProjector(true, policy, identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) { return snapshot, nil }))
	groups, err := projector.Project(context.Background(), nil)
	if err != nil || len(groups) != 1 {
		t.Fatalf("automatic process discovery: %+v %v", groups, err)
	}
	group := groups[0]
	if group.Labels["addp_node_id"] != "" || group.Labels["addp_module_name"] != "monitor" || group.Labels["addp_instance_id"] != "native-worker" || group.Labels["addp_runtime_role"] != "worker" || group.Labels["addp_monitor_kind"] != "process_resources" || group.Labels["addp_source"] != "application" || len(group.Targets) != 1 {
		t.Fatalf("controlled source: %+v", group)
	}
	for _, mutate := range []func(*models.ObservabilityModuleIdentity){
		func(i *models.ObservabilityModuleIdentity) { i.NodeID = "unknown" },
		func(i *models.ObservabilityModuleIdentity) { i.ProcessStartedAt = nil },
		func(i *models.ObservabilityModuleIdentity) { i.LeaseExpiresAt = snapshot.ObservedAt },
		func(i *models.ObservabilityModuleIdentity) { i.Role = "unknown" },
		func(i *models.ObservabilityModuleIdentity) { i.InstanceID = "injected\nidentity" },
	} {
		copy := instance
		mutate(&copy)
		snapshot.ModuleInstances = []models.ObservabilityModuleIdentity{copy}
		if got, err := projector.Project(context.Background(), nil); err != ErrIdentityUnavailable || got != nil {
			t.Fatalf("invalid scope: %+v %v", got, err)
		}
	}
}

func TestProcessProjectionRejectsUnprotectedTransportAndDuplicatePhysicalSource(t *testing.T) {
	for _, auth := range []tls.ClientAuthType{tls.NoClientCert, tls.RequireAndVerifyClientCert} {
		pki := testSourcePKI(t, true)
		server, port := testSourceServer(t, pki, auth, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "process_resident_memory_bytes 4096\n")
		}))
		instance := testProcessIdentity(server.URL + "/metrics")
		snapshot := testSnapshot()
		snapshot.ModuleInstances = []models.ObservabilityModuleIdentity{instance}
		if auth != tls.NoClientCert {
			copy := instance
			copy.InstanceID = "different-process"
			snapshot.ModuleInstances = append(snapshot.ModuleInstances, copy)
		}
		projector := NewNodeProjector(true, testPolicy(t, pki, port), identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) { return snapshot, nil }))
		groups, err := projector.Project(context.Background(), nil)
		server.Close()
		want := ErrUnprotected
		if auth != tls.NoClientCert {
			want = ErrInvalidTarget
		}
		if err != want || groups != nil {
			t.Fatalf("unsafe transport or duplicate: %+v %v", groups, err)
		}
	}
}

func TestProcessAndHostShareWholeSnapshotReservations(t *testing.T) {
	snapshot := testSnapshot()
	for i := 0; i < 10; i++ {
		instance := testProcessIdentity("https://127.0.0.1:" + strconv.Itoa(18100+i) + "/metrics")
		instance.InstanceID = "process-" + strconv.Itoa(i)
		snapshot.ModuleInstances = append(snapshot.ModuleInstances, instance)
	}
	policy := testPolicy(t, testSourcePKI(t, true), 9443)
	projector := NewNodeProjector(true, policy, identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) { return snapshot, nil }))
	if groups, err := projector.Project(context.Background(), nil); err != ErrBudgetExceeded || groups != nil {
		t.Fatalf("process source budget: %+v %v", groups, err)
	}
	snapshot.ModuleInstances = snapshot.ModuleInstances[:9]
	node := testNodeTarget()
	snapshot.Nodes = append(snapshot.Nodes, models.ObservabilityNodeIdentity{NodeID: node.Subject.NodeID, Version: 1})
	if groups, err := projector.Project(context.Background(), []NodeTarget{node}); err != ErrBudgetExceeded || groups != nil {
		t.Fatalf("combined reservation: %+v %v", groups, err)
	}
	if strings.Contains(ErrBudgetExceeded.Error(), "18100") {
		t.Fatal("budget diagnostics leak source")
	}
}
