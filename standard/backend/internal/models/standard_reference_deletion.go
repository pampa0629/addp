package models

import "time"

// StandardReferenceDeletion records a cross-module deletion that still needs
// to converge with the Model reference guard.
type StandardReferenceDeletion struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID      int64     `gorm:"not null;uniqueIndex:uq_standard_reference_deletions_resource" json:"tenant_id"`
	ResourceType  string    `gorm:"size:32;not null;uniqueIndex:uq_standard_reference_deletions_resource" json:"resource_type"`
	ResourceID    int64     `gorm:"not null;uniqueIndex:uq_standard_reference_deletions_resource" json:"resource_id"`
	Attempts      int       `gorm:"not null;default:0" json:"attempts"`
	NextAttemptAt time.Time `gorm:"not null;index" json:"next_attempt_at"`
	LastError     string    `gorm:"type:text" json:"last_error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (StandardReferenceDeletion) TableName() string {
	return "standard.reference_deletions"
}
