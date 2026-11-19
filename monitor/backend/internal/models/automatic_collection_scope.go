package models

import (
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
)

const AutomaticCollectionLockNamespace = 73043
const AutomaticCollectionLockID = 1

// AutomaticCollectionScope records Monitor's intent only. Identity and
// deployment/lease facts remain with System.
type AutomaticCollectionScope struct {
	metricsdiscovery.AutomaticScope `gorm:"embedded"`
	Enabled                         bool      `gorm:"not null" json:"enabled"`
	Version                         int64     `gorm:"not null" json:"version"`
	ChangedAt                       time.Time `gorm:"not null" json:"changed_at"`
}

func (AutomaticCollectionScope) TableName() string { return "monitor.automatic_collection_scopes" }
