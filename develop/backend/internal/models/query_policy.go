package models

import "time"

// QueryPolicy is Develop's typed query execution policy.
// Platform owns the hard limits; a tenant row may override only the default timeout.
type QueryPolicy struct {
	ID                  uint      `gorm:"primaryKey" json:"-"`
	ScopeType           string    `gorm:"size:32;not null;uniqueIndex:idx_develop_query_policy_scope,priority:1" json:"scope_type"`
	TenantID            *uint     `gorm:"uniqueIndex:idx_develop_query_policy_scope,priority:2" json:"tenant_id,omitempty"`
	DefaultQueryTimeout int       `gorm:"not null" json:"default_query_timeout"`
	MaxQueryTimeout     int       `gorm:"not null" json:"max_query_timeout"`
	QueryResultLimit    int       `gorm:"not null" json:"query_result_limit"`
	Version             uint64    `gorm:"not null" json:"version"`
	UpdatedBy           uint      `gorm:"not null" json:"updated_by"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (QueryPolicy) TableName() string { return "develop.query_policy" }
