package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/repository"
	"github.com/minio/minio-go/v7"
	"golang.org/x/sync/singleflight"
)

// UnifiedMVTService 统一的 MVT 服务
// 整合实时生成和缓存访问，对前端隐藏 fingerprint，实现三层缓存穿透
type UnifiedMVTService struct {
	spatialPreviewService *SpatialPreviewService // 缓存访问（内存 LRU → Redis → MinIO）
	mvtService            *MVTService            // 实时生成（直接从 PG 查询）
	metadataRepo          *repository.MetadataRepository
	quickViewService      *QuickViewService  // 快显服务（可选，用于更新统计）
	sf                    singleflight.Group // ✅ Singleflight 防缓存击穿
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
// 流程：快显预生成缓存 → 内存 LRU → Redis → MinIO → 实时 PG 生成 → 持久化到 MinIO
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

	// ✅ 租户验证（必须传递 tenant_id）
	if tenantID == nil {
		return nil, fmt.Errorf("tenant_id is required for MVT tile access")
	}

	// 1. 计算 fingerprint（对前端透明）
	fingerprint := s.calculateFingerprint(resourceID, schema, table)
	logger.L().Info("📍 统一MVT服务收到请求",
		"tenant_id", *tenantID,
		"engine_id", resourceID,
		"schema", schema,
		"table", table,
		"z", z, "x", x, "y", y,
		"fingerprint", fingerprint)

	// 2. 验证 zoom 层级是否合理（基于数据的地理范围）
	// - zoom < minZoom: 返回空瓦片（数据太小，不可见）
	// - minZoom <= zoom <= maxZoom: 正常处理（预缓存范围）
	// - zoom > maxZoom: 允许但降低缓存优先级（超出预缓存范围）
	logger.L().Info("🔍 检查 zoom 验证条件",
		"quickViewService_nil", s.quickViewService == nil,
		"tenantID_nil", tenantID == nil,
		"tenantID", tenantID)

	var minZoom, maxZoom int
	var beyondMaxZoom bool // 是否超出预缓存范围

	if s.quickViewService != nil && tenantID != nil {
		qv, err := s.quickViewService.GetStatus(ctx, *tenantID, resourceID, schema, table)
		logger.L().Info("🔍 GetStatus 结果",
			"err", err,
			"extent_len", len(qv.Extent),
			"extent", qv.Extent)
		if err == nil && len(qv.Extent) == 4 {
			// 使用 ExtentSRID（QuickView 中记录的 extent 坐标系）
			extentSRID := qv.ExtentSRID
			if extentSRID == 0 {
				extentSRID = 4326 // 向后兼容，默认 WGS84
			}

			// 计算 minZoom 和 maxZoom
			minZoom = spatial.CalculateMinZoomFromExtent(qv.Extent, extentSRID)
			maxZoom = qv.MaxZoom // 从快显配置获取（智能计算的 maxZoom）
			if maxZoom == 0 {
				maxZoom = 18 // 默认值
			}

			// 对 minZoom 进行后处理（与 tile_config_handler 保持一致）
			minZoom = minZoom - 2
			if minZoom < 3 {
				minZoom = 3
			}

			// 验证 zoom 是否低于 minZoom（返回空瓦片）
			if z < minZoom {
				logger.L().Info("Zoom 层级过低，返回空瓦片",
					"z", z,
					"min_zoom", minZoom,
					"extent", qv.Extent,
					"extent_srid", extentSRID)
				// 返回空瓦片，让前端停止请求（不是错误）
				return &TileResponse{
					Data:      []byte{}, // 空瓦片
					FromCache: false,
					Duration:  time.Since(startTime),
				}, nil
			}

			// 检查是否超出 maxZoom（允许但降低缓存优先级）
			if z > maxZoom {
				beyondMaxZoom = true
				logger.L().Info("Zoom 层级超出预缓存范围，使用实时生成模式",
					"z", z,
					"max_zoom", maxZoom,
					"will_cache_if_heavy", true)
			}
		}
	}

	// 3. 尝试从三层缓存获取（内存 LRU → Redis → MinIO，租户隔离）
	// MinIO 中包含快显预生成和实时缓存的瓦片，两者存储在同一位置
	logger.L().Info("🔎 尝试从缓存获取瓦片...", "tenant_id", *tenantID)
	tileData, err := s.spatialPreviewService.GetTile(ctx, *tenantID, fingerprint, z, x, y)
	if err == nil && len(tileData) > 0 {
		duration := time.Since(startTime)
		logger.L().Info("✅ 从缓存返回瓦片",
			"tenant_id", *tenantID,
			"size", len(tileData),
			"duration", duration)
		return &TileResponse{
			Data:      tileData,
			FromCache: true,
			Duration:  duration,
		}, nil
	}

	// 特别记录：缓存返回了空数据或错误
	if err != nil {
		logger.L().Warn("⚠️  缓存查询出错", "error", err)
	} else if len(tileData) == 0 {
		logger.L().Warn("⚠️  缓存返回空数据")
	}

	// 4. 缓存未命中，使用 singleflight 从 PG 实时生成
	logger.L().Info("🔧 缓存未命中，开始实时生成瓦片 (singleflight)",
		"engine_id", resourceID,
		"schema", schema,
		"table", table,
		"z", z, "x", x, "y", y)

	// ✅ 构建 singleflight key (确保相同瓦片的并发请求使用同一 key，租户隔离)
	sfKey := fmt.Sprintf("%d:%d:%s:%s:%d:%d:%d", *tenantID, resourceID, schema, table, z, x, y)

	// ✅ singleflight.Do: 多个并发请求同一瓦片时,只生成一次
	v, err, shared := s.sf.Do(sfKey, func() (interface{}, error) {
		// 创建 5 秒超时的 context（实时生成必须快速响应）
		genCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		tileData, err := s.mvtService.GetTile(genCtx, tenantID, resourceID, schema, table, geomCol, cols, z, x, y, srid)
		if err != nil {
			// 特殊处理超时错误：返回空瓦片而非错误（优雅降级）
			if errors.Is(err, context.DeadlineExceeded) {
				logger.L().Warn("实时 MVT 生成超时，返回空瓦片",
					"z", z, "x", x, "y", y,
					"timeout", "5s")
				return []byte{}, nil
			}
			return nil, fmt.Errorf("failed to generate tile from PG: %w", err)
		}

		return tileData, nil
	})

	if err != nil {
		return nil, err
	}

	tileData = v.([]byte)
	duration := time.Since(startTime)

	// ✅ 记录是否为 singleflight 合并的请求
	if shared {
		logger.L().Info("✅ Singleflight 合并请求 (共享结果)",
			"sf_key", sfKey,
			"duration", duration,
			"size", len(tileData))
	} else {
		logger.L().Info("瓦片实时生成完成 (首次请求)",
			"engine_id", resourceID,
			"schema", schema,
			"table", table,
			"z", z, "x", x, "y", y,
			"duration", duration,
			"size", len(tileData))
	}

	// 5. 判断是否需要自动缓存
	// - 预缓存范围内（z <= maxZoom）: 生成时间 > 100ms 或 大小 > 50KB
	// - 超出预缓存范围（z > maxZoom）: 更严格的缓存条件（生成时间 > 200ms 且 大小 > 100KB）
	tileSizeKB := float64(len(tileData)) / 1024.0
	durationMs := float64(duration.Milliseconds())

	var shouldCache bool
	if beyondMaxZoom {
		// 超出预缓存范围：仅缓存高成本瓦片（避免缓存爆炸）
		shouldCache = durationMs > 200 && tileSizeKB > 100
		logger.L().Info("超出预缓存范围的瓦片缓存判断",
			"z", z,
			"duration_ms", durationMs,
			"size_kb", tileSizeKB,
			"should_cache", shouldCache,
			"reason", "beyond_max_zoom_strict_policy")
	} else {
		// 预缓存范围内：正常缓存策略
		shouldCache = durationMs > 100 || tileSizeKB > 50
	}

	if shouldCache && len(tileData) > 0 {
		// 异步持久化到 MinIO（包括回填 Redis 和内存缓存，租户隔离）
		go s.persistToMinIO(context.Background(), *tenantID, fingerprint, z, x, y, tileData,
			resourceID, schema, table, tenantID, durationMs, tileSizeKB)
	}

	return &TileResponse{
		Data:      tileData,
		FromCache: false,
		Duration:  duration,
	}, nil
}

