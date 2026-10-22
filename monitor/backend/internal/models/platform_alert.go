package models

import "time"

const (
	PlatformAlertCategoryLog        = "log_pipeline"
	PlatformAlertCategoryResource   = "resource"
	PlatformAlertCategoryHTTP       = "http_quality"
	PlatformAlertCategoryProcess    = "process_resource"
	PlatformAlertSubjectInstance    = "module_instance"
	PlatformAlertSubjectModule      = "module"
	PlatformAlertSubjectLogObserver = "log_observer"
	PlatformAlertSubjectHostNode    = "host_node"
)

type PlatformAlertIncident struct {
	ProcessEvidence  *ProcessAlertEvidence  `gorm:"serializer:json;type:jsonb" json:"process_evidence,omitempty"`
	HTTPEvidence     *HTTPAlertEvidence     `gorm:"serializer:json;type:jsonb" json:"http_evidence,omitempty"`
	ResourceEvidence *ResourceAlertEvidence `gorm:"serializer:json;type:jsonb" json:"resource_evidence,omitempty"`
	ID               uint                   `gorm:"primaryKey" json:"id"`
	Version          uint64                 `gorm:"not null;default:1" json:"version"`
	Category         string                 `gorm:"not null;size:32" json:"category"`
	SubjectKind      string                 `gorm:"not null;size:32" json:"subject_kind"`
	SubjectID        string                 `gorm:"not null;size:100;index" json:"subject_id"`
	Signal           string                 `gorm:"not null;size:160" json:"signal"`
	InstanceID       string                 `gorm:"size:100" json:"instance_id,omitempty"`
	Severity         string                 `gorm:"not null;size:20" json:"severity"`
	Status           string                 `gorm:"not null;size:20;index" json:"status"`
	OpenedAt         time.Time              `json:"opened_at"`
	LastObservedAt   time.Time              `json:"last_observed_at"`
	ResolvedAt       *time.Time             `json:"resolved_at,omitempty"`
	AcknowledgedBy   *int64                 `json:"acknowledged_by,omitempty"`
	AcknowledgedAt   *time.Time             `json:"acknowledged_at,omitempty"`
	SuppressedUntil  *time.Time             `json:"suppressed_until,omitempty"`
}

func (PlatformAlertIncident) TableName() string { return "monitor.platform_alert_incidents" }

type PlatformAlertEvent struct {
	ProcessEvidence  *ProcessAlertEvidence  `gorm:"serializer:json;type:jsonb" json:"process_evidence,omitempty"`
	HTTPEvidence     *HTTPAlertEvidence     `gorm:"serializer:json;type:jsonb" json:"http_evidence,omitempty"`
	ResourceEvidence *ResourceAlertEvidence `gorm:"serializer:json;type:jsonb" json:"resource_evidence,omitempty"`
	ID               string                 `gorm:"primaryKey;size:36" json:"id"`
	IncidentID       uint                   `gorm:"not null;index" json:"incident_id"`
	Type             string                 `gorm:"not null;size:20" json:"type"`
	Severity         string                 `gorm:"not null;size:20" json:"severity"`
	OccurredAt       time.Time              `json:"occurred_at"`
}

func (PlatformAlertEvent) TableName() string { return "monitor.platform_alert_events" }

type PlatformAlertDestination struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	Version          uint64     `gorm:"not null;default:1" json:"version"`
	Name             string     `gorm:"not null;size:100;uniqueIndex" json:"name"`
	Channel          string     `gorm:"not null;size:20" json:"channel"`
	URL              string     `gorm:"size:2048" json:"url,omitempty"`
	Recipients       StringList `gorm:"type:jsonb;not null" json:"recipients"`
	Categories       StringList `gorm:"type:jsonb;not null" json:"categories"`
	EventTypes       StringList `gorm:"type:jsonb;not null" json:"event_types" enums:"opened,resolved"`
	SecretCiphertext string     `gorm:"type:text" json:"-"`
	SecretConfigured bool       `gorm:"-" json:"secret_configured"`
	Enabled          bool       `gorm:"not null" json:"enabled"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (PlatformAlertDestination) TableName() string { return "monitor.platform_alert_destinations" }

type PlatformAlertDelivery struct {
	ID                    string     `gorm:"primaryKey;size:36" json:"id"`
	EventID               string     `gorm:"not null;size:36;uniqueIndex:uq_platform_alert_event_destination,priority:1" json:"event_id"`
	DestinationID         uint       `gorm:"not null;uniqueIndex:uq_platform_alert_event_destination,priority:2" json:"destination_id"`
	Channel               string     `gorm:"not null;size:20" json:"channel"`
	URL                   string     `gorm:"size:2048" json:"-"`
	SecretCiphertext      string     `gorm:"type:text" json:"-"`
	Recipients            StringList `gorm:"type:jsonb;not null" json:"-"`
	Payload               string     `gorm:"type:text;not null" json:"-"`
	Status                string     `gorm:"not null;size:20;index" json:"status"`
	AttemptCount          int        `gorm:"not null" json:"attempt_count"`
	RetryBaseAttemptCount int        `gorm:"not null;default:0" json:"-"`
	ManualRetryCount      int        `gorm:"not null;default:0" json:"manual_retry_count"`
	NextAttemptAt         *time.Time `json:"next_attempt_at,omitempty"`
	ClaimID               string     `gorm:"size:36" json:"-"`
	LeaseExpiresAt        *time.Time `json:"-"`
	LastError             string     `gorm:"size:100" json:"last_error,omitempty"`
	DeliveredAt           *time.Time `json:"delivered_at,omitempty"`
	CreatedAt             time.Time  `gorm:"index" json:"created_at"`
}

func (PlatformAlertDelivery) TableName() string { return "monitor.platform_alert_deliveries" }
