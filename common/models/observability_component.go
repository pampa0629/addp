package models

import (
	"errors"
	"github.com/google/uuid"
	"time"
)

// ObservabilityComponentSnapshot is a private current projection, not an inventory or an engine grant.
type ObservabilityComponentSnapshot struct {
	State string                           `json:"state"`
	Items []ObservabilityComponentIdentity `json:"items"`
}

type ObservabilityComponentIdentity struct {
	DockerEngineID string    `json:"docker_engine_id"`
	Project        string    `json:"project"`
	Kind           string    `json:"kind"`
	ContainerID    string    `json:"container_id"`
	StartedAt      time.Time `json:"started_at"`
	// Reuse the fixed private metrics transport shape; this does not declare an engine.
	Metrics *EngineMetricsSource `json:"metrics"`
}

func (i ObservabilityComponentIdentity) ID() string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(i.DockerEngineID+"\x00"+i.Project+"\x00"+i.Kind)).String()
}

func (i ObservabilityComponentIdentity) Validate() error {
	if !observabilityDockerEngineID.MatchString(i.DockerEngineID) || i.Project != "addp-infra" ||
		(i.Kind != "redpanda" && i.Kind != "kafka-connect") || !engineMetricsContainerID.MatchString(i.ContainerID) ||
		i.StartedAt.IsZero() || i.Metrics == nil || i.Metrics.Validate() != nil {
		return errors.New("invalid infrastructure component identity")
	}
	if i.Metrics.State == "available" && (i.Metrics.ContainerID != i.ContainerID || !i.Metrics.StartedAt.Equal(i.StartedAt)) {
		return errors.New("infrastructure metrics runtime mismatch")
	}
	return nil
}
