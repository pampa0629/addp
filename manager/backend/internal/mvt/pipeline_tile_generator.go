package mvt

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/addp/common/logger"
)

// PipelineTileGenerator 流水线并行瓦片生成器
// 实现跨层级的流水线并行：当前层有空闲 worker 时，立即启动下一层任务
type PipelineTileGenerator struct {
	tileGen     *TileGenerator
	minioClient MinIOUploader
	bucket      string
}

// MinIOUploader MinIO 上传接口（便于测试）
type MinIOUploader interface {
	PutTile(ctx context.Context, fingerprint string, z, x, y int, data []byte) error
}

// NewPipelineTileGenerator 创建流水线瓦片生成器
func NewPipelineTileGenerator(tileGen *TileGenerator, uploader MinIOUploader, bucket string) *PipelineTileGenerator {
	return &PipelineTileGenerator{
		tileGen:     tileGen,
		minioClient: uploader,
		bucket:      bucket,
	}
}

// TileTask 瓦片任务
type TileTask struct {
	Coord    TileCoord
	Priority int // zoom 层级（低 zoom 优先）
	Index    int // 堆索引
}

// TileTaskHeap 优先级队列（最小堆，按 zoom 层级排序）
type TileTaskHeap []*TileTask

func (h TileTaskHeap) Len() int           { return len(h) }
func (h TileTaskHeap) Less(i, j int) bool { return h[i].Priority < h[j].Priority }
func (h TileTaskHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].Index = i
	h[j].Index = j
}

func (h *TileTaskHeap) Push(x interface{}) {
	n := len(*h)
	task := x.(*TileTask)
	task.Index = n
	*h = append(*h, task)
}

func (h *TileTaskHeap) Pop() interface{} {
	old := *h
	n := len(old)
	task := old[n-1]
	old[n-1] = nil
	task.Index = -1
	*h = old[0 : n-1]
	return task
}

// PipelineGenerateConfig 流水线生成配置
type PipelineGenerateConfig struct {
	QuickViewConfig              // 继承原有配置
	IdleThreshold   int          // 空闲 worker 数量阈值（触发下一层级）
	MaxPendingTasks int          // 最大待处理任务数（避免内存爆炸）
}

// PipelineGenerateResult 流水线生成结果
type PipelineGenerateResult struct {
	GenerateResult              // 继承原有结果
	PipelineEfficiency float64 // 流水线效率（实际并行度 / 理论最大并行度）
}

