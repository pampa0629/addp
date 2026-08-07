package service

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/logger"
	commonPMTiles "github.com/addp/common/pmtiles"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/tilecache"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
)

// SpatialPreviewService MVT 空间数据预览服务
// 实现三层缓存：内存 LRU → Redis → 对象存储
type SpatialPreviewService struct {
	// MinIO 客户端（持久化存储，瓦片缓存结果）
	minioClient *minio.Client
	bucket      string

	// Redis 客户端（中间层缓存，24h TTL）
	redisClient *redis.Client
	redisTTL    time.Duration

	// 内存 LRU 缓存（热点瓦片，5分钟过期）
	memCache   *lruCache
	memTTL     time.Duration
	memEnabled bool
}

// lruEntry 内存缓存条目
type lruEntry struct {
	key       string
	value     []byte
	expiresAt time.Time
}

// lruCache 简易 LRU 实现（带过期时间）
type lruCache struct {
	cap  int
	ll   *list.List
	dict map[string]*list.Element
	mu   sync.RWMutex
}

func newLRU(capacity int) *lruCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &lruCache{
		cap:  capacity,
		ll:   list.New(),
		dict: make(map[string]*list.Element),
	}
}

func (c *lruCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ele, ok := c.dict[key]; ok {
		entry := ele.Value.(lruEntry)
		// 检查是否过期
		if time.Now().After(entry.expiresAt) {
			// 过期，删除
			delete(c.dict, key)
			c.ll.Remove(ele)
			return nil, false
		}
		c.ll.MoveToFront(ele)
		return entry.value, true
	}
	return nil, false
}

func (c *lruCache) Add(key string, val []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiresAt := time.Now().Add(ttl)
	entry := lruEntry{key: key, value: val, expiresAt: expiresAt}

	if ele, ok := c.dict[key]; ok {
		ele.Value = entry
		c.ll.MoveToFront(ele)
		return
	}

	ele := c.ll.PushFront(entry)
	c.dict[key] = ele

	if c.ll.Len() > c.cap {
		last := c.ll.Back()
		if last != nil {
			kv := last.Value.(lruEntry)
			delete(c.dict, kv.key)
			c.ll.Remove(last)
		}
	}
}

func (c *lruCache) DeletePrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	deleted := 0
	for key, ele := range c.dict {
		if strings.HasPrefix(key, prefix) {
			delete(c.dict, key)
			c.ll.Remove(ele)
			deleted++
		}
	}
	return deleted
}

func (c *lruCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ll.Len()
}

// NewSpatialPreviewService 创建空间预览服务（带多层缓存）
// redisClient 可选，如果为 nil 则跳过 Redis 缓存层
func NewSpatialPreviewService(redisClient *redis.Client, minioClient *minio.Client) *SpatialPreviewService {
	// 内存 LRU 配置（默认 8192 条目，5分钟 TTL）
	lruSize := 8192
	memTTL := 5 * time.Minute

	svc := &SpatialPreviewService{
		minioClient: minioClient,
		bucket:      "manager",
		redisClient: redisClient,
		redisTTL:    24 * time.Hour, // Redis 缓存 24 小时
		memCache:    newLRU(lruSize),
		memTTL:      memTTL,
		memEnabled:  true, // 默认启用内存缓存
	}

	logger.L().Info("SpatialPreviewService 初始化完成",
		"memory_lru_size", lruSize,
		"memory_ttl", memTTL,
		"redis_enabled", redisClient != nil,
		"redis_ttl", svc.redisTTL)

	return svc
}

