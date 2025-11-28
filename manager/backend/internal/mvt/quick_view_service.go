package mvt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/addp/common/logger"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// QuickViewService 快显服务
// 负责批量生成 MVT 瓦片并存储到 MinIO
type QuickViewService struct {
	tileGen          *TileGenerator
	minioClient      *minio.Client
	bucket           string
	pipelineGen      *PipelineTileGenerator // 流水线生成器
	usePipeline      bool                   // 是否使用流水线模式
}

// QuickViewConfig 快显配置
type QuickViewConfig struct {
	ResourceID       uint
	TenantID    uint
	Schema      string
	Table       string
	GeomColumn  string
	SRID        int
	PrimaryKey  string
	Extent      []float64 // [minLng, minLat, maxLng, maxLat]
	MinZoom     int       // 自动计算或指定
	MaxZoom     int
	Concurrency int
	Fingerprint string
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

	// 创建 MinIO 上传适配器
	uploader := &minioUploaderAdapter{
		client: client,
		bucket: minioCfg.Bucket,
	}

	// 初始化流水线生成器
	pipelineGen := NewPipelineTileGenerator(tileGen, uploader, minioCfg.Bucket)

	logger.L().Info("QuickView service initialized",
		"endpoint", minioCfg.Endpoint,
		"bucket", minioCfg.Bucket,
		"pipeline_mode", "enabled")

	return &QuickViewService{
		tileGen:     tileGen,
		minioClient: client,
		bucket:      minioCfg.Bucket,
		pipelineGen: pipelineGen,
		usePipeline: true, // 默认启用流水线模式
	}, nil
}

// GenerateResult 快显生成结果
type GenerateResult struct {
	TotalTiles       int
	CachedTiles      int
	ActualMaxZoom    int
	LastZoomAvgTimeMs float64
	LastZoomAvgSizeKB float64
	StopReason       string
	GenerationSec    float64
}

// Generate 开始生成快显缓存
func (s *QuickViewService) Generate(
	ctx context.Context,
	cfg QuickViewConfig,
) (*GenerateResult, error) {
	// 使用流水线并行模式
	if s.usePipeline {
		pipelineCfg := PipelineGenerateConfig{
			QuickViewConfig:  cfg,
			IdleThreshold:    cfg.Concurrency / 2, // 50% 空闲时启动下一层
			MaxPendingTasks:  cfg.Concurrency * 10,
		}

		result, err := s.pipelineGen.GeneratePipeline(ctx, pipelineCfg)
		if err != nil {
			return nil, err
		}

		// 转换结果（PipelineGenerateResult 嵌入了 GenerateResult）
		logger.L().Info("流水线模式生成完成",
			"efficiency", result.PipelineEfficiency)

		return &result.GenerateResult, nil
	}

	// 原有的逐层串行模式（保留作为备用）
	return s.generateSequential(ctx, cfg)
}

// generateSequential 逐层串行生成（原有实现，作为备用）
func (s *QuickViewService) generateSequential(
	ctx context.Context,
	cfg QuickViewConfig,
) (*GenerateResult, error) {
	startTime := time.Now()

	logger.L().Info("开始生成快显缓存（串行模式）",
		"resource_id", cfg.ResourceID,
		"table", fmt.Sprintf("%s.%s", cfg.Schema, cfg.Table),
		"min_zoom", cfg.MinZoom,
		"max_zoom", cfg.MaxZoom,
		"concurrency", cfg.Concurrency)

	result := &GenerateResult{}
	var totalTiles, cachedTiles int
	var lastStats *ZoomLevelStats

	// 逐层生成瓦片
	for z := cfg.MinZoom; z <= cfg.MaxZoom; z++ {
		logger.L().Info("开始生成缩放级别", "zoom", z)

		stats, err := s.processZoomLevel(ctx, cfg, z)
		if err != nil {
			return nil, fmt.Errorf("failed to process zoom level %d: %w", z, err)
		}

		totalTiles += stats.TotalTiles
		cachedTiles += stats.GeneratedTiles
		lastStats = stats

		logger.L().Info("缩放级别完成",
			"zoom", z,
			"generated_tiles", stats.GeneratedTiles,
			"empty_tiles", stats.EmptyTiles,
			"avg_gen_time_ms", stats.AvgGenTimeMs,
			"avg_size_kb", stats.AvgSizeKB)

		// 达到最大层级
		if z == cfg.MaxZoom {
			result.ActualMaxZoom = z
			result.StopReason = "max_zoom_reached"
		}
	}

	// 填充结果
	result.TotalTiles = totalTiles
	result.CachedTiles = cachedTiles
	result.GenerationSec = time.Since(startTime).Seconds()

	if lastStats != nil {
		result.LastZoomAvgTimeMs = lastStats.AvgGenTimeMs
		result.LastZoomAvgSizeKB = lastStats.AvgSizeKB
	}

	// 保存元数据到 MinIO
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
		CreatedAt:        time.Now(),
		GenerationSec:    result.GenerationSec,
	}

	if err := s.putMetadata(ctx, cfg.Fingerprint, metadata); err != nil {
		logger.L().Error("Failed to save metadata", "error", err)
	}

	logger.L().Info("快显生成完成（串行模式）",
		"total_tiles", result.TotalTiles,
		"cached_tiles", result.CachedTiles,
		"duration_sec", result.GenerationSec,
		"max_zoom", result.ActualMaxZoom)

	return result, nil
}

// processZoomLevel 处理单个缩放级别
func (s *QuickViewService) processZoomLevel(
	ctx context.Context,
	cfg QuickViewConfig,
	zoom int,
) (*ZoomLevelStats, error) {
	// 1. 计算瓦片范围
	minX, minY, maxX, maxY := calculateTileBounds(cfg.Extent, zoom)
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
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for coord := range jobs {
				result := s.processTile(ctx, cfg, coord)
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
func (s *QuickViewService) processTile(
	ctx context.Context,
	cfg QuickViewConfig,
	coord TileCoord,
) *tileResult {
	startTime := time.Now()

	// 1. 检查瓦片是否已存在于 MinIO
	objectPath := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", cfg.Fingerprint, coord.Z, coord.X, coord.Y)
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
	objectPath := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

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
	objectPath := fmt.Sprintf("%s/metadata.json", fingerprint)

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
	objectPath := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

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

// minioUploaderAdapter MinIO 上传适配器（实现 MinIOUploader 接口）
type minioUploaderAdapter struct {
	client *minio.Client
	bucket string
}

func (a *minioUploaderAdapter) PutTile(ctx context.Context, fingerprint string, z, x, y int, data []byte) error {
	objectPath := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	_, err := a.client.PutObject(ctx, a.bucket, objectPath, bytes.NewReader(data), int64(len(data)),
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
