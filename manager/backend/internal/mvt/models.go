package mvt

import "time"

// TileCoord 瓦片坐标
type TileCoord struct {
	Z int `json:"z"`
	X int `json:"x"`
	Y int `json:"y"`
}

// ZoomLevelStats 每个缩放级别的统计信息
type ZoomLevelStats struct {
	Zoom           int     `json:"zoom"`
	TotalTiles     int     `json:"total_tiles"`      // 计算出的瓦片总数
	GeneratedTiles int     `json:"generated_tiles"`  // 实际生成的瓦片数（有数据）
	EmptyTiles     int     `json:"empty_tiles"`      // 空瓦片数
	AvgGenTimeMs   float64 `json:"avg_gen_time_ms"`  // 平均生成时间（毫秒）
	AvgSizeKB      float64 `json:"avg_size_kb"`      // 平均大小（KB）
	TotalSizeBytes int64   `json:"total_size_bytes"` // 总大小（字节）
}

// QuickViewMetadata 快显元数据（存储在 MinIO）
type QuickViewMetadata struct {
	EngineID         uint                      `json:"engine_id"`
	Fingerprint      string                    `json:"fingerprint"`
	TableName        string                    `json:"table_name"`
	Schema           string                    `json:"schema"`
	Extent           []float64                 `json:"extent"`            // [minLng, minLat, maxLng, maxLat]
	SRID             int                       `json:"srid"`
	RowCount         int64                     `json:"row_count"`
	GeometryTypes    []string                  `json:"geometry_types"`
	ZoomLevels       map[string]ZoomLevelStats `json:"zoom_levels"`       // "0", "1", ... "maxZoom"
	MaxZoomGenerated int                       `json:"max_zoom_generated"`
	StopReason       string                    `json:"stop_reason"` // "max_zoom_reached" or "adaptive_threshold"
	TotalTiles       int                       `json:"total_tiles"`
	TotalSizeBytes   int64                     `json:"total_size_bytes"`
	CreatedAt        time.Time                 `json:"created_at"`
	GenerationSec    float64                   `json:"generation_duration_seconds"`
}

// SpatialMetadata 空间表元数据
type SpatialMetadata struct {
	GeometryColumn  string    `json:"geometry_column"`
	SRID            int       `json:"srid"`
	Extent          []float64 `json:"extent"` // [minLng, minLat, maxLng, maxLat] WGS84
	GeometryTypes   []string  `json:"geometry_types"`
	HasSpatialIndex bool      `json:"has_spatial_index"`
	IndexName       string    `json:"index_name,omitempty"`
	HasUpdatedAt    bool      `json:"has_updated_at"`
	UpdatedAtColumn string    `json:"updated_at_column,omitempty"`
}

// QuickViewProgress 快显进度
type QuickViewProgress struct {
	Status                string          `json:"status"` // pending/running/completed/failed
	CurrentZoom           int             `json:"current_zoom"`
	MaxZoom               int             `json:"max_zoom"`
	TilesProcessed        int             `json:"tiles_processed"`
	TilesTotalEstimate    int             `json:"tiles_total_estimate"`
	ProgressPercent       float64         `json:"progress_percent"`
	ElapsedSeconds        float64         `json:"elapsed_seconds"`
	EstimatedRemainingSec float64         `json:"estimated_remaining_seconds"`
	CurrentStats          *ZoomLevelStats `json:"current_stats,omitempty"`
	ErrorMessage          string          `json:"error_message,omitempty"`
}
