package models

import "time"

// MonitoringTarget stores collection intent, without copying System node facts.
type MonitoringTarget struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	NodeID      string `gorm:"type:uuid;not null;index"`
	MonitorKind string `gorm:"type:varchar(32);not null"`
	SourceType  string `gorm:"type:varchar(32);not null"`
	Endpoint    string `gorm:"type:varchar(512);not null"`
	Enabled     bool   `gorm:"not null"`
	Version     int64  `gorm:"not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (MonitoringTarget) TableName() string { return "monitor.monitoring_targets" }
