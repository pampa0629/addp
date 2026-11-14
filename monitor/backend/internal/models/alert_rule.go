package models

import "time"

const (
	AlertRuleCDCStalled = "cdc_consumer_stalled"
	AlertRuleCDCDelayed = "cdc_capture_delayed"
	AlertRuleCDCBacklog = "cdc_backlog_high"

	AlertRuleLastTerminalFailed  = "last_terminal_failed"
	AlertRuleLastTerminalTimeout = "last_terminal_timeout"
	AlertRuleConsecutiveFailures = "consecutive_failures"

	NotificationChannelWebhook = "webhook"
	NotificationChannelEmail   = "email"
)

type AlertRule struct {
	ID               uint                 `gorm:"primaryKey" json:"id"`
	RuleID           string               `gorm:"size:36;not null;uniqueIndex" json:"rule_id"`
	TenantID         int                  `gorm:"not null;uniqueIndex:uq_monitor_alert_rule_name,priority:1;index:idx_monitor_alert_rule_tenant_enabled,priority:1" json:"tenant_id"`
	Name             string               `gorm:"size:100;not null;uniqueIndex:uq_monitor_alert_rule_name,priority:2" json:"name"`
	Module           string               `gorm:"size:50;not null;index:idx_monitor_alert_rule_target,priority:1" json:"module"`
	TaskType         string               `gorm:"size:100;not null;index:idx_monitor_alert_rule_target,priority:2" json:"task_type"`
	SourceTaskID     string               `gorm:"size:255;not null;index:idx_monitor_alert_rule_target,priority:3" json:"source_task_id"`
	SourceTaskName   string               `gorm:"size:255" json:"source_task_name,omitempty"`
	BacklogThreshold string               `gorm:"size:20;not null;default:''" json:"backlog_threshold"`
	CDCWindow        CDCPerformanceWindow `gorm:"serializer:json;type:jsonb;not null;default:'{}'" json:"cdc_evaluation"`
	RuleType         string               `gorm:"size:50;not null;index" json:"rule_type"`
	FailureThreshold int                  `gorm:"not null;default:1" json:"failure_threshold"`
	Severity         string               `gorm:"size:20;not null" json:"severity"`
	Enabled          bool                 `gorm:"not null;index:idx_monitor_alert_rule_tenant_enabled,priority:2" json:"enabled"`
	Routes           []NotificationRoute  `gorm:"foreignKey:AlertRuleID" json:"routes"`
	CreatedAt        time.Time            `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time            `gorm:"autoUpdateTime" json:"updated_at"`
}

func (AlertRule) TableName() string { return "monitor.alert_rules" }

type NotificationRoute struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	TenantID      int       `gorm:"not null;index" json:"tenant_id"`
	AlertRuleID   uint      `gorm:"not null;uniqueIndex:uq_monitor_notification_route,priority:1;index" json:"alert_rule_id"`
	Channel       string    `gorm:"size:20;not null;uniqueIndex:uq_monitor_notification_route,priority:2" json:"channel"`
	DestinationID uint      `gorm:"not null;uniqueIndex:uq_monitor_notification_route,priority:3" json:"destination_id"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (NotificationRoute) TableName() string { return "monitor.notification_routes" }

// CDCPerformanceWindow contains safe sampled evidence, never transport identities.
type CDCPerformanceWindow struct {
	ResetAt       *time.Time `json:"reset_at,omitempty"`
	Generation    uint64     `json:"generation,string"`
	SampledAt     *time.Time `json:"sampled_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	TriggerSince  *time.Time `json:"trigger_since,omitempty"`
	RecoverySince *time.Time `json:"recovery_since,omitempty"`
	PreviousValue string     `json:"previous_value,omitempty"`
	State         string     `json:"state"`
	Active        bool       `json:"active"`
}

func IsCDCPerformanceRule(code string) bool {
	return code == AlertRuleCDCStalled || code == AlertRuleCDCDelayed || code == AlertRuleCDCBacklog
}
