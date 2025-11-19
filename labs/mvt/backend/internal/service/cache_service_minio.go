package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"container/list"
	"time"

	"github.com/addp/mvt/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// CacheService 3层缓存：内存 LRU → Redis LRU → MinIO
// - Memory LRU: 8192 entries, 1-2ms, 热点瓦片
// - Redis: 2GB max, 24h TTL, allkeys-lru 自动驱逐, 3-10ms
// - MinIO: 无限容量, 持久化, 5-20ms (替代 PostgreSQL mvt_cache 表)
type CacheService struct {
	// in-memory LRU of gzipped tiles
	mem    *lruCache
	// Redis client (stores gzipped tiles)
	rdb    *redis.Client
	// MinIO client for persistent cache
	minio  *minio.Client
	bucket string
	// TTL for Redis
	ttl    time.Duration
}

const (
	defaultLRUSize = 8192 // 扩大内存缓存（从 2048 提升）
)

// 简易 LRU 实现（键为 string，值为 []byte）
type lruEntry struct {
	key   string
	value []byte
}

type lruCache struct {
	cap  int
	ll   *list.List
	dict map[string]*list.Element
}

func newLRU(capacity int) *lruCache {
	if capacity <= 0 { capacity = 1 }
	return &lruCache{cap: capacity, ll: list.New(), dict: make(map[string]*list.Element)}
}
func (c *lruCache) Get(key string) ([]byte, bool) {
	if ele, ok := c.dict[key]; ok {
		c.ll.MoveToFront(ele)
		return ele.Value.(lruEntry).value, true
	}
	return nil, false
}
func (c *lruCache) Add(key string, val []byte) {
	if ele, ok := c.dict[key]; ok {
		ele.Value = lruEntry{key: key, value: val}
		c.ll.MoveToFront(ele)
		return
	}
	ele := c.ll.PushFront(lruEntry{key: key, value: val})
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
func (c *lruCache) Size() int {
	return c.ll.Len()
}

// NewCacheService 创建缓存服务（Memory + Redis + MinIO）
func NewCacheService(cfg *config.Config) (*CacheService, error) {
	// Memory LRU (使用配置中的大小，默认 8192)
	lruSize := defaultLRUSize
	if cfg.CachePolicy.MemoryLRUSize > 0 {
		lruSize = cfg.CachePolicy.MemoryLRUSize
	}
	mem := newLRU(lruSize)
	log.Printf("[INFO] Memory LRU initialized with size: %d", lruSize)

	// Redis (ADDP 系统 Redis)
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.GetRedisAddr(),
		Password: cfg.Redis.Password,
		DB:       0,
	})

	// verify redis
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}
	log.Printf("[INFO] Connected to ADDP Redis: %s", cfg.GetRedisAddr())

	// MinIO (ADDP 系统 MinIO，替代 PostgreSQL)
	minioClient, err := minio.New(cfg.MinIO.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, ""),
		Secure: cfg.MinIO.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 确保 bucket 存在
	bucket := cfg.MinIO.Bucket
	exists, err := minioClient.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to check minio bucket: %w", err)
	}
	if !exists {
		log.Printf("[INFO] Creating MinIO bucket: %s", bucket)
		if err := minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("failed to create minio bucket: %w", err)
		}
	}
	log.Printf("[INFO] Connected to ADDP MinIO: %s, bucket: %s", cfg.MinIO.Endpoint, bucket)

	svc := &CacheService{
		mem:    mem,
		rdb:    rdb,
		minio:  minioClient,
		bucket: bucket,
		ttl:    cfg.Redis.CacheTTL,
	}

	return svc, nil
}

