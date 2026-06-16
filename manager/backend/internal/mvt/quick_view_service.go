package mvt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/tilecache"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// QuickViewService 快显服务
// 负责批量生成 MVT 瓦片并存储到 MinIO
type QuickViewService struct {
	tileGen     *TileGenerator
	minioClient *minio.Client
	bucket      string
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
	Extent             []float64 // [minLng, minLat, maxLng, maxLat]
	ExtentSRID         int
	MinZoom            int // 自动计算或指定
	MaxZoom            int
	Concurrency        int
	Fingerprint        string
	StorageRef         string
	OptimizationConfig *commonModels.OptimizationConfig // v2.0 优化配置
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
func NewQuickViewService(tileGen *TileGenerator, minioCfg MinIOConfig) (*QuickViewService, error) {
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
	}, nil
}

// GenerateResult 快显生成结果
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
	LastZoomAvgTimeMs  float64
	LastZoomAvgSizeKB  float64
	StopReason         string
	GenerationSec      float64
	ExtentWGS84        []float64
}

// tileResult 瓦片处理结果
type tileResult struct {
	coord     TileCoord
	isEmpty   bool
	genTimeMs float64
	sizeBytes int64
	oversized bool
	err       error
}

type zoomRange struct {
	zoom                   int
	minX, minY, maxX, maxY int
	totalTiles             int
}

type zoomGenerationStats struct {
	sync.Mutex
	totalTiles         int
	generatedTiles     int
	emptyTiles         int
	skippedTiles       int
	oversizedTiles     int
	failedTiles        int
	totalGenTime       float64
	totalSize          int64
	maxTileSize        int64
	minTileSize        int64
	extentReducedCount int
}

func newZoomGenerationStats(totalTiles int) *zoomGenerationStats {
	return &zoomGenerationStats{
		totalTiles:  totalTiles,
		minTileSize: math.MaxInt64,
	}
}

func (s *zoomGenerationStats) applyTileResult(result *tileResult) {
	s.Lock()
	defer s.Unlock()

	if result.err != nil {
		s.failedTiles++
		return
	}
	if result.sizeBytes > 0 {
		s.generatedTiles++
		s.totalGenTime += result.genTimeMs * 1e6 // 转换为纳秒
		s.totalSize += result.sizeBytes
		if result.sizeBytes > s.maxTileSize {
			s.maxTileSize = result.sizeBytes
		}
		if result.sizeBytes < s.minTileSize {
			s.minTileSize = result.sizeBytes
		}
		return
	}
	if result.sizeBytes == -1 {
		s.skippedTiles++
		return
	}
	if result.oversized {
		s.skippedTiles++
		s.oversizedTiles++
		return
	}
	if result.isEmpty {
		s.emptyTiles++
	}
}

type spatialExtentLoader func(context.Context, uint, uint, string, string, string) ([]float64, error)

func quickViewTableName(cfg QuickViewConfig) string {
	return fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table)
}

func tileCoordName(coord TileCoord) string {
	return fmt.Sprintf("z%d/%d/%d", coord.Z, coord.X, coord.Y)
}

func calculateZoomRanges(tileRangeExtent []float64, minZoom, maxZoom int) ([]zoomRange, int) {
	zoomRanges := make([]zoomRange, 0, maxZoom-minZoom+1)
	totalTaskCount := 0
	for z := minZoom; z <= maxZoom; z++ {
		minX, minY, maxX, maxY := calculateTileBounds(tileRangeExtent, z)
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
	}
	return zoomRanges, totalTaskCount
}

func resolveTileRangeExtentWGS84(ctx context.Context, cfg QuickViewConfig, loadExtent spatialExtentLoader) ([]float64, error) {
	if cfg.ExtentSRID == spatial.SRIDWGS84 {
		if len(cfg.Extent) != 4 {
			return nil, fmt.Errorf("tile cache task extent is invalid")
		}
		return append([]float64(nil), cfg.Extent...), nil
	}
	if cfg.ExtentSRID <= 0 {
		return nil, fmt.Errorf("tile cache task extent_srid is required")
	}
	if loadExtent == nil {
		return nil, fmt.Errorf("tile cache task extent_srid %d requires PostGIS extent transform", cfg.ExtentSRID)
	}
	extent, err := loadExtent(ctx, cfg.EngineID, cfg.TenantID, cfg.Schema, cfg.Table, cfg.GeomColumn)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve WGS84 tile range extent: %w", err)
	}
	if len(extent) != 4 {
		return nil, fmt.Errorf("resolved WGS84 tile range extent is invalid")
	}
	return extent, nil
}

