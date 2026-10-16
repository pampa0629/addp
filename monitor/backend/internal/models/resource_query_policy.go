package models

import (
	"github.com/addp/monitor/internal/resourcequery"
	"time"
)

// One key serializes policy CAS and resource-rule evidence commit, including
// the absent default row. Both writers must hold this transaction lock.
const (
	ResourceQueryPolicyLockNamespace = 1836019811
	ResourceQueryPolicyLockID        = 3
)

type ResourceQueryPolicy struct {
	ID                   uint   `gorm:"primaryKey" json:"-"`
	Version              uint64 `gorm:"not null" json:"version"`
	resourcequery.Budget `gorm:"embedded"`
	UpdatedBy            uint      `json:"-"`
	UpdatedAt            time.Time `json:"-"`
}

func (ResourceQueryPolicy) TableName() string { return "monitor.resource_query_policy" }
