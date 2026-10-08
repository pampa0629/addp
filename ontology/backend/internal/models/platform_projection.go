package models

import "time"

// PlatformProjection is a synchronous publication attempt, not a Tenant task.
type PlatformProjection struct {
	Generation                string `gorm:"primaryKey"`
	Capability                string
	Revision                  uint64
	Digest                    string
	BaselineActivationVersion uint64
	Status                    string
	CreatedAt                 time.Time
}

func (PlatformProjection) TableName() string { return "ontology.platform_projections" }

type PlatformProjectionEvent struct {
	ID                   int64 `gorm:"primaryKey"`
	Generation           string
	Action               string
	ActorPrincipalID     int64
	AuthorizationVersion int64
	CreatedAt            time.Time
}

func (PlatformProjectionEvent) TableName() string { return "ontology.platform_projection_events" }
