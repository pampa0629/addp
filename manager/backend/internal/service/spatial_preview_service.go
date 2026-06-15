package service

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/tilecache"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
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
func NewSpatialPreviewService(redisClient *redis.Client) *SpatialPreviewService {
	// 内存 LRU 配置（默认 8192 条目，5分钟 TTL）
	lruSize := 8192
	memTTL := 5 * time.Minute

	// 从环境变量读取配置（可选）
	if sizeStr := os.Getenv("MVT_CACHE_MEMORY_SIZE"); sizeStr != "" {
		var size int
		if _, err := fmt.Sscanf(sizeStr, "%d", &size); err == nil && size > 0 {
			lruSize = size
		}
	}
	if ttlStr := os.Getenv("MVT_CACHE_MEMORY_TTL"); ttlStr != "" {
		if d, err := time.ParseDuration(ttlStr); err == nil && d > 0 {
			memTTL = d
		}
	}

	svc := &SpatialPreviewService{
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
	bucket, objectName, err := s.tileObjectLocationFromStorageRef(storageRef, z, x, y)
	if err != nil {
		return nil, err
	}
	key := s.buildCacheKey(tenantID, cacheScope, z, x, y)
	return s.getTile(ctx, key, bucket, objectName, map[string]interface{}{
		"tenant_id":   tenantID,
		"cache_scope": cacheScope,
		"z":           z,
		"x":           x,
		"y":           y,
	})
}

func (s *SpatialPreviewService) getTile(ctx context.Context, key string, bucket string, objectName string, logFields map[string]interface{}) ([]byte, error) {
	if bucket == "" {
		bucket = s.bucket
	}

	logger.L().Info("🔍 开始查找瓦片",
		"tile", logFields,
		"cache_key", key)

	// 1️⃣ 内存 LRU 缓存（最快，1-2ms）
	if s.memEnabled {
		if data, ok := s.memCache.Get(key); ok {
			logger.L().Info("✅ 瓦片命中内存缓存",
				"tile", logFields,
				"size", len(data))
			return data, nil
		}
		logger.L().Info("❌ 内存缓存未命中", "tile", logFields)
	} else {
		logger.L().Info("⚠️  内存缓存未启用")
	}

	// 2️⃣ Redis 缓存（中等速度，3-10ms）
	if s.redisClient != nil {
		data, err := s.redisClient.Get(ctx, key).Bytes()
		if err == nil && len(data) > 0 {
			logger.L().Info("✅ 瓦片命中 Redis 缓存",
				"tile", logFields,
				"size", len(data))

			// 回填内存缓存（异步）
			if s.memEnabled {
				s.memCache.Add(key, data, s.memTTL)
			}
			return data, nil
		}
		if err != nil && err != redis.Nil {
			logger.L().Warn("Redis 读取失败", "error", err)
		}
		logger.L().Info("❌ Redis 缓存未命中", "tile", logFields)
	} else {
		logger.L().Info("⚠️  Redis 客户端未初始化")
	}

	// 3️⃣ MinIO 持久化存储（瓦片缓存结果，5-20ms）
	logger.L().Info("🔧 尝试初始化 MinIO 客户端...")
	if err := s.ensureMinIOClient(ctx); err != nil {
		logger.L().Error("❌ MinIO 客户端初始化失败", "error", err)
		return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}
	logger.L().Info("✅ MinIO 客户端已初始化")

	logger.L().Info("📦 尝试从 MinIO 读取对象",
		"tile", logFields,
		"bucket", bucket,
		"object", objectName)

	obj, err := s.minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		logger.L().Error("❌ MinIO GetObject 失败", "error", err, "object", objectName)
		return nil, fmt.Errorf("tile not found in MinIO: %w", err)
	}
	defer obj.Close()

	// 检查对象状态
	objInfo, err := obj.Stat()
	if err != nil {
		logger.L().Error("❌ MinIO 对象 Stat 失败", "error", err, "object", objectName)
		return nil, fmt.Errorf("failed to stat MinIO object: %w", err)
	}
	logger.L().Info("📊 MinIO 对象信息", "size", objInfo.Size, "content_type", objInfo.ContentType)

	// 读取全部数据
	data, err := io.ReadAll(obj)
	if err != nil {
		logger.L().Error("❌ 读取 MinIO 数据失败", "error", err)
		return nil, fmt.Errorf("failed to read tile data: %w", err)
	}

	if len(data) == 0 {
		logger.L().Warn("⚠️  从 MinIO 读取的数据为空!", "object", objectName)
		return nil, fmt.Errorf("empty data read from MinIO")
	}

	logger.L().Info("✅ 瓦片从 MinIO 加载成功",
		"tile", logFields,
		"size", len(data))

	// 回填上层缓存（异步，不阻塞响应）
	go s.backfillCache(context.Background(), key, data)

	return data, nil
}

