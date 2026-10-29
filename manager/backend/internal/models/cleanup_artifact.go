package models

import "time"

// CleanupTarget is shared by Manager artifact and PG-only projection writers.
// Generation advances at Cleanup, not on ordinary reads or process restart.
type CleanupTarget struct {
	Key         string `gorm:"primaryKey;size:64"`
	TenantID    uint   `gorm:"not null;index"`
	Kind        string `gorm:"not null;size:64"`
	Identity    string `gorm:"not null;type:text"`
	Generation  uint64 `gorm:"not null;default:0"`
	OperationID string `gorm:"not null;size:36;default:''"`
	UpdatedAt   time.Time
}

func (CleanupTarget) TableName() string { return "manager.cleanup_targets" }

// CleanupArtifactOperation retains the original effect identity independently
// of a later business-result transaction. Unknown effects never become retries.
type CleanupArtifactOperation struct {
	ID            string `gorm:"primaryKey;size:36"`
	TenantID      uint   `gorm:"not null;index"`
	Origin        string `gorm:"not null;size:32"`
	ExecutionID   string `gorm:"not null;size:36;index"`
	Attempt       int    `gorm:"not null"`
	LeaseToken    string `gorm:"not null;size:64"`
	LeaseOwner    string `gorm:"not null;size:128"`
	Action        string `gorm:"not null;size:32"`
	Snapshot      string `gorm:"not null;type:text"`
	TargetKeys    string `gorm:"not null;type:text"`
	BindingDigest string `gorm:"not null;size:64"`
	State         string `gorm:"not null;size:32"`
	CreatedAt     time.Time
	EffectAt      *time.Time
	FinalizedAt   *time.Time
}

func (CleanupArtifactOperation) TableName() string { return "manager.cleanup_artifact_operations" }
