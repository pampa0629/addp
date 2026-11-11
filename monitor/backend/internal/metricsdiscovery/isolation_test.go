package metricsdiscovery

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

func TestDynamicSourceFailureIsolatedAndRecovers(t *testing.T) {
	pki := testSourcePKI(t, true)
	var failed atomic.Int32
	failed.Store(-1)
	endpoints := []string{}
	ports := []uint16{}
	for index := 0; index < 4; index++ {
		index := index
		server, port := testSourceServer(t, pki, tls.RequireAndVerifyClientCert, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if failed.Load() == int32(index) {
				w.WriteHeader(503)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, "source_value 1\n")
		}))
		endpoints = append(endpoints, server.URL+"/metrics")
		ports = append(ports, port)
	}
	target := testNodeTarget()
	target.Source.Endpoint = endpoints[0]
	snapshot := testSnapshot(target)
	start := time.Now().UTC().Add(-time.Minute)
	snapshot.ModuleInstances = []models.ObservabilityModuleIdentity{testProcessIdentity(endpoints[1])}
	snapshot.EngineDeployments = []models.ObservabilityEngineIdentity{{DeploymentID: uuid.NewString(), Version: 1, Scope: "infra", EngineType: "redis", NodeID: target.Subject.NodeID,
		SourceKey: models.ObservabilityEngineSourceKey{DockerEngineID: "docker-test", Project: "addp-infra", Service: "redis"}, Metrics: &models.EngineMetricsSource{State: "available", Endpoint: endpoints[2], ContainerID: strings.Repeat("a", 64), StartedAt: &start}}}
	snapshot.InfrastructureComponents = &models.ObservabilityComponentSnapshot{State: "available", Items: []models.ObservabilityComponentIdentity{{DockerEngineID: "docker-test", Project: "addp-infra", Kind: "redpanda", ContainerID: strings.Repeat("b", 64), StartedAt: start,
		Metrics: &models.EngineMetricsSource{State: "available", Endpoint: endpoints[3], ContainerID: strings.Repeat("b", 64), StartedAt: &start}}}}
	policy := testPolicy(t, pki, ports...)
	projector := NewNodeProjector(true, policy, identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) { return snapshot, nil }))
	for _, index := range []int32{1, 2, 3, -1} {
		failed.Store(index)
		groups, err := projector.Project(context.Background(), []NodeTarget{target})
		want := 4
		if index >= 0 {
			want = 3
		}
		if err != nil || len(groups) != want {
			t.Fatalf("failed source %d: %d groups, %v", index, len(groups), err)
		}
		for _, g := range groups {
			if index >= 0 && g.Targets[0] == strings.TrimSuffix(strings.TrimPrefix(endpoints[index], "https://"), "/metrics") {
				t.Fatal("failed source retained")
			}
		}
	}
	// A failed source retains its reservation and cannot hide a duplicate declaration.
	failed.Store(3)
	policy.targetLimit = 3
	if groups, err := projector.Project(context.Background(), []NodeTarget{target}); !errors.Is(err, ErrBudgetExceeded) || groups != nil {
		t.Fatal("failed source escaped budget", groups, err)
	}
	policy.targetLimit = 12
	target.Source.Endpoint = endpoints[3]
	if groups, err := projector.Project(context.Background(), []NodeTarget{target}); !errors.Is(err, ErrInvalidTarget) || groups != nil {
		t.Fatal("failed source concealed duplicate", groups, err)
	}
	// Different DNS declarations resolving to the failed endpoint remain a global duplicate.
	snapshot.ModuleInstances[0].ProcessMetrics.Endpoint = "https://alias.test:" + strconv.Itoa(int(ports[3])) + "/metrics"
	policy.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	if groups, err := projector.Project(context.Background(), []NodeTarget{target}); !errors.Is(err, ErrInvalidTarget) || groups != nil {
		t.Fatal("failed physical alias concealed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if groups, err := projector.Project(ctx, nil); err == nil || groups != nil {
		t.Fatal("expired discovery published partial data")
	}
	snapshot.InfrastructureComponents.Items[0].Project = "foreign"
	if groups, err := projector.Project(context.Background(), nil); !errors.Is(err, ErrIdentityUnavailable) || groups != nil {
		t.Fatal("invalid identity published partial data", err)
	}
}

func TestDynamicAdmissionUsesTheResolvedAddressWithoutSecondLookup(t *testing.T) {
	pki := testSourcePKI(t, true)
	_, port := testSourceServer(t, pki, tls.RequireAndVerifyClientCert, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "value 1\n")
	}))
	policy := testPolicy(t, pki, port)
	lookups := 0
	policy.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		if lookups > 1 {
			return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	snapshot := testSnapshot()
	snapshot.ModuleInstances = []models.ObservabilityModuleIdentity{testProcessIdentity("https://source.test:" + strconv.Itoa(int(port)) + "/metrics")}
	projector := NewNodeProjector(true, policy, identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) { return snapshot, nil }))
	groups, err := projector.Project(context.Background(), nil)
	if err != nil || len(groups) != 1 || lookups != 1 {
		t.Fatalf("pinned admission: groups=%d lookups=%d error=%v", len(groups), lookups, err)
	}
}