func (s *SpatialPreviewService) PutTileByStorageRef(ctx context.Context, tenantID uint, cacheScope string, storageRef string, z, x, y int, compressed []byte) error {
	bucket, objectName, err := s.tileObjectLocationFromStorageRef(storageRef, z, x, y)
	if err != nil {
		return err
	}
	if err := s.ensureMinIOClient(ctx); err != nil {
		return fmt.Errorf("failed to initialize MinIO client: %w", err)
	}
	_, err = s.minioClient.PutObject(
		ctx,
		bucket,
		objectName,
		bytes.NewReader(compressed),
		int64(len(compressed)),
		minio.PutObjectOptions{
			ContentType:     "application/vnd.mapbox-vector-tile",
			ContentEncoding: "gzip",
		},
	)
	if err != nil {
		return err
	}
	cacheKey := s.buildCacheKey(tenantID, cacheScope, z, x, y)
	s.backfillCache(ctx, cacheKey, compressed)
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
		"tile_cache_id", tileCacheID,
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
	return fmt.Sprintf("manager:tenant_%d:cache:mvt:spatial:tile_cache:%d:", tenantID, tileCacheID)
}

func (s *SpatialPreviewService) tileObjectLocationFromStorageRef(storageRef string, z, x, y int) (string, string, error) {
	return tilecache.TileObjectLocation(storageRef, s.bucket, z, x, y)
}

// ensureMinIOClient 确保 MinIO 客户端已初始化
func (s *SpatialPreviewService) ensureMinIOClient(ctx context.Context) error {
	if s.minioClient != nil {
		return nil
	}

	// MVT 瓦片存储在系统 MinIO（不是业务 MinIO）
	endpoint := os.Getenv("MINIO_SYSTEM_ENDPOINT")
	if endpoint == "" {
		// 动态读取 MinIO API 端口，与 config.go 保持一致
		minioPort := os.Getenv("MINIO_API_PORT")
		if minioPort == "" {
			minioPort = "9000"
		}
		endpoint = fmt.Sprintf("localhost:%s", minioPort)
	}

	accessKey := os.Getenv("MINIO_SYSTEM_ACCESS_KEY")
	if accessKey == "" {
		accessKey = os.Getenv("MINIO_ROOT_USER") // 回退到系统 MinIO 配置
		if accessKey == "" {
			accessKey = "minioadmin"
		}
	}

	secretKey := os.Getenv("MINIO_SYSTEM_SECRET_KEY")
	if secretKey == "" {
		secretKey = os.Getenv("MINIO_ROOT_PASSWORD") // 回退到系统 MinIO 配置
		if secretKey == "" {
			secretKey = "minioadmin"
		}
	}

	// 初始化 MinIO 客户端
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false, // 开发环境使用 HTTP
	})
	if err != nil {
		return fmt.Errorf("failed to create MinIO client: %w", err)
	}

	s.minioClient = client
	logger.L().Info("MinIO client initialized for spatial preview", "endpoint", endpoint)
	return nil
}
