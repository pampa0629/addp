package mvt

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/addp/common/logger"
	metaModels "github.com/addp/meta/internal/models"
)

// PreprocessService 预处理服务
// 负责批量生成 MVT 瓦片并存储到 MinIO
type PreprocessService struct {
	tileGen *TileGenerator
	cache   *CacheService
}

// PreprocessConfig 预处理配置
type PreprocessConfig struct {
	MaxZoom          int     // 最大缩放级别
	Concurrency      int     // 并发数
	StopThresholdSec float64 // 停止阈值：平均生成时间（秒）
	StopThresholdKB  float64 // 停止阈值：平均瓦片大小（KB）
}

// NewPreprocessService 创建预处理服务
func NewPreprocessService(tileGen *TileGenerator, cache *CacheService) *PreprocessService {
	return &PreprocessService{
		tileGen: tileGen,
		cache:   cache,
	}
}

// StartPreprocess 开始预处理
// 返回: metadata, error
func (s *PreprocessService) StartPreprocess(
	ctx context.Context,
	item *metaModels.MetaItem,
	tenantID uint,
	cfg PreprocessConfig,
) (*PreprocessMetadata, error) {
	startTime := time.Now()

	logger.L().Info("开始预处理 MVT 瓦片",
		"item_id", item.ID,
		"table", item.Name,
		"max_zoom", cfg.MaxZoom,
		"concurrency", cfg.Concurrency)

	// 1. 获取空间范围
	extent, err := s.tileGen.GetSpatialExtent(ctx, item, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get spatial extent: %w", err)
	}

	logger.L().Info("空间范围",
		"extent", extent,
		"item_id", item.ID)

	// 2. 提取元数据
	spatialMeta := item.Attributes["spatial_metadata"].(map[string]interface{})
	schema, _ := item.Attributes["schema_name"].(string)
	srid, _ := spatialMeta["srid"].(float64)
	geomTypes := []string{}
	if gt, ok := spatialMeta["geometry_types"].([]interface{}); ok {
		for _, g := range gt {
			if gs, ok := g.(string); ok {
				geomTypes = append(geomTypes, gs)
			}
		}
	}

	// 3. 初始化元数据
	metadata := &PreprocessMetadata{
		NodeID:        item.ID,
		Fingerprint:   item.Fingerprint,
		TableName:     item.Name,
		Schema:        schema,
		Extent:        extent,
		SRID:          int(srid),
		RowCount:      *item.RowCount,
		GeometryTypes: geomTypes,
		ZoomLevels:    make(map[string]ZoomLevelStats),
		CreatedAt:     time.Now(),
	}

	// 4. 逐层生成瓦片
	var totalTiles int
	var totalSize int64

	for z := 0; z <= cfg.MaxZoom; z++ {
		logger.L().Info("开始生成缩放级别",
			"zoom", z,
			"item_id", item.ID)

		stats, err := s.processZoomLevel(ctx, item, tenantID, z, extent, cfg.Concurrency)
		if err != nil {
			return nil, fmt.Errorf("failed to process zoom level %d: %w", z, err)
		}

		// 保存统计信息
		metadata.ZoomLevels[fmt.Sprintf("%d", z)] = *stats
		totalTiles += stats.GeneratedTiles
		totalSize += stats.TotalSizeBytes

		logger.L().Info("缩放级别完成",
			"zoom", z,
			"generated_tiles", stats.GeneratedTiles,
			"empty_tiles", stats.EmptyTiles,
			"avg_gen_time_ms", stats.AvgGenTimeMs,
			"avg_size_kb", stats.AvgSizeKB)

		// 5. 检查自适应停止条件
		if s.shouldStop(stats, cfg) {
			logger.L().Info("达到自适应停止阈值",
				"zoom", z,
				"avg_gen_time_ms", stats.AvgGenTimeMs,
				"avg_size_kb", stats.AvgSizeKB,
				"threshold_sec", cfg.StopThresholdSec,
				"threshold_kb", cfg.StopThresholdKB)

			metadata.MaxZoomGenerated = z
			metadata.StopReason = "adaptive_threshold"
			break
		}

		// 达到最大层级
		if z == cfg.MaxZoom {
			metadata.MaxZoomGenerated = z
			metadata.StopReason = "max_zoom_reached"
		}
	}

	// 6. 完成统计
	metadata.TotalTiles = totalTiles
	metadata.TotalSizeBytes = totalSize
	metadata.GenerationSec = time.Since(startTime).Seconds()

	// 7. 保存元数据到 MinIO
	if err := s.cache.PutMetadata(ctx, item.Fingerprint, metadata); err != nil {
		logger.L().Error("Failed to save metadata", "error", err)
		// 不返回错误，元数据保存失败不影响瓦片使用
	}

	logger.L().Info("预处理完成",
		"item_id", item.ID,
		"total_tiles", totalTiles,
		"total_size_mb", float64(totalSize)/(1024*1024),
		"duration_sec", metadata.GenerationSec,
		"max_zoom", metadata.MaxZoomGenerated)

	return metadata, nil
}

