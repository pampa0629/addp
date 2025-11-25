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
	jobs := make(chan TileCoord, cfg.Concurrency*4) // 缓冲区为并发数的4倍
	results := make(chan *tileResult, cfg.Concurrency*4)

	// 3. 每层统计数据（线程安全）
	type zoomStats struct {
		sync.Mutex
		totalTiles     int
		generatedTiles int
		emptyTiles     int
		totalGenTime   float64
		totalSize      int64
		stopped        bool // 该层是否已达到停止条件
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

	// 5. 动态任务分发器（在单独的goroutine中）
	go func() {
		defer close(jobs)

		// 为每层维护当前任务索引
		type progress struct {
			currentX, currentY int
			finished           bool
		}

		zoomProgress := make(map[int]*progress)
		for _, zr := range zoomRanges {
			zoomProgress[zr.zoom] = &progress{
				currentX: zr.minX,
				currentY: zr.minY,
				finished: false,
			}
		}

		// 持续分发任务，直到所有层级完成或停止
		for {
			allFinished := true
			tasksDispatched := false

			// 遍历所有层级，尝试分发任务
			for _, zr := range zoomRanges {
				stats := statsMap[zr.zoom]
				prog := zoomProgress[zr.zoom]

				// 跳过已完成或已停止的层级
				if prog.finished {
					continue
				}

				allFinished = false

				// 检查该层是否应该停止
				stats.Lock()
				if stats.stopped {
					stats.Unlock()
					prog.finished = true
					logger.L().Info("层级已停止任务分发", "zoom", zr.zoom,
						"generated", stats.generatedTiles, "total", stats.totalTiles)
					continue
				}
				stats.Unlock()

				// 分发该层的下一个瓦片任务
				if prog.currentY <= zr.maxY {
					select {
					case jobs <- TileCoord{Z: zr.zoom, X: prog.currentX, Y: prog.currentY}:
						tasksDispatched = true

						// 更新进度
						prog.currentX++
						if prog.currentX > zr.maxX {
							prog.currentX = zr.minX
							prog.currentY++
						}

						// 检查是否完成
						if prog.currentY > zr.maxY {
							prog.finished = true
						}

					case <-ctx.Done():
						return
					default:
						// 队列满，跳过此层，尝试下一层
					}
				} else {
					prog.finished = true
				}
			}

			// 所有层级完成
			if allFinished {
				break
			}

			// 如果本轮没有分发任何任务，稍作休息避免busy loop
			if !tasksDispatched {
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// 6. 结果收集器
	go func() {
		wg.Wait()
		close(results)
	}()

	// 7. 处理结果并实时统计
	processedTiles := 0
	for result := range results {
		stats := statsMap[result.coord.Z]
		stats.Lock()

		if result.err == nil && result.sizeBytes > 0 {
			stats.generatedTiles++
			stats.totalGenTime += result.genTimeMs * 1e6 // 转换为纳秒
			stats.totalSize += result.sizeBytes
		} else {
			stats.emptyTiles++
		}

		processed := stats.generatedTiles + stats.emptyTiles

		// 实时检查停止条件（每处理10个瓦片检查一次）
		if !stats.stopped && processed >= 10 && processed%10 == 0 {
			avgGenTimeMs := 0.0
			avgSizeKB := 0.0
			if stats.generatedTiles > 0 {
				avgGenTimeMs = stats.totalGenTime / float64(stats.generatedTiles) / 1e6
				avgSizeKB = float64(stats.totalSize) / float64(stats.generatedTiles) / 1024.0
			}

			if avgGenTimeMs > cfg.StopThresholdMs || avgSizeKB > cfg.StopThresholdKB {
				stats.stopped = true
				logger.L().Info("层级达到停止阈值",
					"zoom", result.coord.Z,
					"avg_time_ms", avgGenTimeMs,
					"avg_size_kb", avgSizeKB,
					"threshold_ms", cfg.StopThresholdMs,
					"threshold_kb", cfg.StopThresholdKB)
			}
		}

		stats.Unlock()

		processedTiles++
		if processedTiles%100 == 0 {
			logger.L().Info("快显生成进度",
				"processed", processedTiles,
				"total_estimated", totalTaskCount)
		}
	}

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
			"empty", stats.emptyTiles,
			"total", stats.totalTiles,
			"avg_time_ms", avgTimeMs,
			"avg_size_kb", avgSizeKB,
			"stopped", stats.stopped)

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
