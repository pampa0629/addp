package models

import "time"

// QuickViewPolicy controls Manager's bounded quick-view runtime budgets.
type QuickViewPolicy struct {
	ID                               uint      `gorm:"primaryKey;check:id = 1" json:"-"`
	Version                          uint64    `gorm:"not null" json:"version"`
	DirectFlatGeobufMaxRows          int       `gorm:"not null" json:"direct_flatgeobuf_max_rows"`
	RealtimeTileTimeoutMS            int       `gorm:"not null" json:"realtime_tile_timeout_ms"`
	RealtimeTileRetryAfterSec        int       `gorm:"not null" json:"realtime_tile_retry_after_sec"`
	RasterMosaicGenerationTimeoutSec int64     `gorm:"not null;default:7200" json:"raster_mosaic_generation_timeout_seconds"`
	UpdatedBy                        uint      `gorm:"not null" json:"updated_by"`
	CreatedAt                        time.Time `json:"created_at"`
	UpdatedAt                        time.Time `json:"updated_at"`
}

func (QuickViewPolicy) TableName() string { return "manager.quick_view_policy" }