// processZoomLevel 处理单个缩放级别
func (s *PreprocessService) processZoomLevel(
	ctx context.Context,
	item *metaModels.MetaItem,
	tenantID uint,
	zoom int,
	extent []float64,
	concurrency int,
) (*ZoomLevelStats, error) {
	// 1. 计算瓦片范围
	minX, minY, maxX, maxY := calculateTileBounds(extent, zoom)
	totalTiles := (maxX - minX + 1) * (maxY - minY + 1)

	stats := &ZoomLevelStats{
		Zoom:       zoom,
		TotalTiles: totalTiles,
	}

	// 2. 创建任务队列
	jobs := make(chan TileCoord, totalTiles)
	results := make(chan *tileResult, totalTiles)

	// 3. 启动 Worker Pool
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for coord := range jobs {
				result := s.processTile(ctx, item, tenantID, coord)
				results <- result
			}
		}()
	}

	// 4. 投递任务
	go func() {
		for x := minX; x <= maxX; x++ {
			for y := minY; y <= maxY; y++ {
				jobs <- TileCoord{Z: zoom, X: x, Y: y}
			}
		}
		close(jobs)
	}()

	// 5. 等待完成并收集结果
	go func() {
		wg.Wait()
		close(results)
	}()

	// 6. 统计结果
	var totalGenTime float64
	var totalSize int64

	for result := range results {
		if result.err != nil {
			logger.L().Error("Failed to process tile",
				"z", result.coord.Z,
				"x", result.coord.X,
				"y", result.coord.Y,
				"error", result.err)
			continue
		}

		if result.isEmpty {
			stats.EmptyTiles++
		} else {
			stats.GeneratedTiles++
			totalGenTime += result.genTimeMs
			totalSize += result.sizeBytes
		}
	}

	// 7. 计算平均值（只统计有数据的瓦片）
	if stats.GeneratedTiles > 0 {
		stats.AvgGenTimeMs = totalGenTime / float64(stats.GeneratedTiles)
		stats.AvgSizeKB = bytesToKB(totalSize) / float64(stats.GeneratedTiles)
		stats.TotalSizeBytes = totalSize
	}

	return stats, nil
}

// tileResult 瓦片处理结果
type tileResult struct {
	coord     TileCoord
	isEmpty   bool
	genTimeMs float64
	sizeBytes int64
	err       error
}

// processTile 处理单个瓦片
func (s *PreprocessService) processTile(
	ctx context.Context,
	item *metaModels.MetaItem,
	tenantID uint,
	coord TileCoord,
) *tileResult {
	startTime := time.Now()

	// 1. 生成瓦片
	mvtData, err := s.tileGen.GenerateTile(ctx, item, tenantID, coord.Z, coord.X, coord.Y)
	if err != nil {
		return &tileResult{
			coord: coord,
			err:   err,
		}
	}

	// 2. 空瓦片，不存储
	if len(mvtData) == 0 {
		return &tileResult{
			coord:     coord,
			isEmpty:   true,
			genTimeMs: float64(time.Since(startTime).Milliseconds()),
		}
	}

	// 3. 压缩
	gzData, err := gzipCompress(mvtData)
	if err != nil {
		return &tileResult{
			coord: coord,
			err:   fmt.Errorf("gzip compress failed: %w", err),
		}
	}

	// 4. 存储到 MinIO
	err = s.cache.PutTile(ctx, item.Fingerprint, coord.Z, coord.X, coord.Y, gzData)
	if err != nil {
		return &tileResult{
			coord: coord,
			err:   fmt.Errorf("put tile failed: %w", err),
		}
	}

	return &tileResult{
		coord:     coord,
		isEmpty:   false,
		genTimeMs: float64(time.Since(startTime).Milliseconds()),
		sizeBytes: int64(len(mvtData)),
	}
}

// shouldStop 判断是否应该停止生成
func (s *PreprocessService) shouldStop(stats *ZoomLevelStats, cfg PreprocessConfig) bool {
	if stats.GeneratedTiles == 0 {
		return true // 当前层无数据，停止
	}

	// 平均生成时间 < 阈值 且 平均大小 < 阈值
	return stats.AvgGenTimeMs < cfg.StopThresholdSec*1000 &&
		stats.AvgSizeKB < cfg.StopThresholdKB
}
