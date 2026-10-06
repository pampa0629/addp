package models

import (
	"github.com/addp/monitor/internal/resourcequery"
	"time"
)

type ResourceQueryPolicy struct {
	ID                   uint   `gorm:"primaryKey" json:"-"`
	Version              uint64 `gorm:"not null" json:"version"`
	resourcequery.Budget `gorm:"embedded"`
	UpdatedBy            uint      `json:"-"`
	UpdatedAt            time.Time `json:"-"`
}

func (ResourceQueryPolicy) TableName() string { return "monitor.resource_query_policy" }
