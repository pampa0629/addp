package models

import "time"

// RuntimePolicy stores Service's platform-scoped scheduler policy.
type RuntimePolicy struct {
	ID                  uint      `gorm:"primaryKey;check:id = 1" json:"-"`
	Version             uint64    `gorm:"not null" json:"version"`
	HealthCheckCron     string    `gorm:"not null;size:120" json:"health_check_cron"`
	MetadataRefreshCron string    `gorm:"not null;size:120" json:"metadata_refresh_cron"`
	UpdatedBy           uint      `gorm:"not null" json:"updated_by"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (RuntimePolicy) TableName() string { return "service.runtime_policy" }
