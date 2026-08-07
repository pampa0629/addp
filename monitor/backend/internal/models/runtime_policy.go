package models

import "time"

// RuntimePolicy is Monitor's platform-scoped alert and delivery policy.
type RuntimePolicy struct {
	ID                         uint      `gorm:"primaryKey;check:id = 1" json:"-"`
	Version                    uint64    `gorm:"not null" json:"version"`
	AlertEvaluationIntervalSec int64     `gorm:"not null" json:"alert_evaluation_interval_seconds"`
	WebhookDispatchIntervalSec int64     `gorm:"not null" json:"webhook_dispatch_interval_seconds"`
	WebhookHTTPTimeoutSec      int64     `gorm:"not null" json:"webhook_http_timeout_seconds"`
	WebhookLeaseDurationSec    int64     `gorm:"not null" json:"webhook_lease_duration_seconds"`
	WebhookMaxAttempts         int       `gorm:"not null" json:"webhook_max_attempts"`
	WebhookRetryInitialSec     int64     `gorm:"not null" json:"webhook_retry_initial_backoff_seconds"`
	WebhookRetryMaxSec         int64     `gorm:"not null" json:"webhook_retry_max_backoff_seconds"`
	EmailDispatchIntervalSec   int64     `gorm:"not null" json:"email_dispatch_interval_seconds"`
	EmailSMTPTimeoutSec        int64     `gorm:"not null" json:"email_smtp_timeout_seconds"`
	EmailLeaseDurationSec      int64     `gorm:"not null" json:"email_lease_duration_seconds"`
	EmailMaxAttempts           int       `gorm:"not null" json:"email_max_attempts"`
	EmailRetryInitialSec       int64     `gorm:"not null" json:"email_retry_initial_backoff_seconds"`
	EmailRetryMaxSec           int64     `gorm:"not null" json:"email_retry_max_backoff_seconds"`
	UpdatedBy                  uint      `gorm:"not null" json:"updated_by"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

func (RuntimePolicy) TableName() string { return "monitor.runtime_policy" }
