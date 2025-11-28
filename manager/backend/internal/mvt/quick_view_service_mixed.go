package mvt

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/addp/common/logger"
)

// GenerateMixed 混合入队模式的快显缓存生成
// 与串行Generate不同，该方法会同时处理多个层级的瓦片，充分利用并发
func (s *QuickViewService) GenerateMixed(
	ctx context.Context,
	cfg QuickViewConfig,
) (*GenerateResult, error) {
	startTime := time.Now()

	logger.L().Info("开始生成快显缓存（混合入队模式）",
		"resource_id", cfg.ResourceID,
		"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
		"min_zoom", cfg.MinZoom,
		"max_zoom", cfg.MaxZoom,
		"concurrency", cfg.Concurrency)

	// 1. 预先计算所有层级的瓦片范围
	type zoomRange struct {
		zoom                   int
		minX, minY, maxX, maxY int
		totalTiles             int
	}

	var zoomRanges []zoomRange
	totalTaskCount := 0

	for z := cfg.MinZoom; z <= cfg.MaxZoom; z++ {
		minX, minY, maxX, maxY := calculateTileBounds(cfg.Extent, z)
		tiles := (maxX - minX + 1) * (maxY - minY + 1)
		zoomRanges = append(zoomRanges, zoomRange{
			zoom:       z,
			minX:       minX,
			minY:       minY,
			maxX:       maxX,
			maxY:       maxY,
			totalTiles: tiles,
		})
		totalTaskCount += tiles
		logger.L().Info("计算层级范围", "zoom", z, "tiles", tiles,
			"range", fmt.Sprintf("x[%d-%d] y[%d-%d]", minX, maxX, minY, maxY))
	}

	// 2. 创建统一的任务队列和结果通道
	jobs := make(chan TileCoord, cfg.Concurrency*2) // 缓冲区为并发数的2倍
	results := make(chan *tileResult, cfg.Concurrency*4)

	// 3. 每层统计数据（线程安全）
	type zoomStats struct {
		sync.Mutex
		totalTiles     int
		generatedTiles int
		emptyTiles     int
		skippedTiles   int // 跳过的瓦片数（已存在）
		totalGenTime   float64
		totalSize      int64
	}

	statsMap := make(map[int]*zoomStats)
	for _, zr := range zoomRanges {
		statsMap[zr.zoom] = &zoomStats{totalTiles: zr.totalTiles}
	}

	// 4. 启动 Worker Pool
	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for coord := range jobs {
				result := s.processTile(ctx, cfg, coord)
				results <- result
			}
		}(i)
	}

	// 5. 流式任务分发器（在单独的goroutine中）
	go func() {
		defer close(jobs)

		logger.L().Info("📋 开始流式分发任务",
			"total_tasks", totalTaskCount,
			"min_zoom", cfg.MinZoom,
			"max_zoom", cfg.MaxZoom,
			"concurrency", cfg.Concurrency)

		// 先打印所有层级的队列计划
		logger.L().Info("📊 预缓存队列计划:")
		for _, zr := range zoomRanges {
			logger.L().Info(fmt.Sprintf("  z%d: %d 个瓦片 (x[%d-%d] y[%d-%d])",
				zr.zoom, zr.totalTiles, zr.minX, zr.maxX, zr.minY, zr.maxY))
		}

		dispatched := 0
		dispatchedPerZoom := make(map[int]int) // 记录每层实际分发的任务数

		for _, zr := range zoomRanges {
			logger.L().Info(fmt.Sprintf("🚀 开始分发 z%d 层级任务", zr.zoom),
				"zoom", zr.zoom,
				"expected_tiles", zr.totalTiles,
				"range", fmt.Sprintf("x[%d-%d] y[%d-%d]", zr.minX, zr.maxX, zr.minY, zr.maxY))

			zoomStartDispatched := dispatched

			for x := zr.minX; x <= zr.maxX; x++ {
				for y := zr.minY; y <= zr.maxY; y++ {
					select {
					case jobs <- TileCoord{Z: zr.zoom, X: x, Y: y}:
						dispatched++

						// 每层前10个任务详细记录
						if dispatched-zoomStartDispatched <= 10 {
							logger.L().Debug(fmt.Sprintf("  ✓ 已入队: z%d/%d/%d", zr.zoom, x, y))
						}

						if dispatched%100 == 0 {
							progress := float64(dispatched) / float64(totalTaskCount) * 100
							logger.L().Info("📈 任务分发进度",
								"dispatched", dispatched,
								"total", totalTaskCount,
								"progress", fmt.Sprintf("%.1f%%", progress),
								"current_zoom", zr.zoom)
						}

					case <-ctx.Done():
						logger.L().Warn("⚠️  任务分发被中断",
							"dispatched", dispatched,
							"total", totalTaskCount,
							"stopped_at_zoom", zr.zoom)
						return
					}
				}
			}

			actualDispatched := dispatched - zoomStartDispatched
			dispatchedPerZoom[zr.zoom] = actualDispatched

			logger.L().Info(fmt.Sprintf("✅ z%d 层级任务分发完成", zr.zoom),
				"zoom", zr.zoom,
				"expected", zr.totalTiles,
				"actual_dispatched", actualDispatched,
				"match", actualDispatched == zr.totalTiles)
		}

		// 最终汇总
		logger.L().Info("🎉 所有任务分发完成",
			"total_dispatched", dispatched,
			"expected_total", totalTaskCount,
			"match", dispatched == totalTaskCount)

		// 打印每层分发统计
		logger.L().Info("📊 各层级分发统计:")
		for _, zr := range zoomRanges {
			actual := dispatchedPerZoom[zr.zoom]
			logger.L().Info(fmt.Sprintf("  z%d: %d/%d 个任务 %s",
				zr.zoom, actual, zr.totalTiles,
				func() string {
					if actual == zr.totalTiles {
						return "✅"
					}
					return fmt.Sprintf("❌ 缺失 %d 个", zr.totalTiles-actual)
				}()))
		}
	}()

	// 6. 结果收集器
	go func() {
		wg.Wait()
		close(results)
	}()

	// 7. 处理结果并实时统计
	processedTiles := 0
	processedPerZoom := make(map[int]int) // 记录每层实际处理完成的瓦片数

	for result := range results {
		stats := statsMap[result.coord.Z]
		stats.Lock()

		if result.err == nil {
			if result.sizeBytes > 0 {
				// 新生成的瓦片
				stats.generatedTiles++
				stats.totalGenTime += result.genTimeMs * 1e6 // 转换为纳秒
				stats.totalSize += result.sizeBytes
			} else if result.sizeBytes == -1 {
				// 跳过的瓦片（已存在）
				stats.skippedTiles++
			} else if result.isEmpty {
				// 空瓦片
				stats.emptyTiles++
			}
		}

		stats.Unlock()

		processedTiles++
		processedPerZoom[result.coord.Z]++

		if processedTiles%100 == 0 {
			logger.L().Info("⚙️  快显生成进度",
				"processed", processedTiles,
				"total_estimated", totalTaskCount)
		}
	}

	logger.L().Info("✅ 所有瓦片处理完成",
		"total_processed", processedTiles,
		"expected", totalTaskCount)

	// 8. 汇总结果
	result := &GenerateResult{}
	var lastAvgTime, lastAvgSize float64

	for _, zr := range zoomRanges {
		stats := statsMap[zr.zoom]
		stats.Lock()

		result.TotalTiles += stats.generatedTiles + stats.emptyTiles
		result.CachedTiles += stats.generatedTiles

		avgTimeMs := 0.0
		avgSizeKB := 0.0
		if stats.generatedTiles > 0 {
			avgTimeMs = stats.totalGenTime / float64(stats.generatedTiles) / 1e6
			avgSizeKB = float64(stats.totalSize) / float64(stats.generatedTiles) / 1024.0
		}

		logger.L().Info("层级统计",
			"zoom", zr.zoom,
			"generated", stats.generatedTiles,
			"skipped", stats.skippedTiles,
			"empty", stats.emptyTiles,
			"total", stats.totalTiles,
			"avg_time_ms", avgTimeMs,
			"avg_size_kb", avgSizeKB)

		// 记录最后有效层级（非空且已处理）
		if stats.generatedTiles > 0 {
			lastAvgTime = avgTimeMs
			lastAvgSize = avgSizeKB
		}

		// 确定实际最大层级（处理了任何瓦片的最高层级）
		if stats.generatedTiles > 0 || stats.emptyTiles > 0 {
			result.ActualMaxZoom = zr.zoom
		}

		stats.Unlock()
	}

	result.LastZoomAvgTimeMs = lastAvgTime
	result.LastZoomAvgSizeKB = lastAvgSize
	result.GenerationSec = time.Since(startTime).Seconds()
	result.StopReason = "adaptive_mixed_queue"

	// 9. 保存元数据到 MinIO
	metadata := &QuickViewMetadata{
		ResourceID:       cfg.ResourceID,
		Fingerprint:      cfg.Fingerprint,
		TableName:        cfg.Table,
		Schema:           cfg.Schema,
		Extent:           cfg.Extent,
		SRID:             cfg.SRID,
		GeometryTypes:    []string{}, // TODO: 从meta获取
		ZoomLevels:       make(map[string]ZoomLevelStats),
		MaxZoomGenerated: result.ActualMaxZoom,
		StopReason:       result.StopReason,
		TotalTiles:       result.CachedTiles,
		TotalSizeBytes:   0,
		CreatedAt:        time.Now(),
		GenerationSec:    result.GenerationSec, // 修复字段名
	}

	if err := s.putMetadata(ctx, cfg.Fingerprint, metadata); err != nil {
		logger.L().Warn("Failed to save metadata to MinIO", "error", err)
	}

	logger.L().Info("快显生成完成（混合模式）",
		"total_tiles", result.TotalTiles,
		"cached_tiles", result.CachedTiles,
		"duration_sec", result.GenerationSec,
		"max_zoom", result.ActualMaxZoom)

	return result, nil
}
