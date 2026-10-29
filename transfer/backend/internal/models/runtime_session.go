package models

import "time"

// RuntimeSession records one admitted runtime invocation. StoppedAt is written
// only by its supervisor after Run and all synchronous in-flight work return.
// Lease expiry and recovery never synthesize this evidence.
type RuntimeSession struct {
	ExecutionID     string    `gorm:"primaryKey;size:36"`
	OwnerInstanceID string    `gorm:"primaryKey;size:255"`
	FencingToken    uint64    `gorm:"primaryKey"`
	TaskID          uint      `gorm:"not null;index:idx_runtime_session_task"`
	TenantID        uint      `gorm:"not null"`
	StartedAt       time.Time `gorm:"not null"`
	StoppedAt       *time.Time
}

func (RuntimeSession) TableName() string { return "transfer.runtime_sessions" }
