package mvt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/addp/common/logger"
	"github.com/redis/go-redis/v9"
)

// ProgressTracker 进度跟踪器（使用 Redis 存储）
type ProgressTracker struct {
	redisClient *redis.Client
	fingerprint string
	startTime   time.Time
}

// NewProgressTracker 创建进度跟踪器
func NewProgressTracker(redisClient *redis.Client, fingerprint string) *ProgressTracker {
	return &ProgressTracker{
		redisClient: redisClient,
		fingerprint: fingerprint,
		startTime:   time.Now(),
	}
}

// getRedisKey 获取 Redis key
func (pt *ProgressTracker) getRedisKey() string {
	return fmt.Sprintf("manager:quick_view_progress:%s", pt.fingerprint)
}

// UpdateProgress 更新进度到 Redis
func (pt *ProgressTracker) UpdateProgress(ctx context.Context, progress *QuickViewProgress) error {
	// 计算已用时间
	progress.ElapsedSeconds = time.Since(pt.startTime).Seconds()

	// 计算预计剩余时间
	if progress.TilesProcessed > 0 && progress.ProgressPercent > 0 {
		avgTimePerPercent := progress.ElapsedSeconds / progress.ProgressPercent
		remainingPercent := 100 - progress.ProgressPercent
		progress.EstimatedRemainingSec = avgTimePerPercent * remainingPercent
	}

	data, err := json.Marshal(progress)
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}

	// 存储到 Redis，TTL 24 小时
	key := pt.getRedisKey()
	err = pt.redisClient.Set(ctx, key, data, 24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("redis set: %w", err)
	}

	logger.L().Debug("Progress updated",
		"fingerprint", pt.fingerprint,
		"status", progress.Status,
		"progress", fmt.Sprintf("%.1f%%", progress.ProgressPercent),
		"tiles", fmt.Sprintf("%d/%d", progress.TilesProcessed, progress.TilesTotalEstimate))

	return nil
}

// GetProgress 从 Redis 获取进度
func (pt *ProgressTracker) GetProgress(ctx context.Context) (*QuickViewProgress, error) {
	key := pt.getRedisKey()
	data, err := pt.redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		// Key 不存在，返回 nil（表示没有进度信息）
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}

	var progress QuickViewProgress
	if err := json.Unmarshal([]byte(data), &progress); err != nil {
		return nil, fmt.Errorf("unmarshal progress: %w", err)
	}

	return &progress, nil
}

// DeleteProgress 删除进度信息
func (pt *ProgressTracker) DeleteProgress(ctx context.Context) error {
	key := pt.getRedisKey()
	return pt.redisClient.Del(ctx, key).Err()
}

// SetStartTime 设置开始时间（用于恢复任务）
func (pt *ProgressTracker) SetStartTime(t time.Time) {
	pt.startTime = t
}

// getCancelKey 获取取消标志的 Redis key
func (pt *ProgressTracker) getCancelKey() string {
	return fmt.Sprintf("manager:quick_view_cancel:%s", pt.fingerprint)
}

// SetCancelled 设置取消标志
func (pt *ProgressTracker) SetCancelled(ctx context.Context) error {
	key := pt.getCancelKey()
	// 设置取消标志，TTL 1小时（足够长，确保Worker能检测到）
	err := pt.redisClient.Set(ctx, key, "1", 1*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("redis set cancel flag: %w", err)
	}

	logger.L().Info("Cancel flag set in Redis",
		"fingerprint", pt.fingerprint,
		"key", key)

	return nil
}

// IsCancelled 检查是否被取消
func (pt *ProgressTracker) IsCancelled(ctx context.Context) bool {
	key := pt.getCancelKey()
	val, err := pt.redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		// Key 不存在，表示未取消
		return false
	}
	if err != nil {
		logger.L().Warn("Failed to check cancel flag", "error", err)
		return false
	}

	return val == "1"
}

// ClearCancelFlag 清除取消标志
func (pt *ProgressTracker) ClearCancelFlag(ctx context.Context) error {
	key := pt.getCancelKey()
	return pt.redisClient.Del(ctx, key).Err()
}
