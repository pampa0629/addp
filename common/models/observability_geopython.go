package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// RuntimeMetricsEngineType binds a source to the platform's own registered runtime type.
func RuntimeMetricsEngineType(kind string) string {
	switch kind {
	case "geopython":
		return "geopython_workflow"
	case "spark":
		return "spark_workflow"
	case "model3d":
		return "model3d_workflow"
	case "pointcloud":
		return "pointcloud_workflow"
	case "document":
		return "document_workflow"
	case "supermap":
		return "supermap_workflow"
	case "duckdb":
		return "duckdb"
	case "jupyter":
		return "jupyter"
	case "inference":
		return "inference_runtime"
	default:
		return ""
	}
}

const RuntimeMetricsLeaseDuration = 90 * time.Second

type RuntimeMetricsDeclaration struct {
	InstanceID string    `json:"instance_id"`
	StartedAt  time.Time `json:"started_at"`
	Endpoint   string    `json:"endpoint"`
}

func (d RuntimeMetricsDeclaration) Validate(at time.Time) error {
	id, err := uuid.Parse(d.InstanceID)
	endpoint := ProcessMetricsDeclaration{SchemaVersion: ProcessMetricsSchema, Endpoint: d.Endpoint}
	if err != nil || id == uuid.Nil || id.String() != d.InstanceID || d.StartedAt.Year() < 1000 ||
		d.StartedAt.After(at) || endpoint.Validate() != nil {
		return errors.New("invalid runtime metrics declaration")
	}
	return nil
}

// RuntimeMetricsIdentity is published solely in the private observability snapshot.
type RuntimeMetricsIdentity struct {
	RuntimeMetricsDeclaration
	EngineID       uint      `json:"engine_id" gorm:"primaryKey;autoIncrement:false"`
	Kind           string    `json:"kind" gorm:"-"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}