func startMixedGenerationProgressMonitor(
	ctx context.Context,
	progressTracker ProgressSink,
	cfg QuickViewConfig,
	totalTaskCount int,
	processedTilesCount *int32,
	currentZoom *int32,
) func() {
	if progressTracker == nil {
		return func() {}
	}

	done := make(chan struct{})
	var stopOnce sync.Once
	logger.L().Info("启动进度监控 goroutine", "interval_sec", 2)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				processedCount := int(atomic.LoadInt32(processedTilesCount))
				currentZ := int(atomic.LoadInt32(currentZoom))

				progressPercent := 0.0
				if totalTaskCount > 0 {
					progressPercent = float64(processedCount) / float64(totalTaskCount) * 100
				}

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
					logger.L().Info("进度监控定时更新",
						"processed", processedCount,
						"total", totalTaskCount,
						"percent", fmt.Sprintf("%.1f%%", progressPercent))
				}

			case <-done:
				logger.L().Info("进度监控 goroutine 已停止")
				return
			case <-ctx.Done():
				logger.L().Info("进度监控 goroutine 被取消")
				return
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			close(done)
		})
	}
}

// GenerateMixed 混合入队模式的快显缓存生成
// 与串行Generate不同，该方法会同时处理多个层级的瓦片，充分利用并发
func (s *QuickViewService) GenerateMixed(
	ctx context.Context,
	cfg QuickViewConfig,
	progressTracker ProgressSink, // 可选的进度跟踪器（nil 时不更新进度）
) (*GenerateResult, error) {
	startTime := time.Now()
	tableName := quickViewTableName(cfg)

	logger.L().Info("开始生成快显缓存（混合入队模式）",
		"engine_id", cfg.EngineID,
		"table", tableName,
		"geom_column", cfg.GeomColumn,
		"srid", cfg.SRID,
		"min_zoom", cfg.MinZoom,
		"max_zoom", cfg.MaxZoom,
		"concurrency", cfg.Concurrency)

	if strings.TrimSpace(cfg.StorageRef) == "" {
		return nil, fmt.Errorf("tile cache storage_ref is required")
	}
	_, objectPrefix, err := tilecache.ObjectPrefix(cfg.StorageRef, s.bucket)
	if err != nil {
		return nil, err
	}

	// 1. 读取生成目标真实 SRID。MVT 目标坐标系固定为 3857，非 3857 目标才需要 SQL 内 ST_Transform。
	actualSRID, err := s.tileGen.QuerySourceSRID(ctx, cfg.EngineID, cfg.TenantID, cfg.Schema, cfg.Table, cfg.GeomColumn)
	if err != nil {
		logger.L().Error("瓦片生成目标 SRID 解析失败",
			"table", tableName,
			"error", err)
		return nil, fmt.Errorf("瓦片生成目标 SRID 解析失败: %w", err)
	}
	cfg.SRID = actualSRID

	logger.L().Info("瓦片生成目标 SRID 已解析，MVT 将输出 3857",
		"table", tableName,
		"target_source_srid", actualSRID,
		"target_srid", 3857)

	tileRangeExtent, err := resolveTileRangeExtentWGS84(ctx, cfg, s.tileGen.GetSpatialExtent)
	if err != nil {
		return nil, err
	}
	logger.L().Info("瓦片行列计算范围已解析为 WGS84",
		"source_extent", fmt.Sprintf("%v", cfg.Extent),
		"extent_srid", cfg.ExtentSRID,
		"tile_range_extent", fmt.Sprintf("%v", tileRangeExtent))

	if actualSRID != 3857 {
		logger.L().Info("瓦片生成目标不是 3857，生成 SQL 将在 PostgreSQL 内执行 ST_Transform",
			"table", tableName,
			"target_source_srid", actualSRID,
			"target_srid", 3857)
	} else {
		logger.L().Info("瓦片生成目标已是 3857 坐标系，可直接生成 MVT",
			"srid", actualSRID)
	}

	// 1.5 精准统计信息采集（Phase 1诊断）
	stats, err := s.collectStatistics(ctx, cfg)
	if err != nil {
		logger.L().Warn("统计信息采集失败，继续生成",
			"table", tableName,
			"error", err)
	} else {
		// 记录统计信息摘要
		logger.L().Info("统计信息采集完成",
			"table_rows", stats.TableRows,
			"bounds", fmt.Sprintf("[%.2f,%.2f,%.2f,%.2f]", stats.Bounds[0], stats.Bounds[1], stats.Bounds[2], stats.Bounds[3]),
			"last_analyze_age_hours", stats.LastAnalyzeAgeHours,
			"needs_analyze", stats.NeedsAnalyze)
	}

	// 2. 预先计算所有层级的瓦片范围
	zoomRanges, totalTaskCount := calculateZoomRanges(tileRangeExtent, cfg.MinZoom, cfg.MaxZoom)
	for _, zr := range zoomRanges {
		logger.L().Info("计算层级范围", "zoom", zr.zoom, "tiles", zr.totalTiles,
			"range", fmt.Sprintf("x[%d-%d] y[%d-%d]", zr.minX, zr.maxX, zr.minY, zr.maxY))
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
	var processedTilesCount int32 // 原子计数器
	var currentZoom int32 = int32(cfg.MinZoom)
	stopProgressMonitor := startMixedGenerationProgressMonitor(ctx, progressTracker, cfg, totalTaskCount, &processedTilesCount, &currentZoom)

	// 4. 每层统计数据（线程安全）
	statsMap := make(map[int]*zoomGenerationStats)
	for _, zr := range zoomRanges {
		statsMap[zr.zoom] = newZoomGenerationStats(zr.totalTiles)
	}

	// 5. 启动并发执行器池
	logger.L().Info("MVT executor pool 启动",
		"table", tableName,
		"concurrency", cfg.Concurrency,
		"executor_count", cfg.Concurrency,
		"total_tasks", totalTaskCount)

	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		executorID := i // 捕获循环变量
		logger.L().Debug("MVT executor 启动",
			"executor_id", executorID,
			"total_executors", cfg.Concurrency)
		go func(id int) {
			defer wg.Done()
			logger.L().Debug("Executor 开始处理任务", "executor_id", id)
			tilesProcessed := 0
			for coord := range jobs {
				// 检查 Context 是否被取消
				select {
				case <-ctx.Done():
					logger.L().Info("MVT executor 检测到 Context 取消，停止处理新瓦片",
						"executor_id", id,
						"tiles_processed", tilesProcessed)
					return
				default:
				}

				result := s.processTile(ctx, cfg, coord)
				results <- result
				tilesProcessed++
			}
			logger.L().Info("Executor 完成所有任务", "executor_id", id, "tiles_processed", tilesProcessed)
		}(executorID)
	}

	logger.L().Info("MVT executor pool 已全部启动",
		"active_executors", cfg.Concurrency)

	// 6. 流式任务分发器（在单独的goroutine中）
	go func() {
		defer close(jobs)

		logger.L().Info("开始流式分发 MVT 瓦片任务",
			"total_tasks", totalTaskCount,
			"min_zoom", cfg.MinZoom,
			"max_zoom", cfg.MaxZoom,
			"concurrency", cfg.Concurrency)

		// 先打印所有层级的队列计划
		logger.L().Info("瓦片缓存生成队列计划")
		for _, zr := range zoomRanges {
			logger.L().Info("瓦片缓存生成层级计划",
				"zoom", zr.zoom,
				"tiles", zr.totalTiles,
				"min_x", zr.minX,
				"max_x", zr.maxX,
				"min_y", zr.minY,
				"max_y", zr.maxY)
		}

		dispatched := 0
		dispatchedPerZoom := make(map[int]int) // 记录每层实际分发的任务数

		for _, zr := range zoomRanges {
			logger.L().Info("开始分发 MVT 层级任务",
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
							logger.L().Debug("MVT 瓦片任务已入队",
								"tile", tileCoordName(TileCoord{Z: zr.zoom, X: x, Y: y}))
						}

						if dispatched%100 == 0 {
							progress := float64(dispatched) / float64(totalTaskCount) * 100
							logger.L().Info("MVT 瓦片任务分发进度",
								"dispatched", dispatched,
								"total", totalTaskCount,
								"progress", fmt.Sprintf("%.1f%%", progress),
								"current_zoom", zr.zoom)
						}

					case <-ctx.Done():
						logger.L().Warn("MVT 瓦片任务分发被中断",
							"dispatched", dispatched,
							"total", totalTaskCount,
							"stopped_at_zoom", zr.zoom)
						return
					}
				}
			}

			actualDispatched := dispatched - zoomStartDispatched
			dispatchedPerZoom[zr.zoom] = actualDispatched

			logger.L().Info("MVT 层级任务分发完成",
				"zoom", zr.zoom,
				"expected", zr.totalTiles,
				"actual_dispatched", actualDispatched,
				"match", actualDispatched == zr.totalTiles)
		}

		// 最终汇总
		logger.L().Info("MVT 瓦片任务分发完成",
			"total_dispatched", dispatched,
			"expected_total", totalTaskCount,
			"match", dispatched == totalTaskCount)

		// 打印每层分发统计
		logger.L().Info("MVT 各层级分发统计")
		for _, zr := range zoomRanges {
			actual := dispatchedPerZoom[zr.zoom]
			logger.L().Info("MVT 层级分发统计",
				"zoom", zr.zoom,
				"actual", actual,
				"expected", zr.totalTiles,
				"missing", zr.totalTiles-actual,
				"match", actual == zr.totalTiles)
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
			logger.L().Warn("瓦片生成被 context 取消",
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
			}
			stopProgressMonitor()

			// 返回取消错误
			return nil, context.Canceled
		default:
			// 继续处理
		}

		statsMap[result.coord.Z].applyTileResult(result)

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
			logger.L().Info("MVT 瓦片生成进度采样",
				"zoom", result.coord.Z,
				"processed", processedTiles,
				"total_estimated", totalTaskCount,
				"cpu_percent", resourceInfo.CPUPercent,
				"memory_mb", resourceInfo.MemoryMB,
				"db_connections", resourceInfo.DBConnections,
				"query_time_ms", resourceInfo.QueryTimeMs)
		}
	}

	logger.L().Info("所有瓦片处理完成",
		"total_processed", processedTiles,
		"expected", totalTaskCount)

	// 停止进度监控 goroutine
	stopProgressMonitor()

	// 8. 汇总结果与详细日志
	result := &GenerateResult{
		TilesTotalEstimate: totalTaskCount,
		TilesProcessed:     processedTiles,
		ZoomLevels:         make(map[string]ZoomLevelStats),
		MinTileSizeBytes:   math.MaxInt64,
	}
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

		result.TotalTiles += stats.generatedTiles + stats.emptyTiles + stats.skippedTiles + stats.failedTiles
		result.CachedTiles += stats.generatedTiles
		result.GeneratedTiles += stats.generatedTiles
		result.EmptyTiles += stats.emptyTiles
		result.SkippedTiles += stats.skippedTiles
		result.OversizedTiles += stats.oversizedTiles
		result.FailedTiles += stats.failedTiles
		result.TotalSizeBytes += stats.totalSize
		if stats.maxTileSize > result.MaxTileSizeBytes {
			result.MaxTileSizeBytes = stats.maxTileSize
		}
		if stats.minTileSize < result.MinTileSizeBytes {
			result.MinTileSizeBytes = stats.minTileSize
		}

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
		minSizeBytes := stats.minTileSize
		if minSizeBytes == math.MaxInt64 {
			minSizeBytes = 0
		}
		result.ZoomLevels[fmt.Sprintf("%d", zr.zoom)] = ZoomLevelStats{
			Zoom:           zr.zoom,
			TotalTiles:     stats.totalTiles,
			GeneratedTiles: stats.generatedTiles,
			EmptyTiles:     stats.emptyTiles,
			SkippedTiles:   stats.skippedTiles,
			OversizedTiles: stats.oversizedTiles,
			FailedTiles:    stats.failedTiles,
			AvgGenTimeMs:   avgTimeMs,
			AvgSizeKB:      avgSizeKB,
			TotalSizeBytes: stats.totalSize,
			MaxSizeBytes:   stats.maxTileSize,
			MinSizeBytes:   minSizeBytes,
		}

		logger.L().Info("MVT 层级生成统计",
			"zoom", zr.zoom,
			"tiles_estimate", stats.totalTiles,
			"generated", stats.generatedTiles,
			"empty", stats.emptyTiles,
			"skipped", stats.skippedTiles,
			"oversized", stats.oversizedTiles,
			"failed", stats.failedTiles,
			"avg_time_ms", avgTimeMs,
			"avg_size_kb", avgSizeKB,
			"total_size_mb", totalSizeMB,
			"max_size_mb", maxSizeMB,
			"min_size_kb", minSizeKB,
			"skewness", skewness,
			"empty_ratio", emptyRatio,
			"extent_reduced", stats.extentReducedCount,
			"bottleneck", bottleneck)

		// 性能诊断警告（异常检测和原因分析）
		if bottleneck && overallAvgTime > 0 {
			// 检测到性能异常
			perfDecrease := ((avgTimeMs - overallAvgTime) / overallAvgTime) * 100
			logger.L().Warn("MVT 层级生成性能异常",
				"zoom", zr.zoom,
				"avg_time_ms", avgTimeMs,
				"overall_avg_time_ms", overallAvgTime,
				"perf_decrease_percent", perfDecrease)

			// 原因分析
			if skewness > 3.0 {
				logger.L().Warn("MVT 层级可能存在数据热点",
					"zoom", zr.zoom,
					"avg_tile_size_kb", avgSizeKB,
					"max_tile_size_mb", maxSizeMB,
					"skewness", skewness)
			} else if stats.extentReducedCount > stats.generatedTiles/10 {
				// Extent减半次数超过生成瓦片数的10%
				extentRatio := float64(stats.extentReducedCount) / float64(stats.generatedTiles) * 100
				logger.L().Warn("MVT 层级 extent 自适应降级频繁",
					"zoom", zr.zoom,
					"extent_reduced", stats.extentReducedCount,
					"extent_reduced_ratio", extentRatio)
			} else {
				logger.L().Info("MVT 层级性能异常原因待进一步分析", "zoom", zr.zoom)
			}
		}

		// 数据分布异常检测
		if emptyRatio > 50.0 && stats.generatedTiles > 10 {
			logger.L().Warn("MVT 层级空瓦片率过高",
				"zoom", zr.zoom,
				"empty_ratio", emptyRatio)
		}

		// Extent减半异常检测
		if stats.extentReducedCount > 0 {
			extentRatio := float64(stats.extentReducedCount) / float64(stats.generatedTiles) * 100
			if extentRatio > 20.0 {
				logger.L().Warn("MVT 层级 extent 自适应降级比例过高",
					"zoom", zr.zoom,
					"extent_reduced", stats.extentReducedCount,
					"extent_reduced_ratio", extentRatio)
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
	result.ExtentWGS84 = append([]float64(nil), tileRangeExtent...)
	if result.MinTileSizeBytes == math.MaxInt64 {
		result.MinTileSizeBytes = 0
	}

	if err := s.putMetadata(ctx, cfg.TenantID, cfg.StorageRef, buildQuickViewMetadata(cfg, result, objectPrefix, stats)); err != nil {
		logger.L().Warn("Failed to save metadata to MinIO",
			"tenant_id", cfg.TenantID,
			"error", err)
	}

	// 更新进度：生成完成
	if progressTracker != nil {
		progressTracker.UpdateProgress(ctx, &QuickViewProgress{
			Status:             "ready",
			CurrentZoom:        result.ActualMaxZoom,
			MaxZoom:            cfg.MaxZoom,
			TilesProcessed:     processedTiles,
			TilesTotalEstimate: totalTaskCount,
			ProgressPercent:    100,
		})
	}

	tableRows := int64(0)
	if stats != nil {
		tableRows = stats.TableRows
	}
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
		logger.L().Info("MVT 慢速层级统计", "zooms", slowZooms, "threshold_ms", 100)
	}
	if len(fastZooms) > 0 {
		logger.L().Info("MVT 快速层级统计", "zooms", fastZooms, "threshold_ms", 50)
	}

	var totalEmpty int
	var maxSkewness float64
	var totalExtentReduced int
	for _, zr := range zoomRanges {
		zoomStats := statsMap[zr.zoom]
		totalEmpty += zoomStats.emptyTiles
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

	resourceInfo := s.getResourceMetrics(ctx, cfg)
	logger.L().Info("快显生成完成",
		"table", tableName,
		"table_rows", tableRows,
		"total_tiles", result.TotalTiles,
		"cached_tiles", result.CachedTiles,
		"generated_tiles", result.GeneratedTiles,
		"empty_tiles", result.EmptyTiles,
		"skipped_tiles", result.SkippedTiles,
		"oversized_tiles", result.OversizedTiles,
		"failed_tiles", result.FailedTiles,
		"duration_sec", result.GenerationSec,
		"max_zoom", result.ActualMaxZoom,
		"empty_ratio", emptyRatioTotal,
		"max_skewness", maxSkewness,
		"extent_reduced", totalExtentReduced,
		"slow_zooms", slowZooms,
		"fast_zooms", fastZooms,
		"db_connections", resourceInfo.DBConnections,
		"concurrency", cfg.Concurrency)

	return result, nil
}

// processTile 处理单个瓦片
func (s *QuickViewService) processTile(
	ctx context.Context,
	cfg QuickViewConfig,
	coord TileCoord,
) *tileResult {
	startTime := time.Now()

	// 检查 Context 是否已被取消，避免启动新的 ST_AsMVT 查询。
	select {
	case <-ctx.Done():
		logger.L().Info("检测到 Context 取消，放弃启动新的瓦片生成",
			"tile", tileCoordName(coord),
			"fingerprint", cfg.Fingerprint)
		return &tileResult{
			coord: coord,
			err:   fmt.Errorf("context cancelled before tile generation"),
		}
	default:
	}

	// 1. 检查瓦片是否已存在于对象存储（租户隔离）
	bucket, objectPath, err := tilecache.TileObjectLocation(cfg.StorageRef, s.bucket, coord.Z, coord.X, coord.Y)
	if err != nil {
		return &tileResult{coord: coord, err: err}
	}
	_, err = s.minioClient.StatObject(ctx, bucket, objectPath, minio.StatObjectOptions{})
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
	mvtData, err := s.tileGen.GenerateTile(ctx, TileGenerationSourceFromQuickViewConfig(cfg).Params(coord))
	if err != nil {
		if errors.Is(err, ErrMVTTileOversized) {
			return &tileResult{
				coord:     coord,
				oversized: true,
				genTimeMs: float64(time.Since(startTime).Milliseconds()),
			}
		}
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

	// 5. 存储到对象存储（租户隔离）
	err = s.putTile(ctx, cfg, coord, gzData)
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

// putTile 存储瓦片到对象存储（租户隔离）
func (s *QuickViewService) putTile(
	ctx context.Context,
	cfg QuickViewConfig,
	coord TileCoord,
	data []byte,
) error {
	bucket, objectPath, err := tilecache.TileObjectLocation(cfg.StorageRef, s.bucket, coord.Z, coord.X, coord.Y)
	if err != nil {
		return err
	}

	_, err = s.minioClient.PutObject(ctx, bucket, objectPath, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{
			ContentType: "application/octet-stream",
			UserMetadata: map[string]string{
				"tenant_id":  fmt.Sprintf("%d", cfg.TenantID),
				"z":          fmt.Sprintf("%d", coord.Z),
				"x":          fmt.Sprintf("%d", coord.X),
				"y":          fmt.Sprintf("%d", coord.Y),
				"encoding":   "gzip",
				"created_at": time.Now().Format(time.RFC3339),
			},
		})

	if err != nil {
		return fmt.Errorf("failed to put tile to minio: %w", err)
	}

	return nil
}

// putMetadata 保存元数据到对象存储（租户隔离）
func (s *QuickViewService) putMetadata(
	ctx context.Context,
	tenantID uint,
	storageRef string,
	metadata *QuickViewMetadata,
) error {
	bucket, objectPath, err := tilecache.ManifestObjectLocation(storageRef, s.bucket)
	if err != nil {
		return err
	}

	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = s.minioClient.PutObject(ctx, bucket, objectPath,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{
			ContentType: "application/json",
			UserMetadata: map[string]string{
				"tenant_id": fmt.Sprintf("%d", tenantID),
			},
		})

	if err != nil {
		return fmt.Errorf("failed to put metadata to minio: %w", err)
	}

	return nil
}

func buildQuickViewMetadata(cfg QuickViewConfig, result *GenerateResult, objectPrefix string, stats *StatisticsInfo) *QuickViewMetadata {
	return &QuickViewMetadata{
		EngineID:         cfg.EngineID,
		Fingerprint:      cfg.Fingerprint,
		TileFormat:       "mvt",
		StorageRef:       cfg.StorageRef,
		ObjectPrefix:     objectPrefix,
		TableName:        cfg.Table,
		Schema:           cfg.Schema,
		Extent:           result.ExtentWGS84,
		SRID:             spatial.SRIDWGS84,
		MinZoom:          cfg.MinZoom,
		RowCount:         statisticsRowCount(stats),
		GeometryTypes:    []string{},
		ZoomLevels:       result.ZoomLevels,
		MaxZoomGenerated: result.ActualMaxZoom,
		StopReason:       result.StopReason,
		TotalTiles:       result.CachedTiles,
		TotalSizeBytes:   result.TotalSizeBytes,
		CreatedAt:        time.Now(),
		GenerationSec:    result.GenerationSec,
	}
}

func statisticsRowCount(stats *StatisticsInfo) int64 {
	if stats == nil {
		return 0
	}
	return stats.TableRows
}

func (s *QuickViewService) DeleteByStorageRef(ctx context.Context, storageRef string) error {
	bucket, prefix, err := tilecache.ObjectPrefix(storageRef, s.bucket)
	if err != nil {
		return err
	}
	objectsCh := s.minioClient.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix + "/",
		Recursive: true,
	})

	deletedCount := 0
	for object := range objectsCh {
		if object.Err != nil {
			return fmt.Errorf("failed to list tile cache object %q: %w", prefix, object.Err)
		}
		if err := s.minioClient.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return fmt.Errorf("failed to delete tile cache object %q: %w", object.Key, err)
		}
		deletedCount++
	}

	logger.L().Info("瓦片缓存对象已删除",
		"bucket", bucket,
		"prefix", prefix,
		"deleted_count", deletedCount)
	return nil
}

