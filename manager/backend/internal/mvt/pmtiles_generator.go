package mvt

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	commonPMTiles "github.com/addp/common/pmtiles"
	"github.com/addp/common/spatial"
)

type PMTilesTileSource interface {
	GenerateTile(context.Context, TileGenerationParams) ([]byte, error)
	QuerySourceSRID(context.Context, uint, uint, string, string, string) (int, error)
	GetSpatialExtent(context.Context, uint, uint, string, string, string) ([]float64, error)
}

type PMTilesGenerator struct {
	tileSource PMTilesTileSource
}

type GeneratedPMTilesArchive struct {
	Path       string
	HeaderHash string
	Size       int64
	Result     *GenerateResult
}

func NewPMTilesGenerator(tileSource PMTilesTileSource) *PMTilesGenerator {
	return &PMTilesGenerator{tileSource: tileSource}
}

func (a *GeneratedPMTilesArchive) Close() error {
	if a == nil || a.Path == "" {
		return nil
	}
	err := os.Remove(a.Path)
	a.Path = ""
	return err
}

type pmtilesTileJob struct {
	coord TileCoord
}

type pmtilesZoomRange struct {
	zoom                   int
	minX, minY, maxX, maxY int
	total                  int
}

type pmtilesTileResult struct {
	job       pmtilesTileJob
	data      []byte
	rawSize   int64
	duration  time.Duration
	empty     bool
	oversized bool
	err       error
}

