package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/repository"
	"github.com/minio/minio-go/v7"
)

// UnifiedMVTService 统一的 MVT 服务
// 整合实时生成和缓存访问，对前端隐藏 fingerprint，实现三层缓存穿透
type UnifiedMVTService struct {
	spatialPreviewService *SpatialPreviewService // 缓存访问（内存 LRU → Redis → MinIO）
	mvtService            *MVTService            // 实时生成（直接从 PG 查询）
	metadataRepo          *repository.MetadataRepository
	quickViewService      *QuickViewService      // 快显服务（可选，用于更新统计）
}

// NewUnifiedMVTService 创建统一的 MVT 服务
func NewUnifiedMVTService(
	spatialPreviewService *SpatialPreviewService,
	mvtService *MVTService,
	metadataRepo *repository.MetadataRepository,
) *UnifiedMVTService {
	return &UnifiedMVTService{
		spatialPreviewService: spatialPreviewService,
		mvtService:            mvtService,
		metadataRepo:          metadataRepo,
		quickViewService:      nil, // 延迟注入
	}
}

// SetQuickViewService 设置快显服务（避免循环依赖）
func (s *UnifiedMVTService) SetQuickViewService(quickViewService *QuickViewService) {
	s.quickViewService = quickViewService
}

// TileResponse 瓦片响应结构
type TileResponse struct {
	Data      []byte        // 瓦片数据
	FromCache bool          // 是否来自缓存
	Duration  time.Duration // 生成/获取耗时
}

// GetTile 获取 MVT 瓦片（统一入口，自动缓存穿透）
// 流程：内存 LRU → Redis → MinIO → 实时 PG 生成 → 持久化到 MinIO
func (s *UnifiedMVTService) GetTile(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	schema, table, geomCol string,
	cols []string,
	z, x, y int,
	srid int,
) (*TileResponse, error) {
	startTime := time.Now()

	// 1. 计算 fingerprint（对前端透明）
	fingerprint := s.calculateFingerprint(resourceID, schema, table)

	// 2. 尝试从三层缓存获取（内存 LRU → Redis → MinIO）
	tileData, err := s.spatialPreviewService.GetTile(ctx, fingerprint, z, x, y)
	if err == nil && len(tileData) > 0 {
		duration := time.Since(startTime)
		logger.L().Debug("瓦片从缓存获取",
			"resource_id", resourceID,
			"schema", schema,
			"table", table,
			"z", z, "x", x, "y", y,
			"from_cache", true,
			"duration", duration,
			"size", len(tileData))
		return &TileResponse{
			Data:      tileData,
			FromCache: true,
			Duration:  duration,
		}, nil
	}

	// 3. 缓存未命中，从 PG 实时生成
	logger.L().Debug("缓存未命中，开始实时生成瓦片",
		"resource_id", resourceID,
		"schema", schema,
		"table", table,
		"z", z, "x", x, "y", y)

	tileData, err = s.mvtService.GetTile(ctx, tenantID, resourceID, schema, table, geomCol, cols, z, x, y, srid)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tile from PG: %w", err)
	}

	duration := time.Since(startTime)

	logger.L().Info("瓦片实时生成完成",
		"resource_id", resourceID,
		"schema", schema,
		"table", table,
		"z", z, "x", x, "y", y,
		"from_cache", false,
		"duration", duration,
		"size", len(tileData))

	// 4. 判断是否需要自动缓存（生成时间 > 300ms 或 大小 > 100KB）
	tileSizeKB := float64(len(tileData)) / 1024.0
	durationMs := float64(duration.Milliseconds())
	shouldCache := durationMs > 300 || tileSizeKB > 100

	if shouldCache && len(tileData) > 0 {
		// 异步持久化到 MinIO（包括回填 Redis 和内存缓存）
		go s.persistToMinIO(context.Background(), fingerprint, z, x, y, tileData,
			resourceID, schema, table, tenantID, durationMs, tileSizeKB)
	}

	return &TileResponse{
		Data:      tileData,
		FromCache: false,
		Duration:  duration,
	}, nil
}

// calculateFingerprint 计算表的 fingerprint（内部使用，对前端透明）
// 使用 common 模块的统一算法：SHA256(resourceID:schema.table)
func (s *UnifiedMVTService) calculateFingerprint(resourceID uint, schema, table string) string {
	return commonModels.GenerateTableFingerprint(resourceID, schema, table)
}

// persistToMinIO 持久化瓦片到 MinIO（异步执行，不阻塞响应）
func (s *UnifiedMVTService) persistToMinIO(
	ctx context.Context,
	fingerprint string,
	z, x, y int,
	tileData []byte,
	resourceID uint,
	schema, table string,
	tenantID *uint,
	durationMs, tileSizeKB float64,
) {
	// 1. Gzip 压缩瓦片数据
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	if _, err := gzipWriter.Write(tileData); err != nil {
		logger.L().Warn("Gzip 压缩瓦片数据失败", "error", err, "fingerprint", fingerprint, "z", z, "x", x, "y", y)
		return
	}
	if err := gzipWriter.Close(); err != nil {
		logger.L().Warn("Gzip 关闭失败", "error", err)
		return
	}
	compressed := buf.Bytes()

	// 2. 确保 MinIO 客户端已初始化
	if err := s.spatialPreviewService.ensureMinIOClient(ctx); err != nil {
		logger.L().Warn("MinIO 客户端初始化失败", "error", err)
		return
	}

	// 3. 构建 MinIO 对象名
	objectName := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	// 4. 上传到 MinIO
	_, err := s.spatialPreviewService.minioClient.PutObject(
		ctx,
		s.spatialPreviewService.bucket,
		objectName,
		bytes.NewReader(compressed),
		int64(len(compressed)),
		minio.PutObjectOptions{
			ContentType:     "application/vnd.mapbox-vector-tile",
			ContentEncoding: "gzip",
		},
	)
	if err != nil {
		logger.L().Warn("上传瓦片到 MinIO 失败", "error", err, "fingerprint", fingerprint, "z", z, "x", x, "y", y)
		return
	}

	logger.L().Info("瓦片已持久化到 MinIO",
		"fingerprint", fingerprint,
		"z", z, "x", x, "y", y,
		"compressed_size", len(compressed),
		"object_name", objectName)

	// 5. 回填上层缓存（Redis + 内存 LRU）
	cacheKey := s.spatialPreviewService.buildCacheKey(fingerprint, z, x, y)
	s.spatialPreviewService.backfillCache(ctx, cacheKey, compressed)

	// 6. 更新快显统计（如果有QuickViewService且表有快显记录）
	if s.quickViewService != nil && tenantID != nil {
		err := s.quickViewService.IncrementCachedTiles(
			ctx,
			*tenantID,
			resourceID,
			schema,
			table,
			durationMs,
			tileSizeKB,
		)
		if err != nil {
			// 不阻塞，仅记录日志
			logger.L().Debug("更新快显统计失败（可能表无快显记录）",
				"error", err,
				"resource_id", resourceID,
				"table", fmt.Sprintf("%s.%s", schema, table))
		}
	}
}
