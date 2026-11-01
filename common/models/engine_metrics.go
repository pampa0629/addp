package models

import (
	"errors"
	"regexp"
	"time"
)

// EngineMetricsSource is a private deployment observation. It is never a
// tenant engine, an SQL connection, or a browser-facing declaration.
type EngineMetricsSource struct {
	State       string     `json:"state"`
	Endpoint    string     `json:"endpoint,omitempty"`
	ContainerID string     `json:"container_id,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
}

var engineMetricsContainerID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *EngineMetricsSource) Validate() error {
	if s == nil {
		return nil
	}
	switch s.State {
	case "not_deployed", "unavailable":
		if s.Endpoint == "" && s.ContainerID == "" && s.StartedAt == nil {
			return nil
		}
	case "available":
		if engineMetricsContainerID.MatchString(s.ContainerID) && s.StartedAt != nil && !s.StartedAt.IsZero() &&
			(&ProcessMetricsDeclaration{SchemaVersion: ProcessMetricsSchema, Endpoint: s.Endpoint}).Validate() == nil {
			return nil
		}
	}
	return errors.New("invalid engine metrics source")
}

// PlatformEngineService is the bounded platform deployment directory, not the
// tenant engine plugin catalog. Each supported deployment is one container.
func PlatformEngineService(engineType string) (string, int) {
	switch engineType {
	case "postgresql":
		return "postgres", 5432
	case "redis":
		return "redis", 6379
	default:
		return "", 0
	}
}
