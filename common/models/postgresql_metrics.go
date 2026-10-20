package models

import (
	"errors"
	"regexp"
	"time"
)

// PostgreSQLMetricsSource is a private deployment observation. It is never a
// tenant engine, an SQL connection, or a browser-facing declaration.
type PostgreSQLMetricsSource struct {
	State       string     `json:"state"`
	Endpoint    string     `json:"endpoint,omitempty"`
	ContainerID string     `json:"container_id,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
}

var postgresqlContainerID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *PostgreSQLMetricsSource) Validate() error {
	if s == nil {
		return nil
	}
	switch s.State {
	case "not_deployed", "unavailable":
		if s.Endpoint == "" && s.ContainerID == "" && s.StartedAt == nil {
			return nil
		}
	case "available":
		if postgresqlContainerID.MatchString(s.ContainerID) && s.StartedAt != nil && !s.StartedAt.IsZero() &&
			(&ProcessMetricsDeclaration{SchemaVersion: ProcessMetricsSchema, Endpoint: s.Endpoint}).Validate() == nil {
			return nil
		}
	}
	return errors.New("invalid PostgreSQL metrics source")
}
