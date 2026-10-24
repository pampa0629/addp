package service

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/addp/common/jsonmap"
	commonmodels "github.com/addp/common/models"
	"time"
)

func parseContainerProofs(raw []byte, at time.Time) []commonmodels.ContainerProof {
	object, err := jsonmap.DecodeObjectStrict(raw, deploymentDiscoveryLimit, 5)
	if err != nil {
		return nil
	}
	// DecodeObjectStrict rejects duplicate keys; this closed spelling set also
	// rejects encoding/json's case aliases before typed decoding.
	allowed := map[string]bool{}
	for _, k := range []string{"schema", "observed_at", "proofs", "identity", "kernel_boot_id", "cgroup_id", "pid_namespace_id", "docker_engine_id", "container_id", "module_name", "role", "started_at", "stable_since", "hierarchy_digest", "cpu_cores", "memory_bytes"} {
		allowed[k] = true
	}
	var exact func(any) bool
	exact = func(value any) bool {
		switch v := value.(type) {
		case map[string]any:
			for k, x := range v {
				if !allowed[k] || !exact(x) {
					return false
				}
			}
		case []any:
			for _, x := range v {
				if !exact(x) {
					return false
				}
			}
		}
		return true
	}
	if !exact(object) {
		return nil
	}
	var doc struct {
		Schema     string                        `json:"schema"`
		ObservedAt time.Time                     `json:"observed_at"`
		Proofs     []commonmodels.ContainerProof `json:"proofs"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || doc.Schema != "addp.container-proofs/v1" || doc.Proofs == nil || len(doc.Proofs) > 64 || doc.ObservedAt.IsZero() || doc.ObservedAt.After(at) || at.Sub(doc.ObservedAt) > 90*time.Second {
		return nil
	}
	containers := map[string]bool{}
	boundaries := map[commonmodels.CgroupIdentity]bool{}
	for _, p := range doc.Proofs {
		key := p.DockerEngineID + "/" + p.ContainerID
		if p.Validate(at) != nil || !p.ObservedAt.Equal(doc.ObservedAt) || containers[key] || boundaries[p.Identity] {
			return nil
		}
		containers[key] = true
		boundaries[p.Identity] = true
	}
	return doc.Proofs
}

func (s *ModuleRegistryService) enrichContainerProofs(ctx context.Context, snapshot *commonmodels.ObservabilityIdentitySnapshot) {
	if snapshot == nil {
		return
	}
	for index := range snapshot.ModuleInstances {
		snapshot.ModuleInstances[index].Container = nil
	}
	raw, state := s.deploymentDiscovery.readDocument(ctx, "container-proofs.json")
	if state != "" {
		return
	}
	proofs := parseContainerProofs(raw, snapshot.ObservedAt)
	// Shared boundaries are rejected even if module/role differ.
	counts := map[commonmodels.CgroupIdentity]int{}
	for _, i := range snapshot.ModuleInstances {
		if i.ProcessMetrics != nil && i.ProcessMetrics.Cgroup != nil {
			counts[*i.ProcessMetrics.Cgroup]++
		}
	}
	for index := range snapshot.ModuleInstances {
		i := &snapshot.ModuleInstances[index]
		if i.ProcessMetrics == nil || i.ProcessMetrics.Cgroup == nil || i.ProcessStartedAt == nil || counts[*i.ProcessMetrics.Cgroup] != 1 {
			continue
		}
		for _, p := range proofs {
			if p.Identity == *i.ProcessMetrics.Cgroup && p.ModuleName == i.ModuleName && p.Role == i.Role &&
				!i.ProcessStartedAt.Before(p.StartedAt) && !i.ProcessStartedAt.After(p.ObservedAt) && i.LeaseExpiresAt.After(snapshot.ObservedAt) {
				copy := p
				i.Container = &copy
			}
		}
	}
}
