package models

import "time"

type Projection struct {
	Generation            string `gorm:"primaryKey"`
	PredecessorGeneration *string
	TenantID              uint64
	OntologyID            string
	Revision              uint64
	Digest                string
	ExecutionID           string
	BaselineVersion       uint64
	Status                string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (Projection) TableName() string { return "ontology.projections" }

type ProjectionEvent struct {
	ID                   int64 `gorm:"primaryKey"`
	Generation           string
	Action               string
	Attempt              int
	ActorPrincipalID     int64
	ActorMembershipID    int64
	AuthorizationVersion int64
	CreatedAt            time.Time
}

func (ProjectionEvent) TableName() string { return "ontology.projection_events" }
