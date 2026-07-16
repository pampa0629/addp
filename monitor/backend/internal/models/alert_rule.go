package models

import "time"

const (
	AlertRuleLastTerminalFailed  = "last_terminal_failed"
	AlertRuleLastTerminalTimeout = "last_terminal_timeout"
	AlertRuleConsecutiveFailures = "consecutive_failures"

	NotificationChannelWebhook = "webhook"
	NotificationChannelEmail   = "email"
)

type AlertRule struct {
	ID               uint                `gorm:"primaryKey" json:"id"`
	RuleID           string              `gorm:"size:36;not null;uniqueIndex" json:"rule_id"`
	TenantID         int                 `gorm:"not null;uniqueIndex:uq_monitor_alert_rule_name,priority:1;index:idx_monitor_alert_rule_tenant_enabled,priority:1" json:"tenant_id"`
	Name             string              `gorm:"size:100;not null;uniqueIndex:uq_monitor_alert_rule_name,priority:2" json:"name"`
	Module           string              `gorm:"size:50;not null;index:idx_monitor_alert_rule_target,priority:1" json:"module"`
	TaskType         string              `gorm:"size:100;not null;index:idx_monitor_alert_rule_target,priority:2" json:"task_type"`
	SourceTaskID     string              `gorm:"size:255;not null;index:idx_monitor_alert_rule_target,priority:3" json:"source_task_id"`
	SourceTaskName   string              `gorm:"size:255" json:"source_task_name,omitempty"`
	RuleType         string              `gorm:"size:50;not null;index" json:"rule_type"`
	FailureThreshold int                 `gorm:"not null;default:1" json:"failure_threshold"`
	Severity         string              `gorm:"size:20;not null" json:"severity"`
	Enabled          bool                `gorm:"not null;index:idx_monitor_alert_rule_tenant_enabled,priority:2" json:"enabled"`
	Routes           []NotificationRoute `gorm:"foreignKey:AlertRuleID" json:"routes"`
	CreatedAt        time.Time           `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time           `gorm:"autoUpdateTime" json:"updated_at"`
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
