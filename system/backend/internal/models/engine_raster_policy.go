package models

// EngineRasterPolicy is a System-owned process budget, separate from connection metadata.
type EngineRasterPolicy struct {
	EngineID             uint  `gorm:"primaryKey" json:"engine_id"`
	Version              int64 `gorm:"not null" json:"version"`
	Running              int   `gorm:"not null" json:"running"`
	Waiting              int   `gorm:"not null" json:"waiting"`
	CacheMiB             int   `gorm:"column:cache_mib;not null" json:"cache_mib"`
	DefaultTenantRunning int   `gorm:"not null" json:"default_tenant_running"`
	DefaultTenantWaiting int   `gorm:"not null" json:"default_tenant_waiting"`
}

func (EngineRasterPolicy) TableName() string { return "engine_raster_policies" }

type EngineRasterQuota struct {
	EngineID uint  `gorm:"primaryKey" json:"engine_id"`
	TenantID uint  `gorm:"primaryKey" json:"tenant_id"`
	Version  int64 `gorm:"not null" json:"version"`
	Running  *int  `json:"running"`
	Waiting  *int  `json:"waiting"`
}

func (EngineRasterQuota) TableName() string { return "engine_raster_quotas" }

func DefaultEngineRasterPolicy(id uint) EngineRasterPolicy {
	return EngineRasterPolicy{EngineID: id, Version: 1, Running: 2, Waiting: 2, CacheMiB: 256, DefaultTenantRunning: 2, DefaultTenantWaiting: 2}
}

type RasterPolicyView struct {
	Policy           EngineRasterPolicy     `json:"policy"`
	Quota            *EngineRasterQuota     `json:"quota,omitempty"`
	EffectiveRunning int                    `json:"effective_running"`
	EffectiveWaiting int                    `json:"effective_waiting"`
	Runtime          map[string]interface{} `json:"runtime,omitempty"`
}
