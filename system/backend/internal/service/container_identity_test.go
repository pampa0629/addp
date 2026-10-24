package service

import (
	"context"
	"encoding/json"
	commonmodels "github.com/addp/common/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContainerProofExactCurrentAssociationAndPrivateRead(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	started := now.Add(-time.Minute)
	cpu := .5
	mem := uint64(128 << 20)
	boundary := commonmodels.CgroupIdentity{BootID: "11111111-1111-4111-8111-111111111111", CgroupID: "100", PIDNamespaceID: "200"}
	proof := commonmodels.ContainerProof{Identity: boundary, DockerEngineID: "engine", ContainerID: strings.Repeat("a", 64), ModuleName: "monitor", Role: "worker", StartedAt: now.Add(-time.Hour), ObservedAt: now, StableSince: now.Add(-2 * time.Minute), HierarchyDigest: strings.Repeat("b", 64), CPUCores: &cpu, MemoryBytes: &mem}
	identity := commonmodels.ObservabilityModuleIdentity{ModuleName: "monitor", InstanceID: "actual", Role: "worker", LeaseExpiresAt: now.Add(time.Minute), ProcessStartedAt: &started, ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://localhost:9443/metrics", Cgroup: &boundary}}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	registry := &ModuleRegistryService{deploymentDiscovery: &DeploymentDiscoveryService{Directory: directory}}
	write := func(proofs []commonmodels.ContainerProof) {
		raw, _ := json.Marshal(map[string]any{"schema": "addp.container-proofs/v1", "observed_at": now, "proofs": proofs})
		if os.WriteFile(filepath.Join(directory, "container-proofs.json"), raw, 0600) != nil {
			t.Fatal("write")
		}
	}
	write([]commonmodels.ContainerProof{proof})
	snapshot := func() *commonmodels.ObservabilityIdentitySnapshot {
		return &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now, ModuleInstances: []commonmodels.ObservabilityModuleIdentity{identity}}
	}
	good := snapshot()
	registry.enrichContainerProofs(context.Background(), good)
	if good.ModuleInstances[0].Container == nil {
		t.Fatal("matching proof lost")
	}
	for _, mutate := range []func(*commonmodels.ObservabilityIdentitySnapshot){
		func(s *commonmodels.ObservabilityIdentitySnapshot) { s.ModuleInstances[0].Role = "backend" },
		func(s *commonmodels.ObservabilityIdentitySnapshot) { s.ModuleInstances[0].LeaseExpiresAt = now },
		func(s *commonmodels.ObservabilityIdentitySnapshot) {
			old := now.Add(-2 * time.Hour)
			s.ModuleInstances[0].ProcessStartedAt = &old
		},
		func(s *commonmodels.ObservabilityIdentitySnapshot) {
			copy := identity
			copy.InstanceID = "other"
			s.ModuleInstances = append(s.ModuleInstances, copy)
		},
		func(s *commonmodels.ObservabilityIdentitySnapshot) { s.ObservedAt = now.Add(91 * time.Second) },
	} {
		s := snapshot()
		mutate(s)
		registry.enrichContainerProofs(context.Background(), s)
		for _, i := range s.ModuleInstances {
			if i.Container != nil {
				t.Fatal("unverified/shared/stale association accepted")
			}
		}
	}
	write([]commonmodels.ContainerProof{proof, proof})
	bad := snapshot()
	registry.enrichContainerProofs(context.Background(), bad)
	if bad.ModuleInstances[0].Container != nil {
		t.Fatal("duplicate proof accepted")
	}
	write([]commonmodels.ContainerProof{proof})
	if os.Chmod(filepath.Join(directory, "container-proofs.json"), 0644) != nil {
		t.Fatal("chmod")
	}
	bad = snapshot()
	registry.enrichContainerProofs(context.Background(), bad)
	if bad.ModuleInstances[0].Container != nil {
		t.Fatal("public proof accepted")
	}
}

func TestContainerProofDocumentRejectsAmbiguousFields(t *testing.T) {
	now := time.Now().UTC()
	at := now.Format(time.RFC3339Nano)
	for _, raw := range []string{`{"schema":"addp.container-proofs/v1","schema":"addp.container-proofs/v1","observed_at":"` + at + `","proofs":[]}`, `{"Schema":"addp.container-proofs/v1","observed_at":"` + at + `","proofs":[]}`, `{"schema":"addp.container-proofs/v1","observed_at":"` + at + `","proofs":[],"credentials":"sentinel"}`} {
		if parseContainerProofs([]byte(raw), now) != nil {
			t.Fatal("ambiguous proof accepted")
		}
	}
}
