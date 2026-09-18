package models

import "time"

const (
	Draft              = "draft"
	InReview           = "in_review"
	Published          = "published"
	Withdrawn          = "withdrawn"
	Module             = "ontology"
	ProjectionTaskType = "semantic_projection"
)

// Actor is supplied by the authenticated owner boundary, not by an LLM
// or untrusted request body. These facts do not confer permissions themselves.
type Actor struct {
	TenantID             uint64
	PrincipalID          int64
	MembershipID         int64
	AuthorizationVersion int64
}

type Ontology struct {
	TenantID          uint64 `gorm:"primaryKey"`
	OntologyID        string `gorm:"primaryKey"`
	LastRevision      uint64
	ActivationVersion uint64 `gorm:"default:1"`
	ActiveRevision    *uint64
	ActiveGeneration  *string
}

func (Ontology) TableName() string { return "ontology.ontologies" }

type Revision struct {
	TenantID   uint64 `gorm:"primaryKey"`
	OntologyID string `gorm:"primaryKey"`
	Revision   uint64 `gorm:"primaryKey"`
	Version    uint64
	Status     string
	Payload    string
	Digest     string
	// Immutable first-publication provenance, never the active/current build.
	// All execution and admission resolve an exact models.Projection instead.
	BuildExecutionID *string
	Generation       *string
	PublishedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (Revision) TableName() string { return "ontology.revisions" }

type RevisionEvent struct {
	ID                   int64 `gorm:"primaryKey"`
	TenantID             uint64
	OntologyID           string
	Revision             uint64
	Version              uint64
	Action               string
	FromState            string
	ToState              string
	Digest               string
	ActorPrincipalID     int64
	ActorMembershipID    int64
	AuthorizationVersion int64
	CreatedAt            time.Time
}

func (RevisionEvent) TableName() string { return "ontology.revision_events" }
