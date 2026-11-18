package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const GeoPythonMetricsLeaseDuration = 90 * time.Second

type GeoPythonMetricsDeclaration struct {
	InstanceID string    `json:"instance_id"`
	StartedAt  time.Time `json:"started_at"`
	Endpoint   string    `json:"endpoint"`
}

func (d GeoPythonMetricsDeclaration) Validate(at time.Time) error {
	id, err := uuid.Parse(d.InstanceID)
	endpoint := ProcessMetricsDeclaration{SchemaVersion: ProcessMetricsSchema, Endpoint: d.Endpoint}
	if err != nil || id == uuid.Nil || id.String() != d.InstanceID || d.StartedAt.Year() < 1000 ||
		d.StartedAt.After(at) || endpoint.Validate() != nil {
		return errors.New("invalid GeoPython metrics declaration")
	}
	return nil
}

// GeoPythonMetricsIdentity is published solely in the private observability snapshot.
type GeoPythonMetricsIdentity struct {
	GeoPythonMetricsDeclaration
	EngineID       uint      `json:"engine_id"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}
