package mvt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

// QuickViewService 快显服务
// 负责批量生成 MVT 瓦片并存储到 MinIO
type QuickViewService struct {
	tileGen     *TileGenerator
	minioClient *minio.Client
	bucket      string
	db          *gorm.DB // 数据库连接（用于保存准备状态）
	prepService *PreparationService // 准备阶段服务
}

// QuickViewConfig 快显配置
type QuickViewConfig struct {
	EngineID           uint
	TenantID           uint
	Schema             string
	Table              string
	GeomColumn         string
	SRID               int
	PrimaryKey         string
	Extent             []float64                           // [minLng, minLat, maxLng, maxLat]
	MinZoom            int                                 // 自动计算或指定
	MaxZoom            int
	Concurrency        int
	Fingerprint        string
	OptimizationConfig *commonModels.OptimizationConfig    // v2.0 优化配置
}

// MinIOConfig MinIO 配置
type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string
}

// NewQuickViewService 创建快显服务
func NewQuickViewService(tileGen *TileGenerator, minioCfg MinIOConfig, db *gorm.DB) (*QuickViewService, error) {
	// 初始化 MinIO 客户端
	client, err := minio.New(minioCfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioCfg.AccessKey, minioCfg.SecretKey, ""),
		Secure: minioCfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 检查 bucket 是否存在
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := client.BucketExists(ctx, minioCfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		// 尝试创建 bucket
		if err := client.MakeBucket(ctx, minioCfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("failed to create bucket: %w", err)
		}
		logger.L().Info("Created MinIO bucket", "bucket", minioCfg.Bucket)
	}

	logger.L().Info("QuickView service initialized",
		"endpoint", minioCfg.Endpoint,
		"bucket", minioCfg.Bucket,
		"mode", "mixed")

	return &QuickViewService{
		tileGen:     tileGen,
		minioClient: client,
		bucket:      minioCfg.Bucket,
		db:          db,
		prepService: NewPreparationService(db, tileGen.resourceService),
	}, nil
}

// GenerateResult 快显生成结果
type GenerateResult struct {
	TotalTiles        int
	CachedTiles       int
	ActualMaxZoom     int
	LastZoomAvgTimeMs float64
	LastZoomAvgSizeKB float64
	StopReason        string
	GenerationSec     float64
}

// tileResult 瓦片处理结果
type tileResult struct {
	coord     TileCoord
	isEmpty   bool
	genTimeMs float64
	sizeBytes int64
	err       error
}

