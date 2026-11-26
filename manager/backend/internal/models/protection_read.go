package models

import "time"

// ProtectionRead survives the loss of a Manager replica until the reader's end is proved.
type ProtectionRead struct {
	ReadID            string    `gorm:"primaryKey;size:36"`
	TenantID          int64     `gorm:"not null;index:idx_protection_reads_tenant"`
	ProcessInstanceID string    `gorm:"size:100;not null"`
	StartedAt         time.Time `gorm:"not null"`
}

func (ProtectionRead) TableName() string { return "manager.protection_reads" }
