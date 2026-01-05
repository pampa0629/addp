package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ScanDedupService 扫描去重服务
type ScanDedupService struct {
	redis *redis.Client
}

// NewScanDedupService 创建扫描去重服务
func NewScanDedupService(redis *redis.Client) *ScanDedupService {
	return &ScanDedupService{redis: redis}
}

// GenerateTaskKey 生成任务唯一标识
// 格式: meta:cache:scan_task:{tenant_id}:{engine_id}:{scan_type}
func (s *ScanDedupService) GenerateTaskKey(tenantID, engineID uint, scanType string) string {
	return fmt.Sprintf("meta:cache:scan_task:%d:%d:%s", tenantID, engineID, scanType)
}

// CheckTaskExists 检查任务是否正在执行
func (s *ScanDedupService) CheckTaskExists(ctx context.Context, taskKey string) bool {
	exists, err := s.redis.Exists(ctx, taskKey).Result()
	if err != nil {
		return false
	}
	return exists > 0
}

// MarkTaskRunning 标记任务正在执行
// ttl: 任务超时时间，建议 2 小时
func (s *ScanDedupService) MarkTaskRunning(ctx context.Context, taskKey string, ttl time.Duration) error {
	return s.redis.Set(ctx, taskKey, time.Now().Unix(), ttl).Err()
}

// ClearTask 清除任务标记
func (s *ScanDedupService) ClearTask(ctx context.Context, taskKey string) error {
	return s.redis.Del(ctx, taskKey).Err()
}

// GetLastScanTime 获取上次扫描时间
func (s *ScanDedupService) GetLastScanTime(ctx context.Context, engineID uint) (*time.Time, error) {
	key := fmt.Sprintf("meta:cache:scan_last_time:%d", engineID)
	val, err := s.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	timestamp, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return nil, err
	}
	return &timestamp, nil
}

// UpdateLastScanTime 更新上次扫描时间
func (s *ScanDedupService) UpdateLastScanTime(ctx context.Context, engineID uint) error {
	key := fmt.Sprintf("meta:cache:scan_last_time:%d", engineID)
	return s.redis.Set(ctx, key, time.Now().Format(time.RFC3339), 0).Err()
}

// GenerateSchemaLockKey 生成Schema级锁key
// 格式: meta:scan:lock:tenant:{tenant_id}:engine:{engine_id}:schema:{schema_name}
func (s *ScanDedupService) GenerateSchemaLockKey(tenantID, engineID uint, schemaName string) string {
	return fmt.Sprintf("meta:scan:lock:tenant:%d:engine:%d:schema:%s",
		tenantID, engineID, schemaName)
}

// GenerateBucketLockKey 生成Bucket级锁key
// 格式: meta:scan:lock:tenant:{tenant_id}:engine:{engine_id}:bucket:{bucket_path}
// 路径中的特殊字符会被清理（/ 和 : 替换为 _）
func (s *ScanDedupService) GenerateBucketLockKey(tenantID, engineID uint, bucketPath string) string {
	// 路径清理：去除特殊字符
	cleanPath := bucketPath
	cleanPath = replaceAll(cleanPath, "/", "_")
	cleanPath = replaceAll(cleanPath, ":", "_")
	return fmt.Sprintf("meta:scan:lock:tenant:%d:engine:%d:bucket:%s",
		tenantID, engineID, cleanPath)
}

// replaceAll 辅助函数，替换字符串中的所有匹配
func replaceAll(s, old, new string) string {
	result := s
	for i := 0; i < len(result); i++ {
		if i+len(old) <= len(result) && result[i:i+len(old)] == old {
			result = result[:i] + new + result[i+len(old):]
			i += len(new) - 1
		}
	}
	return result
}
