package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/mvt/internal/config"
	"github.com/redis/go-redis/v9"
)

// CacheService Redis 缓存服务
type CacheService struct {
	client *redis.Client
	ttl    time.Duration
}

// NewCacheService 创建缓存服务
func NewCacheService(cfg *config.Config) (*CacheService, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.GetRedisAddr(),
		Password: cfg.Redis.Password,
		DB:       0,
	})

	// 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &CacheService{
		client: client,
		ttl:    cfg.Redis.CacheTTL,
	}, nil
}

// GetTile 从缓存获取瓦片
func (s *CacheService) GetTile(ctx context.Context, datasourceID string, z, x, y int) ([]byte, error) {
	key := s.buildKey(datasourceID, z, x, y)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // 缓存未命中
		}
		return nil, fmt.Errorf("failed to get tile from cache: %w", err)
	}
	return data, nil
}

// SetTile 将瓦片存入缓存
func (s *CacheService) SetTile(ctx context.Context, datasourceID string, z, x, y int, data []byte) error {
	key := s.buildKey(datasourceID, z, x, y)
	err := s.client.Set(ctx, key, data, s.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to set tile to cache: %w", err)
	}
	return nil
}

// ClearDataSource 清空指定数据源的所有缓存
func (s *CacheService) ClearDataSource(ctx context.Context, datasourceID string) error {
	pattern := fmt.Sprintf("mvt:%s:*", datasourceID)

	// 使用 SCAN 命令遍历所有匹配的键
	iter := s.client.Scan(ctx, 0, pattern, 0).Iterator()
	keys := []string{}

	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	// 批量删除
	if len(keys) > 0 {
		if err := s.client.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete keys: %w", err)
		}
	}

	return nil
}

// ClearAll 清空所有缓存
func (s *CacheService) ClearAll(ctx context.Context) error {
	pattern := "mvt:*"

	iter := s.client.Scan(ctx, 0, pattern, 0).Iterator()
	keys := []string{}

	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if len(keys) > 0 {
		if err := s.client.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete keys: %w", err)
		}
	}

	return nil
}

// GetStats 获取缓存统计信息
func (s *CacheService) GetStats(ctx context.Context) (map[string]interface{}, error) {
	info := s.client.Info(ctx, "stats", "memory")
	if info.Err() != nil {
		return nil, fmt.Errorf("failed to get redis info: %w", info.Err())
	}

	// 获取键数量
	dbSize, err := s.client.DBSize(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get db size: %w", err)
	}

	stats := map[string]interface{}{
		"total_keys": dbSize,
		"info":       info.Val(),
	}

	return stats, nil
}

// buildKey 构建缓存键
func (s *CacheService) buildKey(datasourceID string, z, x, y int) string {
	return fmt.Sprintf("mvt:%s:%d:%d:%d", datasourceID, z, x, y)
}

// Close 关闭 Redis 连接
func (s *CacheService) Close() error {
	return s.client.Close()
}