// GenerateMixed 混合入队模式的快显缓存生成
// 与串行Generate不同，该方法会同时处理多个层级的瓦片，充分利用并发
func (s *QuickViewService) GenerateMixed(
	ctx context.Context,
	cfg QuickViewConfig,
	progressTracker *ProgressTracker, // 可选的进度跟踪器（nil 时不更新进度）
) (*GenerateResult, error) {
	startTime := time.Now()

	logger.L().Info("开始生成快显缓存（混合入队模式）",
		"engine_id", cfg.EngineID,
		"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
		"geom_column", cfg.GeomColumn,
		"srid", cfg.SRID,
		"min_zoom", cfg.MinZoom,
		"max_zoom", cfg.MaxZoom,
		"concurrency", cfg.Concurrency)

	// 1. 验证 SRID 一致性（防止元数据与实际数据不一致导致错误）
	actualSRID, err := s.tileGen.VerifySRID(ctx, cfg.EngineID, cfg.TenantID, cfg.Schema, cfg.Table, cfg.GeomColumn, cfg.SRID)
	if err != nil {
		logger.L().Error("❌ SRID 验证失败",
			"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
			"expected_srid", cfg.SRID,
			"error", err)
		return nil, fmt.Errorf("SRID 验证失败: %w", err)
	}

	logger.L().Info("✅ SRID 验证通过",
		"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
		"srid", actualSRID)

	// 1.3 运行准备阶段检查（v4.0 新增）
	logger.L().Info("🔄 开始准备阶段检查",
		"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table))

	prepStatus, err := s.prepService.RunPreparationChecks(ctx, cfg.TenantID, cfg.EngineID, cfg.Schema, cfg.Table, cfg.GeomColumn)
	if err != nil {
		logger.L().Error("❌ 准备阶段检查失败",
			"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
			"error", err)
		return nil, fmt.Errorf("准备阶段检查失败: %w", err)
	}

	logger.L().Info("✅ 准备阶段检查完成",
		"overall_status", prepStatus.OverallStatus,
		"summary", prepStatus.Summary)

	// 如果准备阶段失败，返回错误并让前端显示失败信息
	if prepStatus.OverallStatus == "failed" {
		// 保存准备状态到数据库（以便前端显示）
		logger.L().Warn("⚠️ 准备阶段有失败项",
			"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
			"checks", prepStatus.Checks)
		return nil, fmt.Errorf("准备阶段检查失败，请手动处理后重试")
	}

	// 🆕 v4.0 检查是否需要使用物化视图（如果原始 SRID 不是 3857）
	// 原始 SRID 是在 actualSRID 中
	if actualSRID != 3857 {
		// 需要使用物化视图，修改配置
		cfg.Table = fmt.Sprintf("%s_mv3857", cfg.Table)
		cfg.GeomColumn = "geom_3857"
		logger.L().Info("🔄 启用物化视图（坐标系转换）",
			"original_srid", actualSRID,
			"mv_table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
			"mv_geom_column", cfg.GeomColumn)
	} else {
		logger.L().Info("✅ 原始表已是 3857 坐标系，无需物化视图",
			"srid", actualSRID)
	}


	// 1.5 精准统计信息采集（Phase 1诊断）
	stats, err := s.collectStatistics(ctx, cfg)
	if err != nil {
		logger.L().Warn("⚠️ 统计信息采集失败，继续生成",
			"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
			"error", err)
	} else {
		// 记录统计信息摘要
		logger.L().Info("📊 统计信息采集完成",
			"table_rows", stats.TableRows,
			"bounds", fmt.Sprintf("[%.2f,%.2f,%.2f,%.2f]", stats.Bounds[0], stats.Bounds[1], stats.Bounds[2], stats.Bounds[3]),
			"last_analyze_age_hours", stats.LastAnalyzeAgeHours,
			"needs_analyze", stats.NeedsAnalyze)
	}

	// 2. 预先计算所有层级的瓦片范围
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

	// 更新进度：开始生成
	if progressTracker != nil {
		progressTracker.UpdateProgress(ctx, &QuickViewProgress{
			Status:             "running",
			CurrentZoom:        cfg.MinZoom,
			MaxZoom:            cfg.MaxZoom,
			TilesProcessed:     0,
			TilesTotalEstimate: totalTaskCount,
			ProgressPercent:    0,
		})
	}

	// 3. 创建统一的任务队列和结果通道
	jobs := make(chan TileCoord, cfg.Concurrency*2) // 缓冲区为并发数的2倍
	results := make(chan *tileResult, cfg.Concurrency*4)

	// 3.5 启动独立的进度监控 goroutine（定期更新已用时，即使没有瓦片完成）
	progressDone := make(chan struct{})
	var processedTilesCount int32 // 原子计数器
	var currentZoom int32 = int32(cfg.MinZoom)

	if progressTracker != nil {
		logger.L().Info("🚀 启动进度监控 goroutine，每 2 秒更新一次")
		go func() {
			ticker := time.NewTicker(2 * time.Second) // 每 2 秒强制更新一次进度
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					// 读取当前进度（原子操作）
					processedCount := int(atomic.LoadInt32(&processedTilesCount))
					currentZ := int(atomic.LoadInt32(&currentZoom))

					progressPercent := 0.0
					if totalTaskCount > 0 {
						progressPercent = float64(processedCount) / float64(totalTaskCount) * 100
					}

					// 强制更新进度（包括已用时）
					err := progressTracker.UpdateProgress(ctx, &QuickViewProgress{
						Status:             "running",
						CurrentZoom:        currentZ,
						MaxZoom:            cfg.MaxZoom,
						TilesProcessed:     processedCount,
						TilesTotalEstimate: totalTaskCount,
						ProgressPercent:    progressPercent,
					})

					if err != nil {
						logger.L().Warn("进度监控 goroutine 更新失败", "error", err)
					} else {
						logger.L().Info("🔄 进度监控定时更新",
							"processed", processedCount,
							"total", totalTaskCount,
							"percent", fmt.Sprintf("%.1f%%", progressPercent))
					}

				case <-progressDone:
					logger.L().Info("✅ 进度监控 goroutine 已停止")
					return
				case <-ctx.Done():
					logger.L().Info("⚠️  进度监控 goroutine 被取消")
					return
				}
			}
		}()
	}

	// 4. 每层统计数据（线程安全）
	type zoomStats struct {
		sync.Mutex
		totalTiles         int
		generatedTiles     int
		emptyTiles         int
		skippedTiles       int // 跳过的瓦片数（已存在）
		totalGenTime       float64
		totalSize          int64
		maxTileSize        int64
		minTileSize        int64
		extentReducedCount int // Extent减半次数
	}

	statsMap := make(map[int]*zoomStats)
	for _, zr := range zoomRanges {
		statsMap[zr.zoom] = &zoomStats{
			totalTiles:  zr.totalTiles,
			minTileSize: math.MaxInt64,
		}
	}

	// 5. 启动 Worker Pool
	// 🔍 日志：记录实际启动的并发 goroutines 数量
	logger.L().Info("🚀 MVT Service: 启动 Worker Pool",
		"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
		"concurrency", cfg.Concurrency,
		"worker_count", cfg.Concurrency,
		"total_tasks", totalTaskCount)

	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		workerID := i // 捕获循环变量
		logger.L().Info("👷 启动 Worker",
			"worker_id", workerID,
			"total_workers", cfg.Concurrency)
		go func(id int) {
			defer wg.Done()
			logger.L().Debug("Worker 开始处理任务", "worker_id", id)
			tilesProcessed := 0
			for coord := range jobs {
				// ✅ 新增：检查 Context 是否被取消
				select {
				case <-ctx.Done():
					logger.L().Info("⚠️  Worker 检测到 Context 取消，停止处理新瓦片",
						"worker_id", id,
						"tiles_processed", tilesProcessed)
					return
				default:
				}

				// ✅ 新增：检查 Redis 取消标志
				if progressTracker != nil && progressTracker.IsCancelled(ctx) {
					logger.L().Info("⚠️  Worker 检测到 Redis 取消标志，停止处理新瓦片",
						"worker_id", id,
						"tiles_processed", tilesProcessed)
					return
				}

				result := s.processTile(ctx, cfg, coord)
				results <- result
				tilesProcessed++
			}
			logger.L().Info("Worker 完成所有任务", "worker_id", id, "tiles_processed", tilesProcessed)
		}(workerID)
	}

	logger.L().Info("✅ MVT Service: Worker Pool 已全部启动",
		"active_workers", cfg.Concurrency)

	// 6. 流式任务分发器（在单独的goroutine中）
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
	lastProgressUpdate := 0               // 上次更新进度时的瓦片数
	lastProgressUpdateTime := time.Now()  // 上次更新进度的时间

	// 计算动态更新阈值：每千分之一瓦片数更新一次（对应前端 0.1% 精度）
	updateThreshold := int(math.Ceil(float64(totalTaskCount) * 0.001))
	if updateThreshold < 1 {
		updateThreshold = 1 // 至少处理 1 个瓦片才更新
	}

	for result := range results {
		// 检查任务是否被取消（优先检查context）
		select {
		case <-ctx.Done():
			logger.L().Warn("⚠️  瓦片生成被context取消",
				"processed", processedTiles,
				"total", totalTaskCount,
				"progress", fmt.Sprintf("%.1f%%", float64(processedTiles)/float64(totalTaskCount)*100))

			// 更新进度为已取消
			if progressTracker != nil {
				progressTracker.UpdateProgress(ctx, &QuickViewProgress{
					Status:             "cancelled",
					CurrentZoom:        result.coord.Z,
					MaxZoom:            cfg.MaxZoom,
					TilesProcessed:     processedTiles,
					TilesTotalEstimate: totalTaskCount,
					ProgressPercent:    float64(processedTiles) / float64(totalTaskCount) * 100,
				})
				// 停止进度监控 goroutine
				close(progressDone)
			}

			// 返回取消错误
			return nil, context.Canceled
		default:
			// 继续处理
		}

	// 每个瓦片都检查Redis取消标志（性能影响<1%，用户体验最佳）
		if progressTracker != nil && progressTracker.IsCancelled(ctx) {
			logger.L().Warn("⚠️  瓦片生成被用户取消（Redis标志）",
				"processed", processedTiles,
				"total", totalTaskCount,
				"progress", fmt.Sprintf("%.1f%%", float64(processedTiles)/float64(totalTaskCount)*100))

			// 更新进度为已取消
			progressTracker.UpdateProgress(ctx, &QuickViewProgress{
				Status:             "cancelled",
				CurrentZoom:        result.coord.Z,
				MaxZoom:            cfg.MaxZoom,
				TilesProcessed:     processedTiles,
				TilesTotalEstimate: totalTaskCount,
				ProgressPercent:    float64(processedTiles) / float64(totalTaskCount) * 100,
			})

			// ⚠️ 不清除取消标志！原因：
			// 1. 多Worker场景下，清除标志会导致其他Worker无法感知取消
			// 2. Redis标志由cancel操作设置，由cancel操作负责清除
			// 3. 标志有TTL（1小时），会自动过期
			// 4. Worker只应该"读取"标志，不应该"修改"标志

			// 停止进度监控 goroutine
			close(progressDone)

			// 返回取消错误
			return nil, fmt.Errorf("task cancelled by user")
		}

		stats := statsMap[result.coord.Z]
		stats.Lock()

		if result.err == nil {
			if result.sizeBytes > 0 {
				// 新生成的瓦片
				stats.generatedTiles++
				stats.totalGenTime += result.genTimeMs * 1e6 // 转换为纳秒
				stats.totalSize += result.sizeBytes

				// 跟踪 min/max 瓦片大小
				if result.sizeBytes > stats.maxTileSize {
					stats.maxTileSize = result.sizeBytes
				}
				if result.sizeBytes < stats.minTileSize {
					stats.minTileSize = result.sizeBytes
				}
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

		// 更新原子计数器，供进度监控 goroutine 使用
		atomic.StoreInt32(&processedTilesCount, int32(processedTiles))
		atomic.StoreInt32(&currentZoom, int32(result.coord.Z))

		// 进度更新策略：满足以下任一条件即更新
		// 1. 每处理 threshold 个瓦片（千分之一总量，对应 0.1% 精度）
		// 2. 距离上次更新超过 3 秒
		// 3. 处理完所有瓦片
		timeSinceLastUpdate := time.Since(lastProgressUpdateTime)
		shouldUpdate := processedTiles-lastProgressUpdate >= updateThreshold ||
			timeSinceLastUpdate >= 3*time.Second ||
			processedTiles == totalTaskCount

		if progressTracker != nil && shouldUpdate {
			progressPercent := float64(processedTiles) / float64(totalTaskCount) * 100
			progressTracker.UpdateProgress(ctx, &QuickViewProgress{
				Status:             "running",
				CurrentZoom:        result.coord.Z,
				MaxZoom:            cfg.MaxZoom,
				TilesProcessed:     processedTiles,
				TilesTotalEstimate: totalTaskCount,
				ProgressPercent:    progressPercent,
			})
			lastProgressUpdate = processedTiles
			lastProgressUpdateTime = time.Now()
		}

		if processedTiles%10 == 0 {
			// 每10个瓦片输出一次进度和资源监控
			resourceInfo := s.getResourceMetrics(ctx, cfg)
			logger.L().Info(fmt.Sprintf("⚙️ z=%d epoch: processed=%d, CPU=%.0f%%, Mem=%.1fMB, DBConns=%d, QueryTime=%.1fms",
				result.coord.Z,
				processedTiles,
				resourceInfo.CPUPercent,
				resourceInfo.MemoryMB,
				resourceInfo.DBConnections,
				resourceInfo.QueryTimeMs),
				"processed", processedTiles,
				"total_estimated", totalTaskCount)
		}
	}

	logger.L().Info("✅ 所有瓦片处理完成",
		"total_processed", processedTiles,
		"expected", totalTaskCount)

	// 停止进度监控 goroutine
	if progressTracker != nil {
		close(progressDone)
	}

	// 8. 汇总结果与详细日志
	result := &GenerateResult{}
	var lastAvgTime, lastAvgSize float64
	var overallAvgTime float64 = 0

	// 第一遍：计算所有平均值用于异常检测
	var totalTime, totalTiles float64
	for _, zr := range zoomRanges {
		stats := statsMap[zr.zoom]
		stats.Lock()
		if stats.generatedTiles > 0 {
			totalTime += stats.totalGenTime / 1e6
			totalTiles += float64(stats.generatedTiles)
		}
		stats.Unlock()
	}
	if totalTiles > 0 {
		overallAvgTime = totalTime / totalTiles
	}

	// 第二遍：输出详细的层级性能日志
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

		// 计算数据倾斜度 (max_size / avg_size)
		skewness := 0.0
		if avgSizeKB > 0 && stats.maxTileSize > 0 {
			skewness = float64(stats.maxTileSize) / 1024.0 / avgSizeKB
		}

		// 计算空瓦片占比
		emptyRatio := 0.0
		if stats.totalTiles > 0 {
			emptyRatio = float64(stats.emptyTiles) / float64(stats.totalTiles) * 100
		}

		// 检测性能异常（与整体平均相比，下降超过50%）
		bottleneck := false
		if overallAvgTime > 0 && avgTimeMs > overallAvgTime*1.5 {
			bottleneck = true
		}

		// 总大小（MB）
		totalSizeMB := float64(stats.totalSize) / 1024.0 / 1024.0

		// 最大/最小瓦片大小（MB/KB）
		maxSizeMB := float64(stats.maxTileSize) / 1024.0 / 1024.0
		minSizeKB := float64(stats.minTileSize) / 1024.0
		if stats.minTileSize == math.MaxInt64 {
			minSizeKB = 0 // 没有生成的瓦片
		}

		// 详细的层级性能日志，按照计划格式
		logger.L().Info(fmt.Sprintf("z=%d: tiles_estimate=%d, generated=%d, empty=%d, skipped=%d, avg_time_ms=%.1f, avg_size_kb=%.1f, total_size_mb=%.1f, max_size_mb=%.1f, min_size_kb=%.1f, skewness=%.1f, empty_ratio=%.1f%%, extent_reduced=%d, bottleneck=%v",
			zr.zoom,
			stats.totalTiles,
			stats.generatedTiles,
			stats.emptyTiles,
			stats.skippedTiles,
			avgTimeMs,
			avgSizeKB,
			totalSizeMB,
			maxSizeMB,
			minSizeKB,
			skewness,
			emptyRatio,
			stats.extentReducedCount,
			bottleneck),
			"zoom", zr.zoom)

		// 性能诊断警告（异常检测和原因分析）
		if bottleneck && overallAvgTime > 0 {
			// 检测到性能异常
			perfDecrease := ((avgTimeMs - overallAvgTime) / overallAvgTime) * 100
			logger.L().Warn(fmt.Sprintf("⚠️ z=%d 性能异常: avg_time=%.1fms (平均=%.1fms, +%.0f%%)",
				zr.zoom, avgTimeMs, overallAvgTime, perfDecrease))

			// 原因分析
			if skewness > 3.0 {
				logger.L().Warn(fmt.Sprintf("原因分析: avg_tile_size=%.1fKB, max_tile_size=%.1fMB, 数据倾斜度=%.1f (存在明显数据热点)",
					avgSizeKB, maxSizeMB, skewness))
				logger.L().Info("建议: 存在数据热点，后续可考虑增量策略或其他优化")
			} else if stats.extentReducedCount > stats.generatedTiles/10 {
				// Extent减半次数超过生成瓦片数的10%
				extentRatio := float64(stats.extentReducedCount) / float64(stats.generatedTiles) * 100
				logger.L().Warn(fmt.Sprintf("原因分析: Extent减半频繁 (%d次, %.1f%%的瓦片需要减半)",
					stats.extentReducedCount, extentRatio))
				logger.L().Info("建议: 考虑调整Extent初始值或瓦片大小阈值")
			} else {
				logger.L().Info("原因分析: 可能存在数据库查询瓶颈或I/O压力，检查资源监控日志")
			}
		}

		// 数据分布异常检测
		if emptyRatio > 50.0 && stats.generatedTiles > 10 {
			logger.L().Warn(fmt.Sprintf("⚠️ z=%d 空瓦片率过高: %.1f%% (数据稀疏)",
				zr.zoom, emptyRatio))
			logger.L().Info("建议: 数据分布稀疏，可考虑仅生成非空瓦片")
		}

		// Extent减半异常检测
		if stats.extentReducedCount > 0 {
			extentRatio := float64(stats.extentReducedCount) / float64(stats.generatedTiles) * 100
			if extentRatio > 20.0 {
				logger.L().Warn(fmt.Sprintf("⚠️ z=%d Extent减半频繁: %d次 (%.1f%%)",
					zr.zoom, stats.extentReducedCount, extentRatio))
			}
		}

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
		EngineID:         cfg.EngineID,
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
		GenerationSec:    result.GenerationSec,
	}

	if err := s.putMetadata(ctx, cfg.Fingerprint, metadata); err != nil {
		logger.L().Warn("Failed to save metadata to MinIO", "error", err)
	}

	// 更新进度：生成完成
	if progressTracker != nil {
		progressTracker.UpdateProgress(ctx, &QuickViewProgress{
			Status:             "completed",
			CurrentZoom:        result.ActualMaxZoom,
			MaxZoom:            cfg.MaxZoom,
			TilesProcessed:     processedTiles,
			TilesTotalEstimate: totalTaskCount,
			ProgressPercent:    100,
		})
	}

	// ============ 预缓存总结报告 ============
	logger.L().Info("========== 预缓存完成总结 ==========")
	logger.L().Info(fmt.Sprintf("表: %s.%s", cfg.Schema, cfg.Table))
	if stats != nil && stats.TableRows > 0 {
		logger.L().Info(fmt.Sprintf("表行数: %d", stats.TableRows))
	}
	logger.L().Info(fmt.Sprintf("总耗时: %.0f分%.0f秒",
		math.Floor(result.GenerationSec/60), math.Mod(result.GenerationSec, 60)))

	// === 性能分布 ===
	logger.L().Info("=== 性能分布 ===")
	var slowZooms, fastZooms []int
	for _, zr := range zoomRanges {
		zoomStats := statsMap[zr.zoom]
		if zoomStats.generatedTiles > 0 {
			avgTimeMs := zoomStats.totalGenTime / float64(zoomStats.generatedTiles) / 1e6
			if avgTimeMs > 100 {
				slowZooms = append(slowZooms, zr.zoom)
			} else if avgTimeMs < 50 {
				fastZooms = append(fastZooms, zr.zoom)
			}
		}
	}
	if len(slowZooms) > 0 {
		logger.L().Info(fmt.Sprintf("慢速层级: z=%v (avg_time > 100ms)", slowZooms))
	}
	if len(fastZooms) > 0 {
		logger.L().Info(fmt.Sprintf("快速层级: z=%v (avg_time < 50ms)", fastZooms))
	}

	// === 数据分布 ===
	logger.L().Info("=== 数据分布 ===")
	var totalEmpty, totalGenerated int
	var maxSkewness float64
	var totalExtentReduced int
	for _, zr := range zoomRanges {
		zoomStats := statsMap[zr.zoom]
		totalEmpty += zoomStats.emptyTiles
		totalGenerated += zoomStats.generatedTiles
		totalExtentReduced += zoomStats.extentReducedCount

		if zoomStats.generatedTiles > 0 {
			avgSizeKB := float64(zoomStats.totalSize) / float64(zoomStats.generatedTiles) / 1024.0
			if avgSizeKB > 0 && zoomStats.maxTileSize > 0 {
				skewness := float64(zoomStats.maxTileSize) / 1024.0 / avgSizeKB
				if skewness > maxSkewness {
					maxSkewness = skewness
				}
			}
		}
	}

	emptyRatioTotal := 0.0
	if result.TotalTiles > 0 {
		emptyRatioTotal = float64(totalEmpty) / float64(result.TotalTiles) * 100
	}
	logger.L().Info(fmt.Sprintf("空瓦片率: %.1f%% (总体稀疏度)", emptyRatioTotal))
	logger.L().Info(fmt.Sprintf("数据倾斜度: %.1f (最大值, 存在%s数据热点)",
		maxSkewness, func() string {
			if maxSkewness > 5 {
				return "明显"
			} else if maxSkewness > 3 {
				return "较明显"
			}
			return "轻微"
		}()))
	logger.L().Info(fmt.Sprintf("Extent减半发生: %d次 (瓦片超过5MB时)", totalExtentReduced))

	// === 硬件利用 ===
	logger.L().Info("=== 硬件利用 ===")
	resourceInfo := s.getResourceMetrics(ctx, cfg)
	logger.L().Info(fmt.Sprintf("当前DB连接: %d", resourceInfo.DBConnections))
	logger.L().Info(fmt.Sprintf("配置并发数: %d", cfg.Concurrency))

	// === 后续建议 ===
	logger.L().Info("=== 后续建议 ===")
	if resourceInfo.DBConnections < cfg.Concurrency*80/100 {
		logger.L().Info("• DB连接未饱和，如果CPU利用率也较低，可考虑增加并发数")
	}
	if len(slowZooms) > 0 {
		logger.L().Info("• 某些层级持续缓慢，建议分析其数据特征")
	}
	if maxSkewness > 5 {
		logger.L().Info("• 数据倾斜明显，后续可考虑针对热点区域的优化策略")
	}
	if totalExtentReduced > totalGenerated/5 {
		logger.L().Info("• Extent减半频繁，可考虑调整初始Extent值或大小阈值")
	}
	if emptyRatioTotal > 50 {
		logger.L().Info("• 空瓦片率过高，可考虑仅生成非空瓦片的策略")
	}
	logger.L().Info("=====================================")

	logger.L().Info("快显生成完成（混合模式）",
		"total_tiles", result.TotalTiles,
		"cached_tiles", result.CachedTiles,
		"duration_sec", result.GenerationSec,
		"max_zoom", result.ActualMaxZoom)

	return result, nil
}