// GetTile 从缓存获取（内存 → Redis → MinIO），返回 gzip 压缩的 MVT
func (s *CacheService) GetTile(ctx context.Context, datasourceID string, z, x, y int) ([]byte, error) {
	key := s.buildKey(datasourceID, z, x, y)

	// 1. Memory LRU
	if v, ok := s.mem.Get(key); ok {
		return v, nil
	}

	// 2. Redis
	data, err := s.rdb.Get(ctx, key).Bytes()
	if err == nil && len(data) > 0 {
		s.mem.Add(key, data) // 回填内存
		return data, nil
	}
	if err != nil && err != redis.Nil {
		log.Printf("[WARN] Redis get error: %v", err)
	}

	// 3. MinIO 持久存储
	objectPath := s.buildObjectPath(datasourceID, z, x, y)
	obj, err := s.minio.GetObject(ctx, s.bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		// MinIO miss - 返回 nil 表示需要生成
		return nil, nil
	}
	defer obj.Close()

	gz, err := io.ReadAll(obj)
	if err != nil {
		log.Printf("[WARN] MinIO read error: %v", err)
		return nil, nil
	}

	// 回填上层缓存 (异步，不阻塞响应)
	go func() {
		ctx := context.Background()
		s.mem.Add(key, gz)
		_ = s.rdb.Set(ctx, key, gz, s.ttl).Err()
	}()

	return gz, nil
}

// SetTileOptions 写入选项
type SetTileOptions struct {
	PersistToMinIO bool  // 是否写入 MinIO 持久缓存
	GenTimeMs      int64 // PostGIS 生成耗时（毫秒）
}

// SetTileWithOptions 将瓦片写入缓存（总是写内存与 Redis；可选 MinIO）
func (s *CacheService) SetTileWithOptions(ctx context.Context, datasourceID string, z, x, y int, gzipped []byte, opt SetTileOptions) error {
	key := s.buildKey(datasourceID, z, x, y)

	// 1. 总是写入内存和 Redis
	s.mem.Add(key, gzipped)
	if err := s.rdb.Set(ctx, key, gzipped, s.ttl).Err(); err != nil {
		log.Printf("[WARN] Redis set error: %v", err)
	}

	// 2. 可选持久化到 MinIO (异步，不阻塞)
	if opt.PersistToMinIO {
		go func() {
			ctx := context.Background()
			objectPath := s.buildObjectPath(datasourceID, z, x, y)

			// 构建元数据
			metadata := map[string]string{
				"datasource":    datasourceID,
				"z":             fmt.Sprintf("%d", z),
				"x":             fmt.Sprintf("%d", x),
				"y":             fmt.Sprintf("%d", y),
				"encoding":      "gzip",
				"created_at":    time.Now().Format(time.RFC3339),
				"size_gzip":     fmt.Sprintf("%d", len(gzipped)),
			}
			if opt.GenTimeMs > 0 {
				metadata["generation_ms"] = fmt.Sprintf("%d", opt.GenTimeMs)
			}

			_, err := s.minio.PutObject(ctx, s.bucket, objectPath,
				bytes.NewReader(gzipped), int64(len(gzipped)),
				minio.PutObjectOptions{
					ContentType:  "application/vnd.mapbox-vector-tile",
					UserMetadata: metadata,
				})
			if err != nil {
				log.Printf("[ERROR] MinIO upload failed: %v", err)
			} else {
				log.Printf("[DEBUG] MinIO cached: %s (size=%d, gen=%dms)", objectPath, len(gzipped), opt.GenTimeMs)
			}
		}()
	}

	return nil
}

// SetTile 兼容旧接口：总是写内存与 Redis，并持久化到 MinIO
func (s *CacheService) SetTile(ctx context.Context, datasourceID string, z, x, y int, gzipped []byte) error {
	return s.SetTileWithOptions(ctx, datasourceID, z, x, y, gzipped, SetTileOptions{PersistToMinIO: true})
}

// Gzip 压缩帮助（输入原始 mvt，输出 gzip）
func (s *CacheService) Gzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		_ = gw.Close()
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return io.ReadAll(bytes.NewReader(buf.Bytes()))
}

