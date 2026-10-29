package models

import "time"

// CleanupOperation is a private, exact external-operation fence. It survives
// task deletion and lease loss; an issued/unknown operation is never resent.
type CleanupOperation struct {
	TargetKey          string `gorm:"primaryKey;size:64"`
	OperationID        string `gorm:"uniqueIndex;not null;size:36"`
	TenantID           uint   `gorm:"not null"`
	TaskID             uint   `gorm:"not null;index"`
	Kind               string `gorm:"not null;size:48"`
	Target             string `gorm:"not null;type:text"`
	CleanupExecutionID string `gorm:"size:36"`
	CaptureResourceID  uint
	ResourceVersion    uint64
	Attempt            int
	Status             string `gorm:"not null;size:16"`
	CreatedAt          time.Time
	CompletedAt        *time.Time
}

func (CleanupOperation) TableName() string { return "transfer.cleanup_operations" }
