package metricsdiscovery

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func TestCDCProjectionSharesBudgetAndEndpointUniqueness(t *testing.T) {
	pki := testSourcePKI(t, true)
	server, port := testSourceServer(t, pki, tls.RequireAndVerifyClientCert, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "addp_cdc_redpanda_disk_total_bytes 100\n")
	}))
	target := testNodeTarget()
	target.Source.Endpoint = server.URL + "/metrics"
	snapshot := testSnapshot(target)
	start := time.Now().UTC().Add(-time.Minute)
	item := models.ObservabilityComponentIdentity{DockerEngineID: "docker-test", Project: "addp-infra", Kind: "redpanda",
		ContainerID: strings.Repeat("a", 64), StartedAt: start, Metrics: &models.EngineMetricsSource{State: "available", Endpoint: target.Source.Endpoint, ContainerID: strings.Repeat("a", 64), StartedAt: &start}}
	snapshot.InfrastructureComponents = &models.ObservabilityComponentSnapshot{State: "available", Items: []models.ObservabilityComponentIdentity{item}}
	policy := testPolicy(t, pki, port)
	projector := NewNodeProjector(true, policy, identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) { return snapshot, nil }))
	groups, err := projector.Project(context.Background(), nil)
	if err != nil || len(groups) != 1 || groups[0].Labels["addp_monitor_kind"] != "cdc_resources" || groups[0].Labels["addp_component_id"] != item.ID() {
		t.Fatal(groups, err)
	}
	if _, err := projector.Project(context.Background(), []NodeTarget{target}); !errors.Is(err, ErrInvalidTarget) {
		t.Fatal("duplicate physical source admitted", err)
	}
	policy.targetLimit = 1
	if _, err := projector.Project(context.Background(), []NodeTarget{target}); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatal("component escaped budget", err)
	}
	policy.targetLimit = 12
	snapshot.InfrastructureComponents.Items = append(snapshot.InfrastructureComponents.Items, item)
	if _, err := projector.Project(context.Background(), nil); !errors.Is(err, ErrIdentityUnavailable) {
		t.Fatal("duplicate component admitted", err)
	}
	search := item
	search.Kind = "meilisearch"
	snapshot.InfrastructureComponents.Items = []models.ObservabilityComponentIdentity{search}
	paused := map[string]bool{ComponentCollectionScope(search).ID: true}
	projector.WithCollectionPauses(func(context.Context) (map[string]bool, error) { return paused, nil })
	if groups, err := projector.Project(context.Background(), nil); err != nil || len(groups) != 0 {
		t.Fatal("paused Meilisearch entered discovery", groups, err)
	}
	delete(paused, ComponentCollectionScope(search).ID)
	if groups, err := projector.Project(context.Background(), nil); err != nil || len(groups) != 1 || groups[0].Labels[CollectionScopeLabel] != ComponentCollectionScope(search).ID {
		t.Fatal("Meilisearch did not return after resume", groups, err)
	}
	snapshot.InfrastructureComponents = &models.ObservabilityComponentSnapshot{State: "unavailable", Items: []models.ObservabilityComponentIdentity{}}
	if groups, err := projector.Project(context.Background(), []NodeTarget{target}); err != nil || len(groups) != 1 {
		t.Fatal("component discovery outage affected existing sources", groups, err)
	}
}
