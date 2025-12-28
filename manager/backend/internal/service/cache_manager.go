package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/repository"
	"github.com/redis/go-redis/v9"
)

// CacheManager 统一缓存管理器
// 负责清理 Manager 模块的所有缓存（数据探查缓存、MVT 瓦片缓存等）
type CacheManager struct {
	metadataRepo *repository.MetadataRepository
	redisClient  *redis.Client
	log          *slog.Logger
}

// NewCacheManager 创建缓存管理器
func NewCacheManager(metadataRepo *repository.MetadataRepository, redisClient *redis.Client) *CacheManager {
	return &CacheManager{
		metadataRepo: metadataRepo,
		redisClient:  redisClient,
		log:          logger.With("component", "cache_manager"),
	}
}

// ClearResourceCache 清理指定资源的所有缓存
func (m *CacheManager) ClearResourceCache(ctx context.Context, engineID uint) error {
	m.log.Info("开始清理资源缓存", "engine_id", engineID)

	// 1. 清理数据探查缓存（内存缓存，如果 MetadataRepository 有缓存的话）
	if m.metadataRepo != nil {
		// MetadataRepository 目前可能没有内存缓存，这里为未来扩展预留
		m.log.Debug("清理数据探查内存缓存", "engine_id", engineID)
	}

	// 2. 清理 MVT 瓦片缓存（Redis）
	if m.redisClient != nil {
		if err := m.clearMVTCache(ctx, engineID); err != nil {
			m.log.Error("清理 MVT 缓存失败",
				"engine_id", engineID,
				"error", err)
			// 不中断，继续清理其他缓存
		}
	}

	m.log.Info("资源缓存清理完成", "engine_id", engineID)
	return nil
}

// clearMVTCache 清理 MVT 瓦片缓存
func (m *CacheManager) clearMVTCache(ctx context.Context, engineID uint) error {
	// MVT 缓存 key 模式：manager:cache:mvt:*:resource:{resource_id}:*
	pattern := fmt.Sprintf("manager:cache:mvt:*:resource:%d:*", engineID)

	m.log.Debug("扫描 MVT 缓存键", "pattern", pattern)

	// 使用 SCAN 而非 KEYS（避免阻塞 Redis）
	var cursor uint64
	deletedCount := 0

	for {
		keys, nextCursor, err := m.redisClient.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("failed to scan MVT cache keys: %w", err)
		}

		// 批量删除找到的键
		if len(keys) > 0 {
			if err := m.redisClient.Del(ctx, keys...).Err(); err != nil {
				m.log.Warn("删除部分 MVT 缓存失败",
					"keys_count", len(keys),
					"error", err)
			} else {
				deletedCount += len(keys)
			}
		}

		// 迭代完成
		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}

	m.log.Info("MVT 缓存清理完成",
		"engine_id", engineID,
		"deleted_keys", deletedCount)

	return nil
}

// ClearAllCache 清理所有资源的缓存（慎用）
func (m *CacheManager) ClearAllCache(ctx context.Context) error {
	m.log.Warn("开始清理所有资源缓存")

	if m.redisClient != nil {
		// 清理所有 MVT 缓存
		pattern := "manager:cache:mvt:*"
		var cursor uint64
		deletedCount := 0

		for {
			keys, nextCursor, err := m.redisClient.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return fmt.Errorf("failed to scan cache keys: %w", err)
			}

			if len(keys) > 0 {
				if err := m.redisClient.Del(ctx, keys...).Err(); err != nil {
					m.log.Warn("删除部分缓存失败",
						"keys_count", len(keys),
						"error", err)
				} else {
					deletedCount += len(keys)
				}
			}

			if nextCursor == 0 {
				break
			}
			cursor = nextCursor
		}

		m.log.Info("所有缓存清理完成", "deleted_keys", deletedCount)
	}

	return nil
}