// ClearDataSource 清空指定数据源的所有缓存
func (s *CacheService) ClearDataSource(ctx context.Context, datasourceID string) error {
	// 1. Redis
	pattern := fmt.Sprintf("mvt:%s:*", datasourceID)
	iter := s.rdb.Scan(ctx, 0, pattern, 0).Iterator()
	keys := []string{}
	for iter.Next(ctx) { keys = append(keys, iter.Val()) }
	if err := iter.Err(); err != nil { return fmt.Errorf("failed to scan redis keys: %w", err) }
	if len(keys) > 0 {
		if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
			log.Printf("[WARN] Failed to delete redis keys: %v", err)
		}
	}

	// 2. Memory (简单重建)
	s.mem = newLRU(s.mem.cap)

	// 3. MinIO (删除整个前缀)
	prefix := datasourceID + "/"
	objectCh := s.minio.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	for obj := range objectCh {
		if obj.Err != nil {
			return fmt.Errorf("failed to list minio objects: %w", obj.Err)
		}
		if err := s.minio.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			log.Printf("[WARN] Failed to remove minio object %s: %v", obj.Key, err)
		}
	}

	log.Printf("[INFO] Cleared cache for datasource: %s", datasourceID)
	return nil
}

// ClearAll 清空所有缓存
func (s *CacheService) ClearAll(ctx context.Context) error {
	// 1. Redis
	iter := s.rdb.Scan(ctx, 0, "mvt:*", 0).Iterator()
	keys := []string{}
	for iter.Next(ctx) { keys = append(keys, iter.Val()) }
	if err := iter.Err(); err != nil { return fmt.Errorf("failed to scan redis keys: %w", err) }
	if len(keys) > 0 {
		if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
			log.Printf("[WARN] Failed to delete redis keys: %v", err)
		}
	}

	// 2. Memory
	s.mem = newLRU(s.mem.cap)

	// 3. MinIO (删除整个 bucket 内容 - 谨慎操作！)
	objectCh := s.minio.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Recursive: true})
	for obj := range objectCh {
		if obj.Err != nil {
			return fmt.Errorf("failed to list minio objects: %w", obj.Err)
		}
		if err := s.minio.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			log.Printf("[WARN] Failed to remove minio object %s: %v", obj.Key, err)
		}
	}

	log.Printf("[INFO] Cleared all cache (memory + redis + minio)")
	return nil
}

// GetStats 获取缓存统计信息
func (s *CacheService) GetStats(ctx context.Context) (map[string]interface{}, error) {
	// Redis stats
	info := s.rdb.Info(ctx, "stats", "memory")
	if info.Err() != nil {
		return nil, fmt.Errorf("failed to get redis info: %w", info.Err())
	}
	dbSize, err := s.rdb.DBSize(ctx).Result()
	if err != nil { return nil, fmt.Errorf("failed to get redis db size: %w", err) }

	// MinIO stats (count objects)
	var minioCount int64
	objectCh := s.minio.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Recursive: true})
	for obj := range objectCh {
		if obj.Err != nil {
			log.Printf("[WARN] Failed to count minio objects: %v", obj.Err)
			break
		}
		minioCount++
	}

	stats := map[string]interface{}{
		"memory_lru_size":     s.mem.cap,
		"memory_lru_used":     s.mem.Size(),
		"redis_keys":          dbSize,
		"redis_info":          info.Val(),
		"minio_objects_count": minioCount,
		"minio_bucket":        s.bucket,
	}
	return stats, nil
}

// buildKey 构建缓存键（用于内存和 Redis）
func (s *CacheService) buildKey(datasourceID string, z, x, y int) string {
	return fmt.Sprintf("mvt:%s:%d:%d:%d", datasourceID, z, x, y)
}

// buildObjectPath 构建 MinIO 对象路径
// 格式: {datasource}/z{z}/{x}_{y}.mvt.gz
func (s *CacheService) buildObjectPath(datasourceID string, z, x, y int) string {
	return fmt.Sprintf("%s/z%d/%d_%d.mvt.gz", datasourceID, z, x, y)
}

// Close 关闭连接
func (s *CacheService) Close() error {
	if s.rdb != nil {
		return s.rdb.Close()
	}
	return nil
}
