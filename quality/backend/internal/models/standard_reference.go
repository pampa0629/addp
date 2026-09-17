package models

import "time"

const (
	StandardReferenceGuardOpen    = "open"
	StandardReferenceGuardFrozen  = "frozen"
	StandardReferenceGuardDeleted = "deleted"
)

type StandardReferenceGuard struct {
	ID           int64  `gorm:"primaryKey"`
	TenantID     int64  `gorm:"not null"`
	ResourceType string `gorm:"size:32;not null"`
	ResourceID   int64  `gorm:"not null"`
	State        string `gorm:"size:16;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (StandardReferenceGuard) TableName() string { return "quality.standard_reference_guards" }
