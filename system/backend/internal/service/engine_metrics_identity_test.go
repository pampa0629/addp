package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
)

func TestEngineMetricsPrivateProjectionDoesNotChangeAdmissionOrBrowser(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	base := discoveryTestValue(now)
	// Keep the discovery snapshot fresh, but reproduce the failed CI's exact
	// public fingerprint independently of the wall clock.
	base.Services[0].Containers[0].StartedAt = "2026-10-30T00:29:17Z"
	fingerprint := deploymentFingerprint(base.Scope, base.Source, base.Services[0])
	if fingerprint != "06e61ece304ab35633d28c1f97252ab7a131b4fcd9346360db5b194430ecd96b" {
		t.Fatalf("private-port collision fixture changed: %s", fingerprint)
	}
	started, _ := time.Parse(time.RFC3339, base.Services[0].Containers[0].StartedAt)
	metrics := &commonmodels.PostgreSQLMetricsSource{State: "available", Endpoint: "https://127.0.0.1:19443/metrics", ContainerID: strings.Repeat("a", 64), StartedAt: &started}
	encode := func(m *commonmodels.PostgreSQLMetricsSource) []byte {
		raw, _ := json.Marshal(base)
		var document map[string]interface{}
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		document["postgresql_metrics"] = m
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	reader := &DeploymentDiscoveryService{Directory: directory}
	if err := os.WriteFile(filepath.Join(directory, "infra.json"), encode(metrics), 0600); err != nil {
		t.Fatal(err)
	}
	public := reader.Read(context.Background())
	want := models.DeploymentDiscovery{
		Scopes: []models.DeploymentDiscoveryScope{base, {
			Schema: "addp.deployment-discovery/v2", Scope: "business", State: "missing", Reason: "source_missing",
			Services: []models.DiscoveredDeploymentService{},
		}},
		Candidates: []models.EngineDeploymentCandidate{{Scope: "infra", Service: "postgres", Fingerprint: fingerprint, Eligible: true}},
	}
	if !reflect.DeepEqual(public, want) {
		t.Fatalf("public discovery projection changed: %#v", public)
	}
	raw, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Scopes []map[string]json.RawMessage `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Scopes) != len(public.Scopes) {
		t.Fatal("public discovery scope shape changed")
	}
	for index, scope := range public.Scopes {
		if _, exists := wire.Scopes[index]["postgresql_metrics"]; exists || scope.PostgreSQLMetrics != nil {
			t.Fatalf("private source field leaked into browser scope %s", scope.Scope)
		}
	}
	if strings.Contains(string(raw), metrics.Endpoint) {
		t.Fatal("complete private endpoint leaked into browser")
	}
	if public.Candidates[0].Fingerprint != fingerprint {
		t.Fatal("source configuration changed admission fingerprint")
	}
	key := commonmodels.ObservabilityEngineSourceKey{DockerEngineID: base.Source.DockerEngineID, Project: base.Source.Project, Service: "postgres"}
	snapshot := &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now, EngineDeployments: []commonmodels.ObservabilityEngineIdentity{{Scope: "infra", SourceKey: key}}}
	s := &ModuleRegistryService{deploymentDiscovery: reader}
	s.enrichEngineMetrics(context.Background(), snapshot)
	if got := snapshot.EngineDeployments[0].Metrics; got == nil || got.State != "available" || got.Endpoint != metrics.Endpoint {
		t.Fatalf("private source missing: %#v", got)
	}
	snapshot.ObservedAt = started.Add(-time.Second)
	s.enrichEngineMetrics(context.Background(), snapshot)
	if snapshot.EngineDeployments[0].Metrics.State != "unavailable" {
		t.Fatal("source newer than the identity snapshot was admitted")
	}
	snapshot.ObservedAt = now
	snapshot.EngineDeployments[0].SourceKey.DockerEngineID = "foreign"
	s.enrichEngineMetrics(context.Background(), snapshot)
	if snapshot.EngineDeployments[0].Metrics.State != "unavailable" {
		t.Fatal("foreign daemon source admitted")
	}
	wrong := *metrics
	wrong.ContainerID = strings.Repeat("b", 64)
	parsed := parseDeploymentDiscovery(encode(&wrong), "infra", now)
	if parsed.State != "available" || parsed.PostgreSQLMetrics.State != "unavailable" {
		t.Fatal("invalid optional source cleared valid deployment")
	}
	parsed = parseDeploymentDiscovery([]byte(strings.Replace(string(encode(metrics)), "/v2", "/v1", 1)), "infra", now)
	if parsed.State == "available" {
		t.Fatal("old protocol accepted")
	}
}
