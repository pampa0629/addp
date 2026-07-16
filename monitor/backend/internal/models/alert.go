package models

import (
	"time"

	commonModels "github.com/addp/common/models"
)

const (
	AlertStatusOpen         = "open"
	AlertStatusAcknowledged = "acknowledged"
	AlertStatusResolved     = "resolved"

	AlertSeverityWarning  = "warning"
	AlertSeverityCritical = "critical"
)

type AlertIncident struct {
	ID              uint                 `gorm:"primaryKey" json:"id"`
	TenantID        int                  `gorm:"not null;index:idx_monitor_alert_tenant_status,priority:1" json:"tenant_id"`
	Module          string               `gorm:"size:50;not null" json:"module"`
	TaskType        string               `gorm:"size:100;not null" json:"task_type"`
	SourceTaskID    string               `gorm:"size:255;not null" json:"source_task_id"`
	ExecutionID     string               `gorm:"size:255;not null;index" json:"execution_id"`
	AlertRuleID     *uint                `gorm:"index" json:"alert_rule_id,omitempty"`
	RuleID          string               `gorm:"size:100;not null;default:'';index" json:"rule_id"`
	RuleName        string               `gorm:"size:100;not null;default:''" json:"rule_name"`
	SignalCode      string               `gorm:"size:100;not null" json:"signal_code"`
	Fingerprint     string               `gorm:"size:64;not null;index" json:"fingerprint"`
	Severity        string               `gorm:"size:20;not null;index" json:"severity"`
	Status          string               `gorm:"size:20;not null;index:idx_monitor_alert_tenant_status,priority:2" json:"status"`
	Details         commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"details"`
	OpenedAt        time.Time            `gorm:"not null;index" json:"opened_at"`
	LastObservedAt  time.Time            `gorm:"not null" json:"last_observed_at"`
	AcknowledgedAt  *time.Time           `json:"acknowledged_at,omitempty"`
	AcknowledgedBy  string               `gorm:"size:255" json:"acknowledged_by,omitempty"`
	SuppressedUntil *time.Time           `json:"suppressed_until,omitempty"`
	ResolvedAt      *time.Time           `json:"resolved_at,omitempty"`
	CreatedAt       time.Time            `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time            `gorm:"autoUpdateTime" json:"updated_at"`
}

func (AlertIncident) TableName() string { return "monitor.alert_incidents" }