// processTile 处理单个瓦片
func (s *QuickViewService) processTile(
	ctx context.Context,
	cfg QuickViewConfig,
	coord TileCoord,
) *tileResult {
	startTime := time.Now()

	// 🆕 检查Context是否已被取消（防止启动新的ST_AsMVT查询）
	select {
	case <-ctx.Done():
		logger.L().Info("⚠️  检测到Context取消，放弃启动新的瓦片生成",
			"tile", fmt.Sprintf("z%d/%d_%d", coord.Z, coord.X, coord.Y),
			"fingerprint", cfg.Fingerprint)
		return &tileResult{
			coord: coord,
			err:   fmt.Errorf("context cancelled before tile generation"),
		}
	default:
	}

	// 1. 检查瓦片是否已存在于 MinIO
	objectPath := fmt.Sprintf("mvt-tiles/%s/tiles/z%d/%d_%d.mvt.gz", cfg.Fingerprint, coord.Z, coord.X, coord.Y)
	_, err := s.minioClient.StatObject(ctx, s.bucket, objectPath, minio.StatObjectOptions{})
	if err == nil {
		// 瓦片已存在，跳过生成（用 -1 标记）
		return &tileResult{
			coord:     coord,
			isEmpty:   false,
			genTimeMs: float64(time.Since(startTime).Milliseconds()),
			sizeBytes: -1, // -1 表示跳过（已存在）
		}
	}

	// 2. 瓦片不存在，生成瓦片
	params := TileGenerationParams{
		EngineID:           cfg.EngineID,
		TenantID:           cfg.TenantID,
		Schema:             cfg.Schema,
		Table:              cfg.Table,
		GeomColumn:         cfg.GeomColumn,
		SRID:               cfg.SRID,
		PrimaryKey:         cfg.PrimaryKey,
		Z:                  coord.Z,
		X:                  coord.X,
		Y:                  coord.Y,
		MaxZoom:            cfg.MaxZoom,                 // 传递最大 zoom 级别
		OptimizationConfig: cfg.OptimizationConfig,      // v3.0 传递优化配置
	}

	mvtData, err := s.tileGen.GenerateTile(ctx, params)
	if err != nil {
		return &tileResult{
			coord: coord,
			err:   err,
		}
	}

	// 3. 空瓦片，不存储
	if len(mvtData) == 0 {
		return &tileResult{
			coord:     coord,
			isEmpty:   true,
			genTimeMs: float64(time.Since(startTime).Milliseconds()),
		}
	}

	// 4. 压缩
	gzData, err := gzipCompress(mvtData)
	if err != nil {
		return &tileResult{
			coord: coord,
			err:   fmt.Errorf("gzip compress failed: %w", err),
		}
	}

	// 5. 存储到 MinIO
	err = s.putTile(ctx, cfg.Fingerprint, coord.Z, coord.X, coord.Y, gzData)
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

// putTile 存储瓦片到 MinIO
func (s *QuickViewService) putTile(
	ctx context.Context,
	fingerprint string,
	z, x, y int,
	data []byte,
) error {
	objectPath := fmt.Sprintf("mvt-tiles/%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	_, err := s.minioClient.PutObject(ctx, s.bucket, objectPath, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{
			ContentType: "application/octet-stream",
			UserMetadata: map[string]string{
				"z":          fmt.Sprintf("%d", z),
				"x":          fmt.Sprintf("%d", x),
				"y":          fmt.Sprintf("%d", y),
				"encoding":   "gzip",
				"created_at": time.Now().Format(time.RFC3339),
			},
		})

	if err != nil {
		return fmt.Errorf("failed to put tile to minio: %w", err)
	}

	return nil
}