func (s *SpatialPreviewService) GetTileByStorageRef(ctx context.Context, tenantID uint, cacheScope string, storageRef string, z, x, y int) ([]byte, error) {
	key := s.buildCacheKey(tenantID, cacheScope, z, x, y)
	if s.memEnabled {
		if data, ok := s.memCache.Get(key); ok {
			return data, nil
		}
	}
	if s.redisClient != nil {
		data, err := s.redisClient.Get(ctx, key).Bytes()
		if err == nil && len(data) > 0 {
			if s.memEnabled {
				s.memCache.Add(key, data, s.memTTL)
			}
			return data, nil
		}
		if err != nil && err != redis.Nil {
			logger.L().Warn("读取瓦片 Redis 缓存失败", "error", err)
		}
	}
	data, err := s.readPMTilesTile(ctx, storageRef, z, x, y)
	if errors.Is(err, commonPMTiles.ErrTileNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.backfillCache(ctx, key, data)
	return data, nil
}

func (s *SpatialPreviewService) readPMTilesTile(ctx context.Context, storageRef string, z, x, y int) ([]byte, error) {
	if z < 0 || z > 31 || x < 0 || y < 0 {
		return nil, commonPMTiles.ErrTileNotFound
	}
	bucket, objectName, err := tilecache.ObjectLocation(storageRef, s.bucket)
	if err != nil {
		return nil, err
	}
	if err := s.ensureMinIOClient(ctx); err != nil {
		return nil, fmt.Errorf("initialize PMTiles object store: %w", err)
	}
	info, err := s.minioClient.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("stat PMTiles object: %w", err)
	}
	readRange := func(ctx context.Context, offset, length int64) ([]byte, error) {
		if offset < 0 || length <= 0 || offset > info.Size || length > info.Size-offset {
			return nil, fmt.Errorf("invalid PMTiles range offset=%d length=%d size=%d", offset, length, info.Size)
		}
		opts := minio.GetObjectOptions{}
		if err := opts.SetRange(offset, offset+length-1); err != nil {
			return nil, err
		}
		obj, err := s.minioClient.GetObject(ctx, bucket, objectName, opts)
		if err != nil {
			return nil, err
		}
		defer obj.Close()
		data, err := io.ReadAll(io.LimitReader(obj, length))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != length {
			return nil, io.ErrUnexpectedEOF
		}
		return data, nil
	}
	headerData, err := readRange(ctx, 0, commonPMTiles.HeaderSize)
	if err != nil {
		return nil, fmt.Errorf("read PMTiles header: %w", err)
	}
	header, err := commonPMTiles.ParseHeaderBytes(headerData)
	if err != nil {
		return nil, err
	}
	if err := commonPMTiles.ValidateHeader(header, info.Size); err != nil {
		return nil, err
	}
	archive, err := commonPMTiles.NewArchive(header, readRange)
	if err != nil {
		return nil, err
	}
	return archive.GetTile(ctx, uint8(z), uint32(x), uint32(y))
}

func (s *SpatialPreviewService) OpenRasterCOG(ctx context.Context, storageRef string, rangeHeader string) (io.ReadCloser, int64, string, string, error) {
	bucket, objectName, err := rastercogref.ObjectLocation(storageRef, s.bucket)
	if err != nil {
		return nil, 0, "", "", err
	}
	if err := s.ensureMinIOClient(ctx); err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to initialize MinIO client: %w", err)
	}
	objInfo, err := s.minioClient.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to stat raster COG object: %w", err)
	}
	opts, contentLength, contentRange, err := parseStorageRange(rangeHeader, objInfo.Size)
	if err != nil {
		return nil, 0, "", "", err
	}
	getOpts := minio.GetObjectOptions{}
	if opts.Length > 0 && contentRange != "" {
		if err := getOpts.SetRange(opts.Offset, opts.Offset+opts.Length-1); err != nil {
			return nil, 0, "", "", fmt.Errorf("failed to set raster COG range: %w", err)
		}
	}
	obj, err := s.minioClient.GetObject(ctx, bucket, objectName, getOpts)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to open raster COG object: %w", err)
	}
	contentType := objInfo.ContentType
	if contentType == "" {
		contentType = "image/tiff"
	}
	return obj, contentLength, contentRange, contentType, nil
}