// GeneratePipeline 使用流水线并行模式生成瓦片
func (g *PipelineTileGenerator) GeneratePipeline(
	ctx context.Context,
	cfg PipelineGenerateConfig,
) (*PipelineGenerateResult, error) {
	startTime := time.Now()

	// 设置默认值
	if cfg.IdleThreshold == 0 {
		cfg.IdleThreshold = cfg.Concurrency / 2 // 默认 50% 空闲时启动下一层
	}
	if cfg.MaxPendingTasks == 0 {
		cfg.MaxPendingTasks = cfg.Concurrency * 10 // 默认最多积压 10 倍并发数
	}

	logger.L().Info("开始流水线并行生成",
		"resource_id", cfg.ResourceID,
		"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
		"min_zoom", cfg.MinZoom,
		"max_zoom", cfg.MaxZoom,
		"concurrency", cfg.Concurrency,
		"idle_threshold", cfg.IdleThreshold)

	// 初始化优先级队列
	taskQueue := &TileTaskHeap{}
	heap.Init(taskQueue)

	// 初始化结果收集
	results := make(chan *tileResult, cfg.Concurrency*2)
	var totalTiles, cachedTiles int32
	var lastStats *ZoomLevelStats
	zoomStats := make(map[int]*ZoomLevelStats)

	// 当前生成层级和任务投递状态
	currentZoom := cfg.MinZoom
	var queueMu sync.Mutex

	// Worker 状态跟踪
	var activeWorkers int32 // 正在工作的 worker 数量
	var totalTasksSubmitted int32

	// 启动 Worker Pool（固定大小，跨层级共享）
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	workerDone := make(chan struct{}, cfg.Concurrency)

	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			defer func() { workerDone <- struct{}{} }()

			for {
				// 1. 从优先级队列获取任务
				queueMu.Lock()
				if taskQueue.Len() == 0 {
					queueMu.Unlock()
					// 队列为空，等待新任务或退出信号
					select {
					case <-ctx.Done():
						return
					case <-time.After(50 * time.Millisecond):
						continue
					}
				}

				task := heap.Pop(taskQueue).(*TileTask)
				queueMu.Unlock()

				// 2. 执行任务
				atomic.AddInt32(&activeWorkers, 1)
				result := g.processTileWithUpload(ctx, cfg.QuickViewConfig, task.Coord)
				atomic.AddInt32(&activeWorkers, -1)

				// 3. 发送结果
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	// 启动任务提交协程（动态投递任务）
	go func() {
		for currentZoom <= cfg.MaxZoom {
			// 检查是否应该启动下一层
			queueMu.Lock()
			pendingTasks := taskQueue.Len()
			queueMu.Unlock()

			idleWorkers := cfg.Concurrency - int(atomic.LoadInt32(&activeWorkers))

			// 条件：空闲 worker 超过阈值 且 待处理任务不超过上限
			if idleWorkers >= cfg.IdleThreshold && pendingTasks < cfg.MaxPendingTasks {
				// 投递当前层级的任务
				tasks := g.generateTasksForZoom(cfg.QuickViewConfig, currentZoom)

				queueMu.Lock()
				for _, task := range tasks {
					heap.Push(taskQueue, task)
				}
				queueMu.Unlock()

				atomic.AddInt32(&totalTasksSubmitted, int32(len(tasks)))

				logger.L().Info("投递新层级任务",
					"zoom", currentZoom,
					"task_count", len(tasks),
					"pending_tasks", pendingTasks,
					"idle_workers", idleWorkers)

				// 移动到下一层
				currentZoom++
			} else {
				// 等待 worker 空闲
				time.Sleep(100 * time.Millisecond)
			}

			// 检查是否所有任务已完成
			if currentZoom > cfg.MaxZoom {
				queueMu.Lock()
				allDone := taskQueue.Len() == 0 && atomic.LoadInt32(&activeWorkers) == 0
				queueMu.Unlock()

				if allDone {
					break
				}
			}
		}

		// 等待所有任务完成
		for {
			queueMu.Lock()
			pendingTasks := taskQueue.Len()
			queueMu.Unlock()
			activeCount := atomic.LoadInt32(&activeWorkers)

			if pendingTasks == 0 && activeCount == 0 {
				break
			}

			time.Sleep(100 * time.Millisecond)
		}

		cancel() // 通知 worker 退出
	}()

	// 收集结果
	go func() {
		wg.Wait()
		close(results)
	}()

	// 统计结果
	for result := range results {
		z := result.coord.Z

		if _, exists := zoomStats[z]; !exists {
			zoomStats[z] = &ZoomLevelStats{Zoom: z}
		}

		stats := zoomStats[z]
		stats.TotalTiles++

		if result.err != nil {
			logger.L().Error("Failed to process tile",
				"z", z,
				"x", result.coord.X,
				"y", result.coord.Y,
				"error", result.err)
			continue
		}

		if result.isEmpty {
			stats.EmptyTiles++
		} else {
			stats.GeneratedTiles++
			atomic.AddInt32(&cachedTiles, 1)
			// 累加统计数据
			stats.TotalSizeBytes += result.sizeBytes
		}

		atomic.AddInt32(&totalTiles, 1)
	}

	// 计算每层统计数据
	actualMaxZoom := cfg.MinZoom
	for z := cfg.MinZoom; z <= cfg.MaxZoom; z++ {
		if stats, exists := zoomStats[z]; exists {
			if stats.GeneratedTiles > 0 {
				stats.AvgSizeKB = bytesToKB(stats.TotalSizeBytes) / float64(stats.GeneratedTiles)
				actualMaxZoom = z
			}

			logger.L().Info("层级统计",
				"zoom", z,
				"generated_tiles", stats.GeneratedTiles,
				"empty_tiles", stats.EmptyTiles,
				"avg_size_kb", stats.AvgSizeKB)

			lastStats = stats
		}
	}

	duration := time.Since(startTime)

	// 计算流水线效率
	idealParallel := float64(cfg.Concurrency) * duration.Seconds()
	actualParallel := float64(atomic.LoadInt32(&totalTasksSubmitted)) // 简化估算
	efficiency := actualParallel / idealParallel
	if efficiency > 1.0 {
		efficiency = 1.0
	}

	result := &PipelineGenerateResult{
		GenerateResult: GenerateResult{
			TotalTiles:    int(atomic.LoadInt32(&totalTiles)),
			CachedTiles:   int(atomic.LoadInt32(&cachedTiles)),
			ActualMaxZoom: actualMaxZoom,
			StopReason:    "max_zoom_reached",
			GenerationSec: duration.Seconds(),
		},
		PipelineEfficiency: efficiency,
	}

	if lastStats != nil {
		result.LastZoomAvgSizeKB = lastStats.AvgSizeKB
	}

	logger.L().Info("流水线生成完成",
		"total_tiles", result.TotalTiles,
		"cached_tiles", result.CachedTiles,
		"duration_sec", result.GenerationSec,
		"max_zoom", result.ActualMaxZoom,
		"pipeline_efficiency", efficiency)

	return result, nil
}

// generateTasksForZoom 为指定 zoom 层级生成任务列表
func (g *PipelineTileGenerator) generateTasksForZoom(cfg QuickViewConfig, zoom int) []*TileTask {
	minX, minY, maxX, maxY := calculateTileBounds(cfg.Extent, zoom)

	tasks := make([]*TileTask, 0, (maxX-minX+1)*(maxY-minY+1))
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			tasks = append(tasks, &TileTask{
				Coord:    TileCoord{Z: zoom, X: x, Y: y},
				Priority: zoom, // 低 zoom 优先
			})
		}
	}

	return tasks
}

// processTileWithUpload 处理单个瓦片并上传到 MinIO
func (g *PipelineTileGenerator) processTileWithUpload(
	ctx context.Context,
	cfg QuickViewConfig,
	coord TileCoord,
) *tileResult {
	startTime := time.Now()

	// 1. 生成瓦片
	params := TileGenerationParams{
		ResourceID: cfg.ResourceID,
		TenantID:   cfg.TenantID,
		Schema:     cfg.Schema,
		Table:      cfg.Table,
		GeomColumn: cfg.GeomColumn,
		SRID:       cfg.SRID,
		PrimaryKey: cfg.PrimaryKey,
		Z:          coord.Z,
		X:          coord.X,
		Y:          coord.Y,
	}

	mvtData, err := g.tileGen.GenerateTile(ctx, params)
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

	// 4. 上传到 MinIO
	err = g.minioClient.PutTile(ctx, cfg.Fingerprint, coord.Z, coord.X, coord.Y, gzData)
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