// putMetadata 保存元数据到 MinIO
func (s *QuickViewService) putMetadata(
	ctx context.Context,
	fingerprint string,
	metadata *QuickViewMetadata,
) error {
	objectPath := fmt.Sprintf("mvt-tiles/%s/metadata.json", fingerprint)

	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = s.minioClient.PutObject(ctx, s.bucket, objectPath,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{
			ContentType: "application/json",
		})

	if err != nil {
		return fmt.Errorf("failed to put metadata to minio: %w", err)
	}

	return nil
}

// GetTile 从 MinIO 获取瓦片
func (s *QuickViewService) GetTile(
	ctx context.Context,
	fingerprint string,
	z, x, y int,
) ([]byte, error) {
	objectPath := fmt.Sprintf("mvt-tiles/%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	obj, err := s.minioClient.GetObject(ctx, s.bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get tile from minio: %w", err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to read tile data: %w", err)
	}

	return data, nil
}

// DeleteTiles 删除指定 fingerprint 的所有瓦片和元数据
func (s *QuickViewService) DeleteTiles(ctx context.Context, fingerprint string) error {
	// 删除瓦片目录：mvt-tiles/{fingerprint}/tiles/
	// 删除元数据文件：mvt-tiles/{fingerprint}/metadata.json

	prefix := fmt.Sprintf("mvt-tiles/%s/", fingerprint)

	// 列出所有对象
	objectsCh := s.minioClient.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	// 删除所有对象
	deletedCount := 0
	for object := range objectsCh {
		if object.Err != nil {
			logger.L().Error("Error listing object for deletion", "error", object.Err, "prefix", prefix)
			continue
		}

		if err := s.minioClient.RemoveObject(ctx, s.bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			logger.L().Error("Failed to delete object", "error", err, "key", object.Key)
			continue
		}

		deletedCount++
	}

	logger.L().Info("Deleted tiles from MinIO",
		"fingerprint", fingerprint,
		"deleted_count", deletedCount)

	return nil
}

