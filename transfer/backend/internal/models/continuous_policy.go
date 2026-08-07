package models

import "time"

// ContinuousPolicy is Transfer's platform-scoped continuous runtime policy.
type ContinuousPolicy struct {
	ID                              uint      `gorm:"primaryKey;check:id = 1" json:"-"`
	Version                         uint64    `gorm:"not null" json:"version"`
	DiagnosticsIntervalSeconds      int64     `gorm:"not null" json:"diagnostics_interval_seconds"`
	RetentionDegradedHorizonSeconds int64     `gorm:"not null" json:"retention_degraded_horizon_seconds"`
	RetentionCriticalHorizonSeconds int64     `gorm:"not null" json:"retention_critical_horizon_seconds"`
	CheckpointStaleAfterSeconds     int64     `gorm:"not null" json:"checkpoint_stale_after_seconds"`
	RecoveryInitialBackoffSeconds   int64     `gorm:"not null" json:"recovery_initial_backoff_seconds"`
	RecoveryMaxBackoffSeconds       int64     `gorm:"not null" json:"recovery_max_backoff_seconds"`
	RecoveryMaxFailures             int       `gorm:"not null" json:"recovery_max_consecutive_failures"`
	RecoveryCircuitOpenSeconds      int64     `gorm:"not null" json:"recovery_circuit_open_seconds"`
	RecoveryStabilityWindowSeconds  int64     `gorm:"not null" json:"recovery_stability_window_seconds"`
	UpdatedBy                       uint      `gorm:"not null" json:"updated_by"`
	CreatedAt                       time.Time `json:"created_at"`
	UpdatedAt                       time.Time `json:"updated_at"`
}

func (ContinuousPolicy) TableName() string { return "transfer.continuous_policy" }
