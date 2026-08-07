package models

import "time"

const (
	MapScopePlatform = "platform"
	MapScopeTenant   = "tenant"
	MapProviderOSM   = "osm"
	MapProviderAMap  = "amap"
	MapProviderTDT   = "tianditu"
)

// BaseMapProvider is a typed online basemap resource owned by Manager.
// Provider keys are client-visible by nature and are therefore not treated as server secrets.
type BaseMapProvider struct {
	ID                 uint      `gorm:"primaryKey" json:"-"`
	Version            uint64    `gorm:"not null" json:"version"`
	ScopeType          string    `gorm:"not null;size:20" json:"scope_type"`
	TenantID           *uint     `json:"tenant_id,omitempty"`
	Provider           string    `gorm:"not null;size:30" json:"provider"`
	Enabled            bool      `gorm:"not null" json:"enabled"`
	SortOrder          int       `gorm:"not null" json:"sort_order"`
	AMapKey            string    `gorm:"type:text" json:"-"`
	AMapSecurityJsCode string    `gorm:"type:text" json:"-"`
	TDTKey             string    `gorm:"type:text" json:"-"`
	UpdatedBy          uint      `gorm:"not null" json:"updated_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (BaseMapProvider) TableName() string { return "manager.base_map_providers" }