// StatisticsInfo 统计信息结构
type StatisticsInfo struct {
	TableRows            int64
	LastAnalyzeAgeHours  int
	NeedsAnalyze         bool
	Bounds               [4]float64 // [minLng, minLat, maxLng, maxLat]
}

// prepareFor3857MVT 准备 3857 物化视图和索引（如果需要）
// 对于非 3857 的空间表，自动创建物化视图和空间索引
func (s *QuickViewService) prepareFor3857MVT(
	ctx context.Context,
	cfg QuickViewConfig,
	actualSRID int,
) error {
	// 1. 判断是否需要物化视图（仅 public.dltb 且 SRID=2360）
	if cfg.Schema != "public" || cfg.Table != "dltb" || actualSRID == 3857 {
		logger.L().Info("⏭️  无需物化视图转换",
			"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
			"srid", actualSRID)
		return nil
	}

	// 2. 获取数据库连接
	conn, err := s.tileGen.GetOrCreateDBPool(ctx, cfg.EngineID, cfg.TenantID)
	if err != nil {
		return fmt.Errorf("failed to get db connection: %w", err)
	}

	mvTable := fmt.Sprintf("%s_3857", cfg.Table)
	mvFullName := fmt.Sprintf("%s.%s", cfg.Schema, mvTable)
	indexName := fmt.Sprintf("idx_%s_geom", mvTable)

	logger.L().Info("🔍 检查物化视图",
		"materialized_view", mvFullName,
		"original_srid", actualSRID,
		"target_srid", 3857)

	// 3. 检查物化视图是否存在
	var mvExists bool
	checkMVSQL := `
		SELECT EXISTS (
			SELECT 1 FROM pg_matviews
			WHERE schemaname = $1 AND matviewname = $2
		)
	`
	if err := conn.QueryRowContext(ctx, checkMVSQL, cfg.Schema, mvTable).Scan(&mvExists); err != nil {
		return fmt.Errorf("failed to check materialized view: %w", err)
	}

	if !mvExists {
		logger.L().Info("🚧 物化视图不存在，开始创建",
			"materialized_view", mvFullName,
			"estimated_time", "可能需要数分钟")

		// 创建物化视图（转换为 3857）
		// ⚠️ 重要：显式指定几何类型和 SRID，确保在 geometry_columns 中正确注册
		createMVSQL := fmt.Sprintf(`
			CREATE MATERIALIZED VIEW %s AS
			SELECT
				id,
				ST_Transform(%s, 3857)::geometry(GEOMETRY, 3857) AS geom_3857
			FROM %s.%s
			WHERE %s IS NOT NULL
		`, mvFullName, cfg.GeomColumn, cfg.Schema, cfg.Table, cfg.GeomColumn)

		if _, err := conn.ExecContext(ctx, createMVSQL); err != nil {
			return fmt.Errorf("failed to create materialized view: %w", err)
		}

		logger.L().Info("✅ 物化视图创建成功（SRID=3857）", "materialized_view", mvFullName)
	} else {
		logger.L().Info("✅ 物化视图已存在", "materialized_view", mvFullName)
	}

	// 4. 检查空间索引是否存在
	var indexExists bool
	checkIndexSQL := `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = $1 AND tablename = $2 AND indexname = $3
		)
	`
	if err := conn.QueryRowContext(ctx, checkIndexSQL, cfg.Schema, mvTable, indexName).Scan(&indexExists); err != nil {
		return fmt.Errorf("failed to check spatial index: %w", err)
	}

	if !indexExists {
		logger.L().Info("🚧 空间索引不存在，开始创建",
			"index", indexName,
			"table", mvFullName,
			"estimated_time", "可能需要数分钟")

		// 创建 GIST 空间索引
		createIndexSQL := fmt.Sprintf(`
			CREATE INDEX %s ON %s USING GIST (geom_3857)
		`, indexName, mvFullName)

		if _, err := conn.ExecContext(ctx, createIndexSQL); err != nil {
			return fmt.Errorf("failed to create spatial index: %w", err)
		}

		logger.L().Info("✅ 空间索引创建成功", "index", indexName)
	} else {
		logger.L().Info("✅ 空间索引已存在", "index", indexName)
	}

	// 5. 执行 ANALYZE 更新统计信息
	logger.L().Info("🔄 更新物化视图统计信息", "table", mvFullName)
	analyzeSQL := fmt.Sprintf("ANALYZE %s", mvFullName)
	if _, err := conn.ExecContext(ctx, analyzeSQL); err != nil {
		logger.L().Warn("⚠️ ANALYZE 执行失败，继续", "error", err)
	} else {
		logger.L().Info("✅ 统计信息更新完成", "table", mvFullName)
	}

	return nil
}

