package models

import (
	"errors"
	"github.com/google/uuid"
	"math"
	"regexp"
	"strconv"
	"time"
)

// CgroupIdentity is a kernel boundary, never a user-supplied container name.
type CgroupIdentity struct {
	BootID         string `json:"kernel_boot_id"`
	CgroupID       string `json:"cgroup_id"`
	PIDNamespaceID string `json:"pid_namespace_id"`
}

func (i CgroupIdentity) Validate() error {
	boot, err := uuid.Parse(i.BootID)
	if err != nil || boot == uuid.Nil || boot.String() != i.BootID {
		return errors.New("invalid cgroup identity")
	}
	for _, s := range []string{i.CgroupID, i.PIDNamespaceID} {
		n, e := strconv.ParseUint(s, 10, 64)
		if e != nil || n == 0 || strconv.FormatUint(n, 10) != s {
			return errors.New("invalid cgroup identity")
		}
	}
	return nil
}

// ContainerProof is deployment-owned, private evidence. Nil limits mean no
// finite verified hard upper bound, not zero. Never serialize into browser DTOs.
type ContainerProof struct {
	Identity        CgroupIdentity `json:"identity"`
	DockerEngineID  string         `json:"docker_engine_id"`
	ContainerID     string         `json:"container_id"`
	ModuleName      string         `json:"module_name"`
	Role            string         `json:"role"`
	StartedAt       time.Time      `json:"started_at"`
	ObservedAt      time.Time      `json:"observed_at"`
	StableSince     time.Time      `json:"stable_since"`
	HierarchyDigest string         `json:"hierarchy_digest"`
	CPUCores        *float64       `json:"cpu_cores"`
	MemoryBytes     *uint64        `json:"memory_bytes"`
}

var containerDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (p *ContainerProof) Validate(at time.Time) error {
	if p == nil || p.Identity.Validate() != nil || !observabilityDockerEngineID.MatchString(p.DockerEngineID) ||
		!containerDigest.MatchString(p.ContainerID) || !containerDigest.MatchString(p.HierarchyDigest) ||
		!regexp.MustCompile(`^[a-z][a-z0-9_-]{0,49}$`).MatchString(p.ModuleName) ||
		p.StartedAt.IsZero() || p.ObservedAt.Before(p.StartedAt) || p.StableSince.Before(p.StartedAt) || p.StableSince.After(p.ObservedAt) ||
		p.ObservedAt.After(at) || at.Sub(p.ObservedAt) > 90*time.Second {
		return errors.New("invalid container proof")
	}
	switch p.Role {
	case "backend", "worker", "scheduler", "ingress":
	default:
		return errors.New("invalid container proof")
	}
	if p.CPUCores != nil && (*p.CPUCores <= 0 || math.IsNaN(*p.CPUCores) || math.IsInf(*p.CPUCores, 0) || *p.CPUCores > 1<<20) {
		return errors.New("invalid container cpu bound")
	}
	if p.MemoryBytes != nil && (*p.MemoryBytes == 0 || *p.MemoryBytes > 1<<53-1) {
		return errors.New("invalid container memory bound")
	}
	return nil
}