func (s *SpatialPreviewService) DeleteByStorageRef(ctx context.Context, storageRef string) error {
	bucket, objectName, err := tilecache.ObjectLocation(storageRef, s.bucket)
	if err != nil {
		return err
	}
	if err := s.ensureMinIOClient(ctx); err != nil {
		return fmt.Errorf("initialize PMTiles object store: %w", err)
	}
	if err := s.minioClient.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete PMTiles object %q: %w", objectName, err)
	}
	return nil
}

func (s *SpatialPreviewService) InvalidateTileCacheRuntimeCache(ctx context.Context, tenantID uint, tileCacheID uint) error {
	if tileCacheID == 0 {
		return nil
	}
	prefix := s.buildTileCacheRuntimeCachePrefix(tenantID, tileCacheID)
	memDeleted := 0
	if s.memEnabled {
		memDeleted = s.memCache.DeletePrefix(prefix)
	}

	redisDeleted := 0
	if s.redisClient != nil {
		var cursor uint64
		pattern := prefix + "*"
		for {
			keys, nextCursor, err := s.redisClient.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return fmt.Errorf("scan tile cache runtime cache keys: %w", err)
			}
			if len(keys) > 0 {
				deleted, err := s.redisClient.Del(ctx, keys...).Result()
				if err != nil {
					return fmt.Errorf("delete tile cache runtime cache keys: %w", err)
				}
				redisDeleted += int(deleted)
			}
			if nextCursor == 0 {
				break
			}
			cursor = nextCursor
		}
	}

	logger.L().Info("瓦片运行时缓存已失效",
		"tenant_id", tenantID,
		"vector_tile_cache_id", tileCacheID,
		"memory_deleted", memDeleted,
		"redis_deleted", redisDeleted)
	return nil
}

// backfillCache 异步回填上层缓存
func (s *SpatialPreviewService) backfillCache(ctx context.Context, key string, data []byte) {
	// 写入内存缓存
	if s.memEnabled {
		s.memCache.Add(key, data, s.memTTL)
	}

	// 写入 Redis 缓存
	if s.redisClient != nil {
		if err := s.redisClient.Set(ctx, key, data, s.redisTTL).Err(); err != nil {
			logger.L().Warn("Redis 写入失败", "error", err)
		}
	}
}

// GetStats 获取缓存统计信息
func (s *SpatialPreviewService) GetStats(ctx context.Context) map[string]interface{} {
	stats := map[string]interface{}{
		"memory_lru_capacity": s.memCache.cap,
		"memory_lru_size":     s.memCache.Size(),
		"memory_enabled":      s.memEnabled,
		"memory_ttl":          s.memTTL.String(),
		"redis_enabled":       s.redisClient != nil,
		"redis_ttl":           s.redisTTL.String(),
		"minio_bucket":        s.bucket,
	}

	// Redis 统计（如果可用）
	if s.redisClient != nil {
		dbSize, err := s.redisClient.DBSize(ctx).Result()
		if err == nil {
			stats["redis_total_keys"] = dbSize
		}
	}

	return stats
}

// buildCacheKey 构建缓存键（用于内存和 Redis，租户隔离）
func (s *SpatialPreviewService) buildCacheKey(tenantID uint, cacheScope string, z, x, y int) string {
	return fmt.Sprintf("manager:tenant_%d:cache:mvt:spatial:%s:%d:%d:%d", tenantID, cacheScope, z, x, y)
}

func (s *SpatialPreviewService) buildTileCacheRuntimeCachePrefix(tenantID uint, tileCacheID uint) string {
	return fmt.Sprintf("manager:tenant_%d:cache:mvt:spatial:vector_tile_cache:%d:", tenantID, tileCacheID)
}

// ensureMinIOClient 确保 MinIO 客户端已初始化
func (s *SpatialPreviewService) ensureMinIOClient(ctx context.Context) error {
	if s.minioClient != nil {
		return nil
	}
	return errors.New("infra MinIO client is not configured")
}