// collectStatistics 采集统计信息（精准化，仅收集MVT需要的）
func (s *QuickViewService) collectStatistics(
	ctx context.Context,
	cfg QuickViewConfig,
) (*StatisticsInfo, error) {
	info := &StatisticsInfo{}

	// 获取表的连接信息
	conn, err := s.tileGen.getOrCreateDBPool(ctx, cfg.EngineID, cfg.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get db connection: %w", err)
	}

	// 1. 检查统计信息年龄
	var lastAnalyze *time.Time
	var nLiveTup int64
	row := conn.QueryRowContext(ctx,
		"SELECT last_analyze, n_live_tup FROM pg_stat_user_tables WHERE schemaname = $1 AND relname = $2",
		cfg.Schema, cfg.Table)
	err = row.Scan(&lastAnalyze, &nLiveTup)
	if err != nil {
		logger.L().Warn("Failed to get table stats", "error", err)
		// 继续，不中断
	} else {
		info.TableRows = nLiveTup
		if lastAnalyze != nil {
			age := time.Since(*lastAnalyze)
			info.LastAnalyzeAgeHours = int(age.Hours())
			if age > 24*time.Hour {
				info.NeedsAnalyze = true
				logger.L().Info("⚠️ 统计信息过旧，建议执行ANALYZE",
					"last_analyze", lastAnalyze.Format(time.RFC3339),
					"age_hours", info.LastAnalyzeAgeHours)
			}
		} else {
			info.NeedsAnalyze = true
			logger.L().Info("⚠️ 统计信息不存在，建议执行ANALYZE")
		}
	}

	// 2. 采集空间bounds（使用物化视图或原始表）
	geomCol := cfg.GeomColumn
	// 检查是否使用物化视图
	table := cfg.Table
	if cfg.Schema == "public" && cfg.Table == "dltb" && cfg.SRID == 2360 {
		table = "dltb_3857"
		geomCol = "geom_3857"
	}

	boundRow := conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT ST_Extent(%s) as bounds FROM %s.%s", geomCol, cfg.Schema, table))
	var boundsWKT *string
	if err := boundRow.Scan(&boundsWKT); err != nil {
		logger.L().Warn("Failed to get spatial bounds", "error", err)
	} else if boundsWKT != nil {
		// 解析 WKT 格式的 bounds
		// 格式: "POLYGON((minLng minLat,maxLng minLat,maxLng maxLat,minLng maxLat,minLng minLat))"
		if bounds, err := parseBoundsFromWKT(*boundsWKT); err == nil {
			info.Bounds = bounds
		} else {
			logger.L().Warn("Failed to parse bounds", "error", err)
		}
	}

	return info, nil
}

