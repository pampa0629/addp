package models

import "time"

const (
	EmailDeliveryPending    = "pending"
	EmailDeliveryDelivering = "delivering"
	EmailDeliveryDelivered  = "delivered"
	EmailDeliveryDead       = "dead"
	EmailDeliverySuppressed = "suppressed"
	EmailDeliveryCancelled  = "cancelled"
)

type EmailDestination struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	TenantID   int        `gorm:"not null;uniqueIndex:uq_monitor_email_destination_name,priority:1;index:idx_monitor_email_destination_tenant_enabled,priority:1" json:"tenant_id"`
	Name       string     `gorm:"size:100;not null;uniqueIndex:uq_monitor_email_destination_name,priority:2" json:"name"`
	Recipients StringList `gorm:"type:jsonb;not null" json:"recipients"`
	Enabled    bool       `gorm:"not null;index:idx_monitor_email_destination_tenant_enabled,priority:2" json:"enabled"`
	EventTypes StringList `gorm:"type:jsonb;not null" json:"event_types"`
	CreatedAt  time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (EmailDestination) TableName() string { return "monitor.email_destinations" }

type EmailDelivery struct {
	ID                    uint       `gorm:"primaryKey" json:"id"`
	DeliveryID            string     `gorm:"size:36;not null;uniqueIndex" json:"delivery_id"`
	TenantID              int        `gorm:"not null;index:idx_monitor_email_delivery_tenant_time,priority:1" json:"tenant_id"`
	DestinationID         uint       `gorm:"not null;uniqueIndex:uq_monitor_email_event_destination,priority:2;index" json:"destination_id"`
	DestinationName       string     `gorm:"size:100;not null" json:"destination_name"`
	AlertEventID          uint       `gorm:"not null;uniqueIndex:uq_monitor_email_event_destination,priority:1;index" json:"alert_event_id"`
	IncidentID            uint       `gorm:"not null;index" json:"incident_id"`
	EventType             string     `gorm:"size:20;not null;index" json:"event_type"`
	Recipients            StringList `gorm:"type:jsonb;not null" json:"recipients"`
	Subject               string     `gorm:"size:500;not null" json:"subject"`
	TextBody              string     `gorm:"type:text;not null" json:"text_body"`
	HTMLBody              string     `gorm:"type:text;not null" json:"html_body"`
	Status                string     `gorm:"size:20;not null;index" json:"status"`
	AttemptCount          int        `gorm:"not null;default:0" json:"attempt_count"`
	RetryBaseAttemptCount int        `gorm:"not null;default:0" json:"retry_base_attempt_count"`
	ManualRetryCount      int        `gorm:"not null;default:0" json:"manual_retry_count"`
	NextAttemptAt         *time.Time `json:"next_attempt_at,omitempty"`
	ClaimedBy             string     `gorm:"size:255" json:"claimed_by,omitempty"`
	LeaseExpiresAt        *time.Time `json:"lease_expires_at,omitempty"`
	LastError             string     `gorm:"type:text" json:"last_error,omitempty"`
	DeliveredAt           *time.Time `json:"delivered_at,omitempty"`
	CreatedAt             time.Time  `gorm:"autoCreateTime;index:idx_monitor_email_delivery_tenant_time,priority:2,sort:desc" json:"created_at"`
	UpdatedAt             time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (EmailDelivery) TableName() string { return "monitor.email_deliveries" }
