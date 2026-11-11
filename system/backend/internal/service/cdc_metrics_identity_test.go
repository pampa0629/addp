package service

import (
	"context"
	"encoding/json"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCDCPrivateFactsDoNotCreateEnginesOrExposeEndpoints(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	base := discoveryTestValue(now)
	c := base.Services[0].Containers[0]
	base.Services = []models.DiscoveredDeploymentService{{Service: "redpanda", EngineType: "unclassified", Containers: []models.DiscoveredDeploymentContainer{c}}}
	started, _ := time.Parse(time.RFC3339Nano, c.StartedAt)
	metrics := commonmodels.EngineMetricsSource{State: "available", Endpoint: "https://127.0.0.1:19443/metrics", ContainerID: c.ContainerID, StartedAt: &started}
	raw, _ := json.Marshal(base)
	document := map[string]any{}
	json.Unmarshal(raw, &document)
	document["cdc_metrics"] = map[string]any{"redpanda": metrics}
	raw, _ = json.Marshal(document)
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	os.WriteFile(filepath.Join(dir, "infra.json"), raw, 0600)
	discovery := &DeploymentDiscoveryService{Directory: dir}
	service := &ModuleRegistryService{deploymentDiscovery: discovery}
	snapshot := &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now}
	service.enrichInfrastructureComponents(context.Background(), snapshot)
	p := snapshot.InfrastructureComponents
	if p.State != "available" || len(p.Items) != 1 || p.Items[0].Metrics.State != "available" || len(snapshot.EngineDeployments) != 0 {
		t.Fatal(p)
	}
	public, _ := json.Marshal(discovery.Read(context.Background()))
	if strings.Contains(string(public), "19443") || strings.Contains(string(public), "cdc_metrics") {
		t.Fatal("private endpoint exposed")
	}
	metrics.ContainerID = strings.Repeat("f", 64)
	document["cdc_metrics"] = map[string]any{"redpanda": metrics}
	raw, _ = json.Marshal(document)
	os.WriteFile(filepath.Join(dir, "infra.json"), raw, 0600)
	service.enrichInfrastructureComponents(context.Background(), snapshot)
	if snapshot.InfrastructureComponents.Items[0].Metrics.State != "unavailable" {
		t.Fatal("stale container source admitted")
	}
	os.Remove(filepath.Join(dir, "infra.json"))
	service.enrichInfrastructureComponents(context.Background(), snapshot)
	if snapshot.InfrastructureComponents.State != "unavailable" || len(snapshot.InfrastructureComponents.Items) != 0 {
		t.Fatal("discovery outage disguised as empty success")
	}
}