// StatisticsInfo 统计信息结构
type StatisticsInfo struct {
	TableRows           int64
	LastAnalyzeAgeHours int
	NeedsAnalyze        bool
	Bounds              [4]float64 // [minLng, minLat, maxLng, maxLat]
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
				logger.L().Info("统计信息过旧，建议执行 ANALYZE",
					"last_analyze", lastAnalyze.Format(time.RFC3339),
					"age_hours", info.LastAnalyzeAgeHours)
			}
		} else {
			info.NeedsAnalyze = true
			logger.L().Info("统计信息不存在，建议执行 ANALYZE")
		}
	}

	// 2. 采集空间 bounds（上游已决定使用源表或通用 3857 物化视图）
	geomCol := cfg.GeomColumn
	table := cfg.Table

	boundRow := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT ST_Extent(%s) as bounds FROM %s",
		spatial.QuotePostGISIdentifier(geomCol),
		spatial.QualifiedPostGISTable(cfg.Schema, table),
	))
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
	CPUPercent    float64
	MemoryMB      float64
	DBConnections int
	QueryTimeMs   float64
}

// getResourceMetrics 采集资源监控指标
func (s *QuickViewService) getResourceMetrics(ctx context.Context, cfg QuickViewConfig) *ResourceMetrics {
	metrics := &ResourceMetrics{
		CPUPercent:    0,
		MemoryMB:      0,
		DBConnections: 0,
		QueryTimeMs:   0,
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	metrics.MemoryMB = float64(memStats.Alloc) / 1024.0 / 1024.0

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

	return metrics
}
