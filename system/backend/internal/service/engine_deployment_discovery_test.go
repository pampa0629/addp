package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
)

func discoveryTestValue(now time.Time) models.DeploymentDiscoveryScope {
	return models.DeploymentDiscoveryScope{Schema: "addp.deployment-discovery/v1", Scope: "infra", State: "available", ObservedAt: now.Format("2006-01-02T15:04:05Z"),
		Source:   &models.BusinessDeploymentSource{Type: "docker_compose", Runtime: "desktop_linux_vm", DockerEngineID: "daemon-identity", Project: "addp-infra"},
		Services: []models.DiscoveredDeploymentService{{Service: "postgres", EngineType: "postgresql", Containers: []models.DiscoveredDeploymentContainer{{ContainerID: strings.Repeat("a", 64), Status: "running", StartedAt: now.Add(-time.Hour).Format(time.RFC3339), PublishedPorts: []models.BusinessDeploymentPort{{Address: "127.0.0.1", Port: 25432, ContainerPort: 5432, Protocol: "tcp"}}}}}}}
}

func TestDeploymentDiscoverySourceValidationAndStaleness(t *testing.T) {
	now := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	encode := func(value models.DeploymentDiscoveryScope) []byte {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	base := discoveryTestValue(now)
	if got := parseDeploymentDiscovery(encode(base), "infra", now); got.State != "available" || len(got.Services) != 1 || got.Services[0].Containers[0].PublishedPorts[0].Port != 25432 {
		t.Fatalf("valid source = %#v", got)
	}
	stale := parseDeploymentDiscovery(encode(base), "infra", now.Add(121*time.Second))
	if stale.State != "stale" || stale.Source != nil || len(stale.Services) != 0 || stale.ObservedAt != base.ObservedAt {
		t.Fatalf("stale source retained candidates: %#v", stale)
	}
	if got := parseDeploymentDiscovery(encode(base), "infra", now.Add(120*time.Second)); got.State != "available" {
		t.Fatal("120-second boundary rejected")
	}
	mutations := map[string]func(*models.DeploymentDiscoveryScope){
		"wrong scope":      func(v *models.DeploymentDiscoveryScope) { v.Scope = "business" },
		"wrong project":    func(v *models.DeploymentDiscoveryScope) { v.Source.Project = "business" },
		"unknown protocol": func(v *models.DeploymentDiscoveryScope) { v.Schema = "other" },
		"unknown runtime":  func(v *models.DeploymentDiscoveryScope) { v.Source.Runtime = "remote" },
		"future source": func(v *models.DeploymentDiscoveryScope) {
			v.ObservedAt = now.Add(31 * time.Second).Format(time.RFC3339)
		},
		"missing list":      func(v *models.DeploymentDiscoveryScope) { v.Services = nil },
		"missing identity":  func(v *models.DeploymentDiscoveryScope) { v.Source.DockerEngineID = "" },
		"duplicate service": func(v *models.DeploymentDiscoveryScope) { v.Services = append(v.Services, v.Services[0]) },
		"duplicate container": func(v *models.DeploymentDiscoveryScope) {
			v.Services[0].Containers = append(v.Services[0].Containers, v.Services[0].Containers[0])
		},
		"invalid state": func(v *models.DeploymentDiscoveryScope) { v.Services[0].Containers[0].Status = "ready" },
		"missing start": func(v *models.DeploymentDiscoveryScope) { v.Services[0].Containers[0].StartedAt = "" },
		"bad start":     func(v *models.DeploymentDiscoveryScope) { v.Services[0].Containers[0].StartedAt = "invalid" },
		"invalid port":  func(v *models.DeploymentDiscoveryScope) { v.Services[0].Containers[0].PublishedPorts[0].Port = 0 },
		"duplicate port": func(v *models.DeploymentDiscoveryScope) {
			v.Services[0].Containers[0].PublishedPorts = append(v.Services[0].Containers[0].PublishedPorts, v.Services[0].Containers[0].PublishedPorts[0])
		},
		"scoped ip": func(v *models.DeploymentDiscoveryScope) {
			v.Services[0].Containers[0].PublishedPorts[0].Address = "fe80::1%zone"
		},
		"negative restarts":      func(v *models.DeploymentDiscoveryScope) { v.Services[0].Containers[0].RestartCount = -1 },
		"failed with candidates": func(v *models.DeploymentDiscoveryScope) { v.State = "unavailable"; v.Reason = "source_unavailable" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := discoveryTestValue(now)
			mutate(&value)
			got := parseDeploymentDiscovery(encode(value), "infra", now)
			if got.State != "unavailable" || got.Source != nil || len(got.Services) != 0 {
				t.Fatalf("invalid source retained evidence: %#v", got)
			}
		})
	}
	for _, raw := range []string{`{"schema":"private-sentinel","Schema":"other"}`, string(encode(base)) + ` {}`, strings.Replace(string(encode(base)), `"schema":`, `"schema":"duplicate","schema":`, 1), strings.Replace(string(encode(base)), `"scope":`, `"Scope":`, 1), strings.Replace(string(encode(base)), `"source":{`, `"source":{"password":"private-sentinel",`, 1), strings.Repeat("x", deploymentDiscoveryLimit+1)} {
		got := parseDeploymentDiscovery([]byte(raw), "infra", now)
		if got.State != "unavailable" || strings.Contains(string(encode(got)), "private-sentinel") {
			t.Fatal("malformed source was accepted or leaked")
		}
	}
	base.Services = []models.DiscoveredDeploymentService{}
	if got := parseDeploymentDiscovery(encode(base), "infra", now); got.State != "available" || got.Services == nil {
		t.Fatal("successful empty discovery lost")
	}
	base.Source, base.State, base.Reason = nil, "unavailable", "source_unavailable"
	if got := parseDeploymentDiscovery(encode(base), "infra", now); got.State != "unavailable" || got.Reason != "source_unavailable" {
		t.Fatal("explicit failure lost")
	}
}

