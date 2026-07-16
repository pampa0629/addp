package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	commonModels "github.com/addp/common/models"
)

const (
	AlertEventOpened    = "opened"
	AlertEventEscalated = "escalated"
	AlertEventResolved  = "resolved"

	WebhookDeliveryPending    = "pending"
	WebhookDeliveryDelivering = "delivering"
	WebhookDeliveryDelivered  = "delivered"
	WebhookDeliveryDead       = "dead"
	WebhookDeliverySuppressed = "suppressed"
	WebhookDeliveryCancelled  = "cancelled"
)

type StringList []string

func (values StringList) Value() (driver.Value, error) {
	return json.Marshal(values)
}

func (values *StringList) Scan(value interface{}) error {
	if value == nil {
		*values = nil
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("unsupported StringList value %T", value)
	}
	return json.Unmarshal(data, values)
}

type AlertEvent struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	EventID      string    `gorm:"size:36;not null;uniqueIndex" json:"event_id"`
	TenantID     int       `gorm:"not null;index:idx_monitor_alert_event_tenant_time,priority:1" json:"tenant_id"`
	IncidentID   uint      `gorm:"not null;index" json:"incident_id"`
	EventType    string    `gorm:"size:20;not null;index" json:"event_type"`
	FromSeverity string    `gorm:"size:20" json:"from_severity,omitempty"`
	ToSeverity   string    `gorm:"size:20;not null" json:"to_severity"`
	OccurredAt   time.Time `gorm:"not null;index:idx_monitor_alert_event_tenant_time,priority:2,sort:desc" json:"occurred_at"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (AlertEvent) TableName() string { return "monitor.alert_events" }

type WebhookDestination struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	TenantID         int        `gorm:"not null;uniqueIndex:uq_monitor_webhook_destination_name,priority:1;index:idx_monitor_webhook_destination_tenant_enabled,priority:1" json:"tenant_id"`
	Name             string     `gorm:"size:100;not null;uniqueIndex:uq_monitor_webhook_destination_name,priority:2" json:"name"`
	URL              string     `gorm:"size:2048;not null" json:"url"`
	SecretCiphertext string     `gorm:"type:text;not null" json:"-"`
	SecretConfigured bool       `gorm:"-" json:"secret_configured"`
	Enabled          bool       `gorm:"not null;index:idx_monitor_webhook_destination_tenant_enabled,priority:2" json:"enabled"`
	EventTypes       StringList `gorm:"type:jsonb;not null" json:"event_types"`
	CreatedAt        time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (WebhookDestination) TableName() string { return "monitor.webhook_destinations" }

type WebhookDelivery struct {
	ID                    uint                 `gorm:"primaryKey" json:"id"`
	DeliveryID            string               `gorm:"size:36;not null;uniqueIndex" json:"delivery_id"`
	TenantID              int                  `gorm:"not null;index:idx_monitor_webhook_delivery_tenant_time,priority:1" json:"tenant_id"`
	DestinationID         uint                 `gorm:"not null;uniqueIndex:uq_monitor_webhook_event_destination,priority:2;index" json:"destination_id"`
	DestinationName       string               `gorm:"size:100;not null" json:"destination_name"`
	AlertEventID          uint                 `gorm:"not null;uniqueIndex:uq_monitor_webhook_event_destination,priority:1;index" json:"alert_event_id"`
	IncidentID            uint                 `gorm:"not null;index" json:"incident_id"`
	EventType             string               `gorm:"size:20;not null;index" json:"event_type"`
	RequestURL            string               `gorm:"size:2048;not null" json:"request_url"`
	SecretCiphertext      string               `gorm:"type:text" json:"-"`
	Payload               commonModels.JSONMap `gorm:"type:jsonb;not null" json:"payload"`
	Status                string               `gorm:"size:20;not null;index" json:"status"`
	AttemptCount          int                  `gorm:"not null;default:0" json:"attempt_count"`
	RetryBaseAttemptCount int                  `gorm:"not null;default:0" json:"retry_base_attempt_count"`
	ManualRetryCount      int                  `gorm:"not null;default:0" json:"manual_retry_count"`
	NextAttemptAt         *time.Time           `json:"next_attempt_at,omitempty"`
	ClaimedBy             string               `gorm:"size:255" json:"claimed_by,omitempty"`
	LeaseExpiresAt        *time.Time           `json:"lease_expires_at,omitempty"`
	LastHTTPStatus        *int                 `json:"last_http_status,omitempty"`
	LastError             string               `gorm:"type:text" json:"last_error,omitempty"`
	DeliveredAt           *time.Time           `json:"delivered_at,omitempty"`
	CreatedAt             time.Time            `gorm:"autoCreateTime;index:idx_monitor_webhook_delivery_tenant_time,priority:2,sort:desc" json:"created_at"`
	UpdatedAt             time.Time            `gorm:"autoUpdateTime" json:"updated_at"`
}

func (WebhookDelivery) TableName() string { return "monitor.webhook_deliveries" }