// parseBoundsFromWKT 从WKT格式解析bounds
// 格式: "POLYGON((minLng minLat,maxLng minLat,maxLng maxLat,minLng maxLat,minLng minLat))"
func parseBoundsFromWKT(wkt string) ([4]float64, error) {
	// 简单的解析：提取数字
	// 实际应该用更健壮的方式，这里为了演示
	var bounds [4]float64

	// 查找第一个(
	start := bytes.Index([]byte(wkt), []byte("(("))
	if start == -1 {
		return bounds, fmt.Errorf("invalid WKT format")
	}

	// 简化处理：假设格式正确，直接扫描坐标
	// 更好的方式是使用正则或标准库，这里为了快速
	fmt.Sscanf(wkt[start+2:], "%f %f,%f %f", &bounds[0], &bounds[1], &bounds[2], &bounds[3])

	return bounds, nil
}

// ResourceMetrics 资源监控指标
type ResourceMetrics struct {
	CPUPercent     float64
	MemoryMB       float64
	DBConnections  int
	QueryTimeMs    float64
}

// getResourceMetrics 采集资源监控指标
func (s *QuickViewService) getResourceMetrics(ctx context.Context, cfg QuickViewConfig) *ResourceMetrics {
	metrics := &ResourceMetrics{
		CPUPercent:    0,
		MemoryMB:      0,
		DBConnections: 0,
		QueryTimeMs:   0,
	}

	// 获取数据库连接数
	conn, err := s.tileGen.getOrCreateDBPool(ctx, cfg.EngineID, cfg.TenantID)
	if err == nil {
		// 查询活跃连接数
		row := conn.QueryRowContext(ctx,
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND state = 'active'")
		var activeConns int
		if err := row.Scan(&activeConns); err == nil {
			metrics.DBConnections = activeConns
		}
	}

	// TODO: 添加CPU和内存的采集（需要OS层面的调用或通过proc文件系统）
	// 当前版本留作占位符，实际实现时可以使用:
	// - runtime.ReadMemStats() 获取Go内存使用
	// - 通过shell命令或专门库获取CPU使用率

	return metrics
}

// isAlreadyMaterializedView 检查表名是否已经是物化视图格式 (xxx_mv3857)
func isAlreadyMaterializedView(tableName string) bool {
	return strings.HasSuffix(tableName, "_mv3857")
}