func TestDeploymentDiscoveryReadsOnlyPrivateFixedScopeFiles(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	reader := &DeploymentDiscoveryService{Directory: directory}
	write := func(name string, raw []byte, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(directory, name), raw, mode); err != nil {
			t.Fatal(err)
		}
	}
	valid, _ := json.Marshal(discoveryTestValue(time.Now().UTC()))
	write("infra.json", valid, 0600)
	write("business.json", []byte(`{"password":"private-sentinel"}`), 0600)
	if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private fixture directory permissions: info=%v err=%v", info, err)
	}
	result := reader.Read(context.Background())
	if len(result.Scopes) != 2 || result.Scopes[0].State != "available" || result.Scopes[1].State != "unavailable" {
		t.Fatalf("scope isolation = %#v", result)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "private-sentinel") || strings.Contains(string(raw), directory) {
		t.Fatal("source error exposed private facts")
	}
	if got := (&DeploymentDiscoveryService{}).Read(context.Background()); got.Scopes[0].State != "unconfigured" {
		t.Fatal("missing configuration was hidden")
	}
	if err := os.Remove(filepath.Join(directory, "business.json")); err != nil {
		t.Fatal(err)
	}
	if got := reader.Read(context.Background()); got.Scopes[1].State != "missing" {
		t.Fatal("missing source was hidden")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := reader.Read(ctx); got.Scopes[0].State != "unavailable" || len(got.Scopes[0].Services) != 0 {
		t.Fatal("cancelled read retained candidates")
	}
	if err := os.Chmod(filepath.Join(directory, "infra.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := reader.Read(context.Background()); got.Scopes[0].State != "unavailable" {
		t.Fatal("shared file accepted")
	}
	if err := os.Remove(filepath.Join(directory, "infra.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "outside"), filepath.Join(directory, "infra.json")); err != nil {
		t.Fatal(err)
	}
	if got := reader.Read(context.Background()); got.Scopes[0].State != "unavailable" {
		t.Fatal("symlink source accepted")
	}
	if err := os.Remove(filepath.Join(directory, "infra.json")); err != nil {
		t.Fatal(err)
	}
	write("valid-source", valid, 0600)
	if err := os.Symlink("valid-source", filepath.Join(directory, "infra.json")); err != nil {
		t.Fatal(err)
	}
	if got := reader.Read(context.Background()); got.Scopes[0].State != "unavailable" {
		t.Fatal("relative symlink to valid source accepted")
	}
	if err := os.Remove(filepath.Join(directory, "infra.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(directory, "infra.json"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := reader.Read(context.Background()); got.Scopes[0].State != "unavailable" {
		t.Fatal("FIFO source accepted")
	}
	if err := os.Remove(filepath.Join(directory, "infra.json")); err != nil {
		t.Fatal(err)
	}
	write("infra.json", []byte(strings.Repeat("x", deploymentDiscoveryLimit+1)), 0600)
	if got := reader.Read(context.Background()); got.Scopes[0].State != "unavailable" {
		t.Fatal("oversized source accepted")
	}
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if got := reader.Read(context.Background()); got.Scopes[1].State != "unavailable" {
		t.Fatal("shared directory accepted")
	}
}
