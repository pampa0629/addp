package models

import "time"

// PlatformActor carries already-authorized platform boundary facts. It is not
// an authentication or permission check and cannot represent a Tenant actor.
type PlatformActor struct {
	ContextType          string
	PrincipalID          int64
	PrincipalType        string
	AuthorizationVersion int64
}

type PlatformCapability struct {
	Capability        string `gorm:"primaryKey"`
	LastRevision      uint64
	PublishGeneration *string
	ActiveRevision    *uint64
	ActiveGeneration  *string
	ActivationVersion uint64
}

func (PlatformCapability) TableName() string { return "ontology.platform_capabilities" }

// PlatformRevision is a recorded immutable release, not a published/active one.
type PlatformRevision struct {
	Capability string `gorm:"primaryKey"`
	Revision   uint64 `gorm:"primaryKey"`
	Payload    string
	Digest     string
	CreatedAt  time.Time
}

func (PlatformRevision) TableName() string { return "ontology.platform_revisions" }

type PlatformRevisionEvent struct {
	ID                   int64 `gorm:"primaryKey"`
	Capability           string
	Revision             uint64
	Action               string
	Digest               string
	ActorPrincipalID     int64
	ActorPrincipalType   string
	AuthorizationVersion int64
	CreatedAt            time.Time
}

func (PlatformRevisionEvent) TableName() string { return "ontology.platform_revision_events" }