func (g *PMTilesGenerator) Generate(ctx context.Context, cfg QuickViewConfig, progress ProgressSink) (*GeneratedPMTilesArchive, error) {
	if g == nil || g.tileSource == nil {
		return nil, errors.New("PostGIS PMTiles tile source is required")
	}
	if cfg.EngineID == 0 || cfg.Schema == "" || cfg.Table == "" || cfg.GeomColumn == "" {
		return nil, errors.New("PostGIS PMTiles source identity is incomplete")
	}
	if cfg.MinZoom < 0 || cfg.MaxZoom < cfg.MinZoom || cfg.MaxZoom > 31 {
		return nil, errors.New("PostGIS PMTiles zoom range is invalid")
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.LayerName == "" {
		cfg.LayerName = cfg.Table
	}

	startedAt := time.Now()
	actualSRID, err := g.tileSource.QuerySourceSRID(ctx, cfg.EngineID, cfg.TenantID, cfg.Schema, cfg.Table, cfg.GeomColumn)
	if err != nil {
		return nil, fmt.Errorf("query PostGIS source SRID: %w", err)
	}
	cfg.SRID = actualSRID
	extent, err := resolveTileRangeExtentWGS84(ctx, cfg, g.tileSource.GetSpatialExtent)
	if err != nil {
		return nil, err
	}
	zoomRanges, totalTiles := pmtilesZoomRanges(extent, cfg.MinZoom, cfg.MaxZoom)
	if totalTiles == 0 {
		return nil, errors.New("PostGIS PMTiles source extent produced no tile coordinates")
	}

	writer, err := commonPMTiles.NewWriter(commonPMTiles.WriterOptions{
		Bounds:  [4]float64{extent[0], extent[1], extent[2], extent[3]},
		MinZoom: uint8(cfg.MinZoom), MaxZoom: uint8(cfg.MaxZoom),
		Metadata: map[string]interface{}{
			"name": cfg.LayerName, "format": "pbf",
			"vector_layers": []map[string]interface{}{{"id": cfg.LayerName}},
		},
	})
	if err != nil {
		return nil, err
	}
	defer writer.Close()

	result := &GenerateResult{
		TilesTotalEstimate: totalTiles, ZoomLevels: make(map[string]ZoomLevelStats),
		MinTileSizeBytes: math.MaxInt64, ActualMaxZoom: cfg.MinZoom, ExtentWGS84: append([]float64(nil), extent...),
	}
	zoomDurations := make(map[int]time.Duration)
	if progress != nil {
		_ = progress.UpdateProgress(ctx, &QuickViewProgress{Status: "running", CurrentZoom: cfg.MinZoom, MaxZoom: cfg.MaxZoom, TilesTotalEstimate: totalTiles})
	}

	jobs := make([]pmtilesTileJob, 0, cfg.Concurrency)
	flush := func() error {
		if len(jobs) == 0 {
			return nil
		}
		lastZoom, err := g.processBatch(ctx, cfg, jobs, writer, result, zoomDurations)
		jobs = jobs[:0]
		if err != nil {
			return err
		}
		if progress != nil {
			percent := float64(result.TilesProcessed) / float64(totalTiles) * 100
			_ = progress.UpdateProgress(ctx, &QuickViewProgress{
				Status: "running", CurrentZoom: lastZoom, MaxZoom: cfg.MaxZoom,
				TilesProcessed: result.TilesProcessed, TilesTotalEstimate: totalTiles, ProgressPercent: percent,
			})
		}
		return nil
	}
	for _, zoomRange := range zoomRanges {
		for x := zoomRange.minX; x <= zoomRange.maxX; x++ {
			for y := zoomRange.minY; y <= zoomRange.maxY; y++ {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				jobs = append(jobs, pmtilesTileJob{coord: TileCoord{Z: zoomRange.zoom, X: x, Y: y}})
				if len(jobs) == cap(jobs) {
					if err := flush(); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if result.GeneratedTiles == 0 {
		return nil, errors.New("PostGIS PMTiles generation produced no non-empty tiles")
	}

	for key, stats := range result.ZoomLevels {
		if stats.GeneratedTiles > 0 {
			stats.AvgGenTimeMs = float64(zoomDurations[stats.Zoom].Microseconds()) / 1000 / float64(stats.GeneratedTiles)
			stats.AvgSizeKB = float64(stats.TotalSizeBytes) / 1024 / float64(stats.GeneratedTiles)
		}
		if stats.MinSizeBytes == math.MaxInt64 {
			stats.MinSizeBytes = 0
		}
		result.ZoomLevels[key] = stats
	}
	if result.MinTileSizeBytes == math.MaxInt64 {
		result.MinTileSizeBytes = 0
	}
	archiveFile, err := os.CreateTemp("", "addp-postgis-*.pmtiles")
	if err != nil {
		return nil, fmt.Errorf("create PostGIS PMTiles archive: %w", err)
	}
	archivePath := archiveFile.Name()
	removeArchive := true
	defer func() {
		_ = archiveFile.Close()
		if removeArchive {
			_ = os.Remove(archivePath)
		}
	}()
	if _, err := writer.WriteTo(archiveFile); err != nil {
		return nil, err
	}
	if err := archiveFile.Sync(); err != nil {
		return nil, fmt.Errorf("sync PostGIS PMTiles archive: %w", err)
	}
	info, err := archiveFile.Stat()
	if err != nil {
		return nil, err
	}
	headerBytes := make([]byte, commonPMTiles.HeaderSize)
	if _, err := archiveFile.ReadAt(headerBytes, 0); err != nil {
		return nil, fmt.Errorf("read generated PMTiles header: %w", err)
	}
	headerHash, err := commonPMTiles.HeaderHash(headerBytes)
	if err != nil {
		return nil, err
	}
	result.ArchiveHeaderHash = headerHash
	result.ArchiveSizeBytes = info.Size()
	result.GenerationSec = time.Since(startedAt).Seconds()
	result.StopReason = "postgis_st_asmvt_pmtiles"
	if progress != nil {
		_ = progress.UpdateProgress(ctx, &QuickViewProgress{
			Status: "ready", CurrentZoom: result.ActualMaxZoom, MaxZoom: cfg.MaxZoom,
			TilesProcessed: result.TilesProcessed, TilesTotalEstimate: totalTiles, ProgressPercent: 100,
		})
	}
	removeArchive = false
	return &GeneratedPMTilesArchive{Path: archivePath, HeaderHash: headerHash, Size: info.Size(), Result: result}, nil
}

func (g *PMTilesGenerator) processBatch(ctx context.Context, cfg QuickViewConfig, jobs []pmtilesTileJob, writer *commonPMTiles.Writer, result *GenerateResult, zoomDurations map[int]time.Duration) (int, error) {
	batch := make([]pmtilesTileResult, len(jobs))
	var wait sync.WaitGroup
	for index, job := range jobs {
		wait.Add(1)
		go func(index int, job pmtilesTileJob) {
			defer wait.Done()
			batch[index] = g.generateTile(ctx, cfg, job)
		}(index, job)
	}
	wait.Wait()
	for _, tile := range batch {
		if tile.err != nil {
			return tile.job.coord.Z, tile.err
		}
		applyPMTilesResult(result, zoomDurations, tile)
		if len(tile.data) > 0 {
			if err := writer.AddTile(uint8(tile.job.coord.Z), uint32(tile.job.coord.X), uint32(tile.job.coord.Y), tile.data); err != nil {
				return tile.job.coord.Z, err
			}
		}
	}
	return batch[len(batch)-1].job.coord.Z, nil
}

func (g *PMTilesGenerator) generateTile(ctx context.Context, cfg QuickViewConfig, job pmtilesTileJob) pmtilesTileResult {
	startedAt := time.Now()
	data, err := g.tileSource.GenerateTile(ctx, TileGenerationSource{
		EngineID: cfg.EngineID, TenantID: cfg.TenantID, Schema: cfg.Schema, Table: cfg.Table,
		GeomColumn: cfg.GeomColumn, SRID: cfg.SRID, PrimaryKey: cfg.PrimaryKey, MaxZoom: cfg.MaxZoom,
		OptimizationConfig: cfg.OptimizationConfig,
	}.Params(job.coord))
	result := pmtilesTileResult{job: job, duration: time.Since(startedAt)}
	if err != nil {
		if errors.Is(err, ErrMVTTileOversized) {
			result.oversized = true
			return result
		}
		result.err = fmt.Errorf("generate PostGIS MVT %d/%d/%d: %w", job.coord.Z, job.coord.X, job.coord.Y, err)
		return result
	}
	if len(data) == 0 {
		result.empty = true
		return result
	}
	result.rawSize = int64(len(data))
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(data); err != nil {
		result.err = err
		return result
	}
	if err := zw.Close(); err != nil {
		result.err = err
		return result
	}
	result.data = compressed.Bytes()
	return result
}

func pmtilesZoomRanges(extent []float64, minZoom, maxZoom int) ([]pmtilesZoomRange, int) {
	ranges := make([]pmtilesZoomRange, 0, maxZoom-minZoom+1)
	total := 0
	for zoom := minZoom; zoom <= maxZoom; zoom++ {
		minX, minY, maxX, maxY := calculateTileBounds(extent, zoom)
		count := (maxX - minX + 1) * (maxY - minY + 1)
		ranges = append(ranges, pmtilesZoomRange{zoom: zoom, minX: minX, minY: minY, maxX: maxX, maxY: maxY, total: count})
		total += count
	}
	return ranges, total
}

func resolveTileRangeExtentWGS84(ctx context.Context, cfg QuickViewConfig, loadExtent func(context.Context, uint, uint, string, string, string) ([]float64, error)) ([]float64, error) {
	if cfg.ExtentSRID == spatial.SRIDWGS84 {
		if len(cfg.Extent) != 4 {
			return nil, errors.New("PostGIS PMTiles extent is invalid")
		}
		return append([]float64(nil), cfg.Extent...), nil
	}
	if cfg.ExtentSRID <= 0 {
		return nil, errors.New("PostGIS PMTiles extent_srid is required")
	}
	extent, err := loadExtent(ctx, cfg.EngineID, cfg.TenantID, cfg.Schema, cfg.Table, cfg.GeomColumn)
	if err != nil {
		return nil, fmt.Errorf("resolve PostGIS PMTiles WGS84 extent: %w", err)
	}
	if len(extent) != 4 {
		return nil, errors.New("resolved PostGIS PMTiles WGS84 extent is invalid")
	}
	return extent, nil
}

func applyPMTilesResult(result *GenerateResult, durations map[int]time.Duration, tile pmtilesTileResult) {
	result.TotalTiles++
	result.TilesProcessed++
	zoom := tile.job.coord.Z
	key := fmt.Sprintf("%d", zoom)
	stats := result.ZoomLevels[key]
	stats.Zoom = zoom
	stats.TotalTiles++
	if tile.oversized {
		result.SkippedTiles++
		result.OversizedTiles++
		stats.SkippedTiles++
		stats.OversizedTiles++
	} else if tile.empty {
		result.EmptyTiles++
		stats.EmptyTiles++
	} else {
		result.CachedTiles++
		result.GeneratedTiles++
		result.TotalSizeBytes += tile.rawSize
		result.ActualMaxZoom = zoom
		stats.GeneratedTiles++
		stats.TotalSizeBytes += tile.rawSize
		durations[zoom] += tile.duration
		if tile.rawSize > result.MaxTileSizeBytes {
			result.MaxTileSizeBytes = tile.rawSize
		}
		if tile.rawSize < result.MinTileSizeBytes {
			result.MinTileSizeBytes = tile.rawSize
		}
		if tile.rawSize > stats.MaxSizeBytes {
			stats.MaxSizeBytes = tile.rawSize
		}
		if stats.MinSizeBytes == 0 || tile.rawSize < stats.MinSizeBytes {
			stats.MinSizeBytes = tile.rawSize
		}
	}
	result.ZoomLevels[key] = stats
}
