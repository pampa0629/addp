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
	metrics := &commonmodels.EngineMetricsSource{State: "available", Endpoint: "https://127.0.0.1:19443/metrics", ContainerID: strings.Repeat("a", 64), StartedAt: &started}
	encode := func(m *commonmodels.EngineMetricsSource) []byte {
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
		if _, exists := wire.Scopes[index]["postgresql_metrics"]; exists || scope.PostgreSQLMetrics != nil || scope.RedisMetrics != nil {
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
	snapshot := &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now, EngineDeployments: []commonmodels.ObservabilityEngineIdentity{{Scope: "infra", EngineType: "postgresql", SourceKey: key}}}
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

func TestSingleEngineMetricsExactPrivateAssociationAndFailureIsolation(t *testing.T) {
	for _, engineType := range []string{"redis", "minio"} {
		t.Run(engineType, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			base := discoveryTestValue(now)
			redis := base.Services[0]
			redis.Service, redis.EngineType = engineType, engineType
			redis.Containers = append([]models.DiscoveredDeploymentContainer(nil), redis.Containers...)
			redis.Containers[0].ContainerID = strings.Repeat("b", 64)
			redis.Containers[0].PublishedPorts = []models.EngineDeploymentPort{{Address: "127.0.0.1", Port: 16379, ContainerPort: func() int { _, port := commonmodels.PlatformEngineService(engineType); return port }(), Protocol: "tcp"}}
			base.Services = append(base.Services, redis)
			started, _ := time.Parse(time.RFC3339Nano, redis.Containers[0].StartedAt)
			metrics := commonmodels.EngineMetricsSource{State: "available", Endpoint: "https://127.0.0.1:19444/metrics", ContainerID: redis.Containers[0].ContainerID, StartedAt: &started}
			directory := t.TempDir()
			os.Chmod(directory, 0700)
			reader := &DeploymentDiscoveryService{Directory: directory}
			write := func(value commonmodels.EngineMetricsSource) {
				raw, _ := json.Marshal(base)
				var doc map[string]interface{}
				json.Unmarshal(raw, &doc)
				doc[engineType+"_metrics"] = value
				raw, _ = json.Marshal(doc)
				if err := os.WriteFile(filepath.Join(directory, "infra.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			key := commonmodels.ObservabilityEngineSourceKey{DockerEngineID: base.Source.DockerEngineID, Project: base.Source.Project, Service: engineType}
			snapshot := &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now, EngineDeployments: []commonmodels.ObservabilityEngineIdentity{{Scope: "infra", EngineType: engineType, SourceKey: key}}}
			registry := &ModuleRegistryService{deploymentDiscovery: reader}
			write(metrics)
			registry.enrichEngineMetrics(context.Background(), snapshot)
			if snapshot.EngineDeployments[0].Metrics.State != "available" {
				t.Fatalf("%s source not associated: source=%#v metrics=%#v", engineType, reader.readScope(context.Background(), "infra"), snapshot.EngineDeployments[0].Metrics)
			}
			public := reader.Read(context.Background())
			raw, _ := json.Marshal(public)
			if strings.Contains(string(raw), metrics.Endpoint) || public.Scopes[0].RedisMetrics != nil || public.Scopes[0].MinIOMetrics != nil {
				t.Fatal("private source entered browser DTO")
			}
			for _, kind := range []string{"container", "start", "future", "type", "key", "stale", "disabled"} {
				next := metrics
				snapshot.EngineDeployments[0].SourceKey = key
				snapshot.EngineDeployments[0].EngineType = engineType
				switch kind {
				case "container":
					next.ContainerID = strings.Repeat("c", 64)
				case "start":
					at := started.Add(time.Second)
					next.StartedAt = &at
				case "future":
					at := now.Add(time.Second)
					next.StartedAt = &at
				case "type":
					snapshot.EngineDeployments[0].EngineType = "postgresql"
				case "key":
					snapshot.EngineDeployments[0].SourceKey.DockerEngineID = "foreign"
				case "stale":
					base.ObservedAt = now.Add(-121 * time.Second).Format(time.RFC3339)
				case "disabled":
					base.ObservedAt = now.Format(time.RFC3339)
					next = commonmodels.EngineMetricsSource{State: "not_deployed"}
				}
				write(next)
				registry.enrichEngineMetrics(context.Background(), snapshot)
				if snapshot.EngineDeployments[0].Metrics.State == "available" {
					t.Fatal("invalid Redis source associated", kind)
				}
			}
		})
	}
}
