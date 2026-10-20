package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
)

func TestEngineMetricsPrivateProjectionDoesNotChangeAdmissionOrBrowser(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	base := discoveryTestValue(now)
	fingerprint := deploymentFingerprint(base.Scope, base.Source, base.Services[0])
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
	raw, _ := json.Marshal(public)
	if strings.Contains(string(raw), "19443") || strings.Contains(string(raw), "postgresql_metrics") || public.Scopes[0].PostgreSQLMetrics != nil {
		t.Fatal("private source leaked into browser")
	}
	if public.Candidates[0].Fingerprint != fingerprint {
		t.Fatal("source configuration changed admission fingerprint")
	}
	key := commonmodels.ObservabilityEngineSourceKey{DockerEngineID: base.Source.DockerEngineID, Project: base.Source.Project, Service: "postgres"}
	snapshot := &commonmodels.ObservabilityIdentitySnapshot{EngineDeployments: []commonmodels.ObservabilityEngineIdentity{{Scope: "infra", SourceKey: key}}}
	s := &ModuleRegistryService{deploymentDiscovery: reader}
	s.enrichEngineMetrics(context.Background(), snapshot)
	if got := snapshot.EngineDeployments[0].Metrics; got == nil || got.State != "available" || got.Endpoint != metrics.Endpoint {
		t.Fatalf("private source missing: %#v", got)
	}
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
