package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
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

func (i ObservabilityComponentIdentity) IsCDC() bool {
	return i.Kind == "redpanda" || i.Kind == "kafka-connect"
}

func (s *ObservabilityComponentSnapshot) Validate(at time.Time) error {
	if s == nil || s.Items == nil || len(s.Items) > 3 ||
		(s.State != "available" && s.State != "unavailable") || (s.State == "unavailable" && len(s.Items) != 0) {
		return errors.New("invalid infrastructure component snapshot")
	}
	seen := map[string]bool{}
	for _, item := range s.Items {
		if item.Validate() != nil || item.StartedAt.After(at) || seen[item.Kind] {
			return errors.New("invalid infrastructure component snapshot item")
		}
		seen[item.Kind] = true
	}
	return nil
}

func (i ObservabilityComponentIdentity) Validate() error {
	if !observabilityDockerEngineID.MatchString(i.DockerEngineID) || i.Project != "addp-infra" ||
		(!i.IsCDC() && i.Kind != "meilisearch") || !engineMetricsContainerID.MatchString(i.ContainerID) ||
		i.StartedAt.IsZero() || i.Metrics == nil || i.Metrics.Validate() != nil {
		return errors.New("invalid infrastructure component identity")
	}
	if i.Metrics.State == "available" && (i.Metrics.ContainerID != i.ContainerID || !i.Metrics.StartedAt.Equal(i.StartedAt)) {
		return errors.New("infrastructure metrics runtime mismatch")
	}
	return nil
}