// calculateFingerprint 计算表的 fingerprint（内部使用，对前端透明）
// 使用 common 模块的统一算法：SHA256(engineID:schema.table)
func (s *UnifiedMVTService) calculateFingerprint(engineID uint, schema, table string) string {
	// 两步计算方式：先拼接 full_name，再计算指纹
	fullName := fmt.Sprintf("%s.%s", schema, table)
	return commonModels.GenerateItemFingerprint(engineID, fullName)
}

// persistToMinIO 持久化瓦片到 MinIO（异步执行，不阻塞响应，租户隔离）
func (s *UnifiedMVTService) persistToMinIO(
	ctx context.Context,
	tenantID uint, // ✅ 改为必传参数
	fingerprint string,
	z, x, y int,
	tileData []byte,
	resourceID uint,
	schema, table string,
	tenantIDPtr *uint, // ✅ 保留原参数（用于更新 QuickView）
	durationMs, tileSizeKB float64,
) {
	// 1. Gzip 压缩瓦片数据
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	if _, err := gzipWriter.Write(tileData); err != nil {
		logger.L().Warn("Gzip 压缩瓦片数据失败",
			"tenant_id", tenantID,
			"error", err, "fingerprint", fingerprint, "z", z, "x", x, "y", y)
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

	// 3. 构建 MinIO 对象名（租户隔离）
	objectName := s.spatialPreviewService.buildMinIOPath(tenantID, fingerprint, z, x, y)

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
		logger.L().Warn("上传瓦片到 MinIO 失败",
			"tenant_id", tenantID,
			"error", err, "fingerprint", fingerprint, "z", z, "x", x, "y", y)
		return
	}

	logger.L().Info("瓦片已持久化到 MinIO",
		"tenant_id", tenantID,
		"fingerprint", fingerprint,
		"z", z, "x", x, "y", y,
		"compressed_size", len(compressed),
		"object_name", objectName)

	// 5. 回填上层缓存（Redis + 内存 LRU，租户隔离）
	cacheKey := s.spatialPreviewService.buildCacheKey(tenantID, fingerprint, z, x, y)
	s.spatialPreviewService.backfillCache(ctx, cacheKey, compressed)

	// 6. 更新快显统计（如果有QuickViewService且表有快显记录）
	if s.quickViewService != nil && tenantIDPtr != nil {
		err := s.quickViewService.IncrementCachedTiles(
			ctx,
			*tenantIDPtr,
			resourceID,
			schema,
			table,
		)
		if err != nil {
			// 不阻塞，仅记录日志
			logger.L().Debug("更新快显统计失败（可能表无快显记录）",
				"error", err,
				"engine_id", resourceID,
				"table", fmt.Sprintf("%s.%s", schema, table))
		}
	}
}
