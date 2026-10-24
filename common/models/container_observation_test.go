package models

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestContainerProofIdentityBoundsAndFreshness(t *testing.T) {
	at := time.Now().UTC()
	cpu := .5
	mem := uint64(128 << 20)
	valid := ContainerProof{Identity: CgroupIdentity{"11111111-1111-4111-8111-111111111111", "100", "200"}, DockerEngineID: "engine-1", ContainerID: strings.Repeat("a", 64), ModuleName: "monitor", Role: "worker", StartedAt: at.Add(-time.Hour), ObservedAt: at, StableSince: at.Add(-time.Minute), HierarchyDigest: strings.Repeat("b", 64), CPUCores: &cpu, MemoryBytes: &mem}
	if valid.Validate(at) != nil {
		t.Fatal("valid proof rejected")
	}
	for _, mutate := range []func(*ContainerProof){
		func(p *ContainerProof) { p.Identity.CgroupID = "0100" }, func(p *ContainerProof) { p.Identity.PIDNamespaceID = "0" }, func(p *ContainerProof) { p.Identity.BootID = strings.ToUpper(p.Identity.BootID) + "x" },
		func(p *ContainerProof) { p.ObservedAt = at.Add(-91 * time.Second) }, func(p *ContainerProof) { p.ObservedAt = at.Add(time.Second) }, func(p *ContainerProof) { p.StableSince = at.Add(time.Second) }, func(p *ContainerProof) { p.StartedAt = at.Add(time.Second) },
		func(p *ContainerProof) { v := math.NaN(); p.CPUCores = &v }, func(p *ContainerProof) { v := 0.; p.CPUCores = &v }, func(p *ContainerProof) { v := uint64(1 << 53); p.MemoryBytes = &v }, func(p *ContainerProof) { p.ContainerID = "short" },
	} {
		p := valid
		mutate(&p)
		if p.Validate(at) == nil {
			t.Fatalf("invalid proof accepted: %+v", p)
		}
	}
	valid.CPUCores = nil
	valid.MemoryBytes = nil
	if valid.Validate(at) != nil {
		t.Fatal("unlimited is a valid boundary without finite bounds")
	}
}

func TestCgroupDeclarationHasValueEqualityAndIndependentCopies(t *testing.T) {
	original := &ProcessMetricsDeclaration{SchemaVersion: ProcessMetricsSchema, Endpoint: "https://localhost:9443/metrics", Cgroup: &CgroupIdentity{"11111111-1111-4111-8111-111111111111", "100", "200"}}
	copy := original.Clone()
	if original.Validate() != nil || !original.Equal(copy) {
		t.Fatal("equivalent declaration rejected")
	}
	copy.Cgroup.CgroupID = "101"
	if original.Equal(copy) || original.Cgroup.CgroupID != "100" {
		t.Fatal("identity change or alias accepted")
	}
	copy.Cgroup.CgroupID = "01"
	if copy.Validate() == nil {
		t.Fatal("invalid cgroup declaration")
	}
}
