package metricsdiscovery

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

func TestEngineProjectionUsesSharedAdmissionAndReservations(t *testing.T) {
	target := testNodeTarget()
	pki := testSourcePKI(t, true)
	server, port := testSourceServer(t, pki, tls.RequireAndVerifyClientCert, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "addp_postgresql_connections 0\n")
	}))
	target.Source.Endpoint = server.URL + "/metrics"
	snapshot := testSnapshot(target)
	started := time.Now().UTC().Add(-time.Hour)
	snapshot.EngineDeployments = []models.ObservabilityEngineIdentity{{DeploymentID: uuid.NewString(), Version: 1, Scope: "infra", EngineType: "postgresql", NodeID: target.Subject.NodeID,
		SourceKey: models.ObservabilityEngineSourceKey{DockerEngineID: "daemon", Project: "addp-infra", Service: "postgres"},
		Metrics:   &models.PostgreSQLMetricsSource{State: "available", Endpoint: target.Source.Endpoint, ContainerID: strings.Repeat("a", 64), StartedAt: &started}}}
	policy := testPolicy(t, pki, port)
	projector := NewNodeProjector(true, policy, identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) { return snapshot, nil }))
	groups, err := projector.Project(context.Background(), nil)
	if err != nil || len(groups) != 1 || groups[0].Labels["addp_monitor_kind"] != "engine_resources" || groups[0].Labels["addp_database_container_id"] != strings.Repeat("a", 64) {
		t.Fatalf("engine source: %#v %v", groups, err)
	}
	if _, err := projector.Project(context.Background(), []NodeTarget{target}); err != ErrInvalidTarget {
		t.Fatalf("duplicate physical endpoint accepted: %v", err)
	}
	snapshot.EngineDeployments[0].Metrics = &models.PostgreSQLMetricsSource{State: "unavailable"}
	groups, err = projector.Project(context.Background(), []NodeTarget{target})
	if err != nil || len(groups) != 1 || groups[0].Labels["addp_monitor_kind"] != "host_resources" {
		t.Fatal("optional unavailable engine suppressed host")
	}
	snapshot.EngineDeployments[0].NodeID = uuid.NewString()
	if _, err := projector.Project(context.Background(), nil); err != ErrIdentityUnavailable {
		t.Fatal("foreign node admitted")
	}
}
