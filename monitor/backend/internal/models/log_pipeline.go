package models

import (
	"github.com/addp/common/logpipeline"
	"time"
)

type LogPipelinePolicy struct {
	ID              uint      `gorm:"primaryKey" json:"-"`
	Version         uint64    `gorm:"not null;default:1" json:"version"`
	FailureSamples  int       `gorm:"not null" json:"failure_samples"`
	RecoverySamples int       `gorm:"not null" json:"recovery_samples"`
	StaleSeconds    int       `gorm:"not null" json:"stale_seconds"`
	DelayMS         int64     `gorm:"not null" json:"delay_ms"`
	CapacityPercent int       `gorm:"not null" json:"capacity_percent"`
	RecoveryPercent int       `gorm:"not null" json:"recovery_percent"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (LogPipelinePolicy) TableName() string { return "monitor.log_pipeline_policy" }

type LogSignalState struct {
	Failures   int    `json:"failures"`
	Successes  int    `json:"successes"`
	Active     bool   `json:"active"`
	Severity   string `json:"severity"`
	InstanceID string `json:"instance_id,omitempty"`
}
type LogPipelineNode struct {
	Node          string                    `gorm:"primaryKey;size:100" json:"node"`
	ReceivedAt    *time.Time                `json:"received_at,omitempty"`
	StartedAt     time.Time                 `json:"started_at"`
	Observation   logpipeline.Observation   `gorm:"serializer:json;type:jsonb;not null" json:"observation"`
	Signals       map[string]LogSignalState `gorm:"serializer:json;type:jsonb;not null" json:"-"`
	RegistryValid bool                      `json:"registry_valid"`
}

func (LogPipelineNode) TableName() string { return "monitor.log_pipeline_nodes" }

type LogObserverBoot struct {
	BootID string `gorm:"primaryKey;size:100"`
	Node   string `gorm:"not null;size:100;index"`
}

func (LogObserverBoot) TableName() string { return "monitor.log_observer_boots" }

type PlatformLogIncident struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	Version         uint64     `gorm:"not null;default:1" json:"version"`
	Node            string     `gorm:"not null;size:100;index" json:"node"`
	Signal          string     `gorm:"not null;size:160" json:"signal"`
	InstanceID      string     `gorm:"size:100" json:"instance_id,omitempty"`
	Severity        string     `gorm:"not null;size:20" json:"severity"`
	Status          string     `gorm:"not null;size:20;index" json:"status"`
	OpenedAt        time.Time  `json:"opened_at"`
	LastObservedAt  time.Time  `json:"last_observed_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	AcknowledgedBy  *int64     `json:"acknowledged_by,omitempty"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	SuppressedUntil *time.Time `json:"suppressed_until,omitempty"`
}

func (PlatformLogIncident) TableName() string { return "monitor.platform_log_incidents" }

type PlatformLogEvent struct {
	ID         string    `gorm:"primaryKey;size:36" json:"id"`
	IncidentID uint      `gorm:"not null;index" json:"incident_id"`
	Type       string    `gorm:"not null;size:20" json:"type"`
	Severity   string    `gorm:"not null;size:20" json:"severity"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (PlatformLogEvent) TableName() string { return "monitor.platform_log_events" }

type PlatformLogDestination struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	Version          uint64     `gorm:"not null;default:1" json:"version"`
	Name             string     `gorm:"not null;size:100;uniqueIndex" json:"name"`
	Channel          string     `gorm:"not null;size:20" json:"channel"`
	URL              string     `gorm:"size:2048" json:"url,omitempty"`
	Recipients       StringList `gorm:"type:jsonb;not null" json:"recipients"`
	EventTypes       StringList `gorm:"type:jsonb;not null" json:"event_types"`
	SecretCiphertext string     `gorm:"type:text" json:"-"`
	SecretConfigured bool       `gorm:"-" json:"secret_configured"`
	Enabled          bool       `gorm:"not null" json:"enabled"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (PlatformLogDestination) TableName() string { return "monitor.platform_log_destinations" }

type PlatformLogDelivery struct {
	ID               string     `gorm:"primaryKey;size:36" json:"id"`
	EventID          string     `gorm:"not null;size:36;uniqueIndex:uq_platform_log_event_destination,priority:1" json:"event_id"`
	DestinationID    uint       `gorm:"not null;uniqueIndex:uq_platform_log_event_destination,priority:2" json:"destination_id"`
	Channel          string     `gorm:"not null;size:20" json:"channel"`
	URL              string     `gorm:"size:2048" json:"-"`
	SecretCiphertext string     `gorm:"type:text" json:"-"`
	Recipients       StringList `gorm:"type:jsonb;not null" json:"-"`
	Payload          string     `gorm:"type:text;not null" json:"-"`
	Status           string     `gorm:"not null;size:20;index" json:"status"`
	AttemptCount     int        `gorm:"not null" json:"attempt_count"`
	NextAttemptAt    *time.Time `json:"next_attempt_at,omitempty"`
	ClaimID          string     `gorm:"size:36" json:"-"`
	LeaseExpiresAt   *time.Time `json:"-"`
	LastError        string     `gorm:"size:100" json:"last_error,omitempty"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	CreatedAt        time.Time  `gorm:"index" json:"created_at"`
}

func (PlatformLogDelivery) TableName() string { return "monitor.platform_log_deliveries" }
