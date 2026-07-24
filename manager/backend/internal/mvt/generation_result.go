package mvt

import commonModels "github.com/addp/common/models"

// QuickViewConfig is the normalized execution snapshot used for PMTiles generation.
type QuickViewConfig struct {
	EngineID           uint
	TenantID           uint
	Schema             string
	Table              string
	GeomColumn         string
	SRID               int
	PrimaryKey         string
	Extent             []float64
	ExtentSRID         int
	MinZoom            int
	MaxZoom            int
	Fingerprint        string
	StorageRef         string
	LayerName          string
	Concurrency        int
	OptimizationConfig *commonModels.OptimizationConfig
}

type GenerateResult struct {
	TotalTiles         int
	CachedTiles        int
	TilesTotalEstimate int
	TilesProcessed     int
	GeneratedTiles     int
	EmptyTiles         int
	SkippedTiles       int
	OversizedTiles     int
	FailedTiles        int
	TotalSizeBytes     int64
	MaxTileSizeBytes   int64
	MinTileSizeBytes   int64
	ZoomLevels         map[string]ZoomLevelStats
	ActualMaxZoom      int
	StopReason         string
	GenerationSec      float64
	ExtentWGS84        []float64
	ArchiveHeaderHash  string
	ArchiveSizeBytes   int64
}
