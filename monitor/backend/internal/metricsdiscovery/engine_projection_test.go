package metricsdiscovery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

func TestEngineProjectionUsesSharedAdmissionAndReservations(t *testing.T) {
	target := testNodeTarget()
	snapshot := testSnapshot(target)
	started := time.Now().UTC().Add(-time.Hour)
	snapshot.EngineDeployments = []models.ObservabilityEngineIdentity{{DeploymentID: uuid.NewString(), Version: 1, Scope: "infra", EngineType: "postgresql", NodeID: target.Subject.NodeID,
		SourceKey: models.ObservabilityEngineSourceKey{DockerEngineID: "daemon", Project: "addp-infra", Service: "postgres"},
		Metrics:   &models.PostgreSQLMetricsSource{State: "available", Endpoint: target.Source.Endpoint, ContainerID: strings.Repeat("a", 64), StartedAt: &started}}}
	policy := testPolicy(t, testSourcePKI(t, true), 9443)
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
