package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/repository"
	"golang.org/x/sync/singleflight"
)

// UnifiedMVTService 统一的 MVT 服务。
// 整合实时生成和按 storage_ref 访问的瓦片对象，对前端隐藏 fingerprint。
type UnifiedMVTService struct {
	spatialPreviewService *SpatialPreviewService // 瓦片对象访问（内存 LRU → Redis → MinIO）
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

func (s *UnifiedMVTService) GetSpatialExtentWGS84(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	schema, table, geomCol string,
) ([]float64, error) {
	if s.mvtService == nil {
		return nil, fmt.Errorf("MVT service is not initialized")
	}
	return s.mvtService.GetSpatialExtentWGS84(ctx, tenantID, resourceID, schema, table, geomCol)
}

func (s *UnifiedMVTService) TransformExtentWGS84(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	extent []float64,
	extentSRID int,
) ([]float64, error) {
	if s.mvtService == nil {
		return nil, fmt.Errorf("MVT service is not initialized")
	}
	return s.mvtService.TransformExtentWGS84(ctx, tenantID, resourceID, extent, extentSRID)
}

func (s *UnifiedMVTService) ResolveRealtimeTileTarget(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	schema, table, geomCol string,
	sourceSRID int,
) (*RealtimeTileTarget, error) {
	if s.mvtService == nil {
		return nil, fmt.Errorf("MVT service is not initialized")
	}
	return s.mvtService.ResolveRealtimeTileTarget(ctx, tenantID, resourceID, schema, table, geomCol, sourceSRID)
}

// TileResponse 瓦片响应结构
type TileResponse struct {
	Data                  []byte        // 瓦片数据
	FromCache             bool          // 是否来自缓存
	Duration              time.Duration // 生成/获取耗时
	RenderSource          string        // cached_tile 或 realtime_tile
	TileCacheID           *uint         // 命中的瓦片缓存结果 ID
	Status                string        // ok、empty、timeout、degraded
	PerformanceMode       string        // ready_3857_target、source_transform_path 等
	TimeoutRecommendation string        // 超时时推荐动作
	TimeoutRetryPolicy    string        // 超时后前端重试策略
}

const (
	TileStatusOK       = "ok"
	TileStatusEmpty    = "empty"
	TileStatusTimeout  = "timeout"
	TileStatusDegraded = "degraded"
)

// GetTile 获取 MVT 瓦片（统一入口）。
// 流程：按 storage_ref 查询已有瓦片对象 → 实时 PG 生成 → 按当前 storage_ref 回填对象存储。
func (s *UnifiedMVTService) GetTile(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	schema, table, geomCol string,
	cols []string,
	z, x, y int,
	srid int,
	realtimeTarget *RealtimeTileTarget,
) (*TileResponse, error) {
	startTime := time.Now()

	// 租户验证（必须传递 tenant_id）
	if tenantID == nil {
		return nil, fmt.Errorf("tenant_id is required for MVT tile access")
	}

	// 1. 计算 fingerprint（对前端透明）
	fingerprint := s.calculateFingerprint(resourceID, schema, table)
	logger.L().Info("统一 MVT 服务收到请求",
		"tenant_id", *tenantID,
		"engine_id", resourceID,
		"schema", schema,
		"table", table,
		"z", z, "x", x, "y", y,
		"fingerprint", fingerprint)

	// 2. 验证 zoom 层级是否合理（基于数据的地理范围）
	// - zoom < minZoom: 返回空瓦片（数据太小，不可见）
	// - minZoom <= zoom <= maxZoom: 正常处理（产物覆盖范围）
	// - zoom > maxZoom: 允许但降低缓存优先级（超出产物覆盖范围）
	logger.L().Debug("检查瓦片层级约束",
		"quickViewService_nil", s.quickViewService == nil,
		"tenantID_nil", tenantID == nil,
		"tenantID", tenantID)

	var minZoom, maxZoom int
	var beyondMaxZoom bool // 是否超出产物覆盖范围
	cacheScope := fmt.Sprintf("fingerprint:%s", fingerprint)
	storageRef := ""
	renderSource := QuickViewRenderSourceRealtimeTile
	var tileCacheID *uint

	if s.quickViewService != nil && tenantID != nil {
		tileCache, err := s.quickViewService.GetDefaultTileCache(ctx, *tenantID, resourceID, schema, table)
		if err != nil {
			logger.L().Warn("获取默认瓦片缓存结果失败，使用实时生成路径", "error", err)
		}
		if tileCache != nil && strings.TrimSpace(tileCache.StorageRef) != "" {
			storageRef = strings.TrimSpace(tileCache.StorageRef)
			cacheScope = fmt.Sprintf("tile_cache:%d", tileCache.ID)
			renderSource = QuickViewRenderSourceCachedTile
			tileCacheID = &tileCache.ID
		}
		extent, extentSRID, hasExtent := TileCacheExtent(tileCache)
		if hasExtent {
			// 计算 minZoom 和 maxZoom
			minZoom = spatial.CalculateMinZoomFromExtent(extent, extentSRID)
			if configuredMinZoom, configuredMaxZoom, ok := TileCacheZoomRange(tileCache); ok {
				minZoom = configuredMinZoom
				maxZoom = configuredMaxZoom
			}
			if maxZoom == 0 {
				maxZoom = 18
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
					"extent", extent,
					"extent_srid", extentSRID)
				// 返回空瓦片，让前端停止请求（不是错误）
				return &TileResponse{
					Data:         []byte{}, // 空瓦片
					FromCache:    false,
					Duration:     time.Since(startTime),
					RenderSource: renderSource,
					TileCacheID:  tileCacheID,
					Status:       TileStatusEmpty,
				}, nil
			}

			// 检查是否超出 maxZoom（允许但降低缓存优先级）
			if z > maxZoom {
				beyondMaxZoom = true
				logger.L().Info("Zoom 层级超出瓦片缓存结果覆盖范围，使用实时生成模式",
					"z", z,
					"max_zoom", maxZoom,
					"will_cache_if_heavy", true)
			}
		}
	}

	// 3. 尝试从 storage_ref 对应的瓦片对象获取（内存 LRU → Redis → 对象存储，租户隔离）
	// 瓦片对象位置统一由 storage_ref 描述。
	var tileData []byte
	var err error
	if strings.TrimSpace(storageRef) != "" {
		logger.L().Debug("尝试从 storage_ref 获取瓦片", "tenant_id", *tenantID, "storage_ref", storageRef)
		tileData, err = s.spatialPreviewService.GetTileByStorageRef(ctx, *tenantID, cacheScope, storageRef, z, x, y)
		if err == nil && len(tileData) > 0 {
			duration := time.Since(startTime)
			logger.L().Info("从瓦片对象返回瓦片",
				"tenant_id", *tenantID,
				"size", len(tileData),
				"duration", duration)
			return &TileResponse{
				Data:         tileData,
				FromCache:    true,
				Duration:     duration,
				RenderSource: renderSource,
				TileCacheID:  tileCacheID,
				Status:       TileStatusOK,
			}, nil
		}

		// 特别记录：瓦片对象查询返回了空数据或错误
		if err != nil {
			logger.L().Warn("瓦片对象查询出错", "error", err)
		} else if len(tileData) == 0 {
			logger.L().Debug("瓦片对象未命中")
		}
	}

	if realtimeTarget == nil {
		logger.L().Info("瓦片对象未命中且没有可索引 realtime target，返回空瓦片",
			"engine_id", resourceID,
			"schema", schema,
			"table", table,
			"z", z, "x", x, "y", y)
		return &TileResponse{
			Data:         []byte{},
			FromCache:    false,
			Duration:     time.Since(startTime),
			RenderSource: renderSource,
			TileCacheID:  tileCacheID,
			Status:       TileStatusDegraded,
		}, nil
	}

	// 4. 瓦片对象未命中，使用 singleflight 从 PG 实时生成
	generationSchema := realtimeTarget.Schema
	generationTable := realtimeTarget.Table
	generationGeomCol := realtimeTarget.GeomColumn
	generationSRID := realtimeTarget.SRID
	realtimeInfo := realtimeTileInfoFromTarget(realtimeTarget)
	logger.L().Info("瓦片对象未命中，开始实时生成瓦片",
		"engine_id", resourceID,
		"schema", generationSchema,
		"table", generationTable,
		"z", z, "x", x, "y", y)

	// 构建 singleflight key，确保相同瓦片的并发请求使用同一 key，租户隔离。
	sfKey := fmt.Sprintf("%d:%d:%s:%s:%d:%d:%d", *tenantID, resourceID, generationSchema, generationTable, z, x, y)

	// 多个并发请求同一瓦片时只生成一次。
	v, err, shared := s.sf.Do(sfKey, func() (interface{}, error) {
		// 创建 5 秒超时的 context（实时生成必须快速响应）
		genCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		tileData, err := s.mvtService.GetTile(genCtx, tenantID, resourceID, generationSchema, generationTable, generationGeomCol, cols, z, x, y, generationSRID)
		if err != nil {
			// 特殊处理超时错误：返回空瓦片而非错误（优雅降级）
			if errors.Is(err, context.DeadlineExceeded) {
				logger.L().Warn("实时 MVT 生成超时，返回空瓦片",
					"z", z, "x", x, "y", y,
					"timeout", "5s")
				return &TileResponse{
					Data:                  []byte{},
					FromCache:             false,
					Duration:              time.Since(startTime),
					RenderSource:          QuickViewRenderSourceRealtimeTile,
					Status:                TileStatusTimeout,
					PerformanceMode:       realtimeInfo.PerformanceMode,
					TimeoutRecommendation: realtimeInfo.TimeoutRecommendation,
					TimeoutRetryPolicy:    realtimeInfo.TimeoutRetryPolicy,
				}, nil
			}
			return nil, fmt.Errorf("failed to generate tile from PG: %w", err)
		}

		return &TileResponse{
			Data:                  tileData,
			FromCache:             false,
			Duration:              time.Since(startTime),
			RenderSource:          QuickViewRenderSourceRealtimeTile,
			Status:                tileStatusForData(tileData),
			PerformanceMode:       realtimeInfo.PerformanceMode,
			TimeoutRecommendation: realtimeInfo.TimeoutRecommendation,
			TimeoutRetryPolicy:    realtimeInfo.TimeoutRetryPolicy,
		}, nil
	})

	if err != nil {
		return nil, err
	}

	response := v.(*TileResponse)
	tileData = response.Data
	duration := time.Since(startTime)

	// 记录是否为 singleflight 合并的请求。
	if shared {
		logger.L().Info("Singleflight 合并请求",
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

	// 5. 判断是否需要按当前 storage_ref 回填瓦片对象
	// - 产物覆盖范围内（z <= maxZoom）: 生成时间 > 100ms 或 大小 > 50KB
	// - 超出产物覆盖范围（z > maxZoom）: 更严格的缓存条件（生成时间 > 200ms 且 大小 > 100KB）
	tileSizeKB := float64(len(tileData)) / 1024.0
	durationMs := float64(duration.Milliseconds())

	var shouldCache bool
	if beyondMaxZoom {
		// 超出产物覆盖范围：仅缓存高成本瓦片（避免缓存爆炸）
		shouldCache = durationMs > 200 && tileSizeKB > 100
		logger.L().Info("超出瓦片缓存结果覆盖范围的缓存判断",
			"z", z,
			"duration_ms", durationMs,
			"size_kb", tileSizeKB,
			"should_cache", shouldCache,
			"reason", "beyond_max_zoom_strict_policy")
	} else {
		// 产物覆盖范围内：正常回填策略
		shouldCache = durationMs > 100 || tileSizeKB > 50
	}

	if shouldCache && len(tileData) > 0 && strings.TrimSpace(storageRef) != "" {
		// 异步持久化到对象存储（包括回填 Redis 和内存缓存，租户隔离）
		go s.persistToObjectStorage(context.Background(), *tenantID, cacheScope, storageRef, z, x, y, tileData)
	}

	return &TileResponse{
		Data:                  tileData,
		FromCache:             false,
		Duration:              duration,
		RenderSource:          QuickViewRenderSourceRealtimeTile,
		TileCacheID:           nil,
		Status:                response.Status,
		PerformanceMode:       response.PerformanceMode,
		TimeoutRecommendation: response.TimeoutRecommendation,
		TimeoutRetryPolicy:    response.TimeoutRetryPolicy,
	}, nil
}

func tileStatusForData(data []byte) string {
	if len(data) == 0 {
		return TileStatusEmpty
	}
	return TileStatusOK
}

// calculateFingerprint 计算表的 fingerprint（内部使用，对前端透明）
// 使用 common 模块的统一算法：SHA256(engineID:schema.table)
func (s *UnifiedMVTService) calculateFingerprint(engineID uint, schema, table string) string {
	// 两步计算方式：先拼接 full_name，再计算指纹
	fullName := fmt.Sprintf("%s.%s", schema, table)
	return commonModels.GenerateItemFingerprint(engineID, fullName)
}

// persistToObjectStorage 持久化瓦片到对象存储（异步执行，不阻塞响应，租户隔离）
func (s *UnifiedMVTService) persistToObjectStorage(
	ctx context.Context,
	tenantID uint,
	cacheScope string,
	storageRef string,
	z, x, y int,
	tileData []byte,
) {
	// 1. Gzip 压缩瓦片数据
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	if _, err := gzipWriter.Write(tileData); err != nil {
		logger.L().Warn("Gzip 压缩瓦片数据失败",
			"tenant_id", tenantID,
			"error", err, "cache_scope", cacheScope, "z", z, "x", x, "y", y)
		return
	}
	if err := gzipWriter.Close(); err != nil {
		logger.L().Warn("Gzip 关闭失败", "error", err)
		return
	}
	compressed := buf.Bytes()

	if err := s.spatialPreviewService.PutTileByStorageRef(ctx, tenantID, cacheScope, storageRef, z, x, y, compressed); err != nil {
		logger.L().Warn("上传瓦片到对象存储失败",
			"tenant_id", tenantID,
			"error", err, "cache_scope", cacheScope, "z", z, "x", x, "y", y)
		return
	}

	logger.L().Info("瓦片已持久化到对象存储",
		"tenant_id", tenantID,
		"cache_scope", cacheScope,
		"z", z, "x", x, "y", y,
		"compressed_size", len(compressed))
}
