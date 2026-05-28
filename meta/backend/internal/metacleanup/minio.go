package metacleanup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/meta/internal/models"
	"github.com/minio/minio-go/v7"
)

type MinIOGarbageStats struct {
	TotalCount     int
	TotalSizeBytes int64
	TotalSizeMB    float64
	ByBucket       map[string]int
	Samples        []models.MinIOObjectInfo
}

type MinIOCleanupResult struct {
	DeletedObjects int
	FreedSpaceMB   float64
}

type MinIOCleaner struct {
	client *minio.Client
	log    *slog.Logger
}

func NewMinIOCleaner(client *minio.Client, log *slog.Logger) *MinIOCleaner {
	return &MinIOCleaner{client: client, log: log}
}

func (c *MinIOCleaner) Enabled() bool {
	return c != nil && c.client != nil
}

func (c *MinIOCleaner) ScanGarbage(ctx context.Context, invalidFingerprints []string) (*MinIOGarbageStats, error) {
	stats := &MinIOGarbageStats{
		ByBucket: make(map[string]int),
		Samples:  []models.MinIOObjectInfo{},
	}
	if !c.Enabled() {
		if c.log != nil {
			c.log.Warn("MinIO 客户端未配置，跳过 MinIO 垃圾扫描")
		}
		return stats, nil
	}

	invalidFingerprintSet := stringSet(invalidFingerprints)

	systemStats := c.scanBucket(ctx, "system", func(key string, size int64, modified time.Time) (bool, string) {
		if strings.HasPrefix(key, "audit-logs/") && time.Since(modified) > 30*24*time.Hour {
			return true, "超过30天"
		}
		return false, ""
	})
	stats.addBucket("system", systemStats)

	managerStats := c.scanBucket(ctx, "manager", func(key string, size int64, modified time.Time) (bool, string) {
		if strings.HasPrefix(key, "mvt-tiles/") {
			parts := strings.Split(key, "/")
			if len(parts) >= 2 && invalidFingerprintSet[parts[1]] {
				return true, "引擎已删除"
			}
		}
		return false, ""
	})
	stats.addBucket("manager", managerStats)
	stats.TotalSizeMB = float64(stats.TotalSizeBytes) / (1024 * 1024)

	return stats, nil
}

func (c *MinIOCleaner) ExecuteCleanup(ctx context.Context, invalidFingerprints []string) (*MinIOCleanupResult, error) {
	result := &MinIOCleanupResult{}
	if !c.Enabled() {
		if c.log != nil {
			c.log.Warn("MinIO 客户端未配置，跳过 MinIO 清理")
		}
		return result, nil
	}

	invalidFingerprintSet := stringSet(invalidFingerprints)
	var totalFreedBytes int64

	systemDeleted, systemFreed := c.deleteBucketObjects(ctx, "system", func(key string, size int64, modified time.Time) bool {
		return strings.HasPrefix(key, "audit-logs/") && time.Since(modified) > 30*24*time.Hour
	})
	result.DeletedObjects += systemDeleted
	totalFreedBytes += systemFreed

	managerDeleted, managerFreed := c.deleteBucketObjects(ctx, "manager", func(key string, size int64, modified time.Time) bool {
		if strings.HasPrefix(key, "mvt-tiles/") {
			parts := strings.Split(key, "/")
			return len(parts) >= 2 && invalidFingerprintSet[parts[1]]
		}
		return false
	})
	result.DeletedObjects += managerDeleted
	totalFreedBytes += managerFreed
	result.FreedSpaceMB = float64(totalFreedBytes) / (1024 * 1024)

	return result, nil
}

func (c *MinIOCleaner) DeleteMVTByFingerprints(ctx context.Context, engineID uint, fingerprints []string) (int, int64) {
	if !c.Enabled() || len(fingerprints) == 0 {
		return 0, 0
	}

	if c.log != nil {
		c.log.Info("开始清理引擎 MVT 瓦片", "engine_id", engineID, "fingerprint_count", len(fingerprints))
	}

	var totalDeleted int
	var totalFreedBytes int64
	for _, fingerprint := range fingerprints {
		deleted, freed := c.deleteMVTByFingerprint(ctx, fingerprint)
		totalDeleted += deleted
		totalFreedBytes += freed
	}

	if c.log != nil {
		c.log.Info("引擎 MVT 瓦片清理完成",
			"engine_id", engineID,
			"deleted_objects", totalDeleted,
			"freed_mb", float64(totalFreedBytes)/(1024*1024))
	}
	return totalDeleted, totalFreedBytes
}

func (c *MinIOCleaner) scanBucket(ctx context.Context, bucket string, isGarbage func(key string, size int64, modified time.Time) (bool, string)) *MinIOGarbageStats {
	stats := &MinIOGarbageStats{
		ByBucket: make(map[string]int),
		Samples:  []models.MinIOObjectInfo{},
	}

	objectCh := c.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})
	for object := range objectCh {
		if object.Err != nil {
			if c.log != nil {
				c.log.Error("列出对象失败", "bucket", bucket, "error", object.Err)
			}
			continue
		}
		if isGarbage, reason := isGarbage(object.Key, object.Size, object.LastModified); isGarbage {
			stats.TotalCount++
			stats.TotalSizeBytes += object.Size
			if len(stats.Samples) < 10 {
				stats.Samples = append(stats.Samples, models.MinIOObjectInfo{
					Bucket:   bucket,
					Key:      object.Key,
					Size:     object.Size,
					Modified: object.LastModified,
					Reason:   reason,
				})
			}
		}
	}
	return stats
}

func (c *MinIOCleaner) deleteBucketObjects(ctx context.Context, bucket string, shouldDelete func(key string, size int64, modified time.Time) bool) (int, int64) {
	var freedBytes int64
	var objectsToDelete []string

	objectCh := c.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})
	for object := range objectCh {
		if object.Err != nil {
			if c.log != nil {
				c.log.Error("列出对象失败", "bucket", bucket, "error", object.Err)
			}
			continue
		}
		if shouldDelete(object.Key, object.Size, object.LastModified) {
			objectsToDelete = append(objectsToDelete, object.Key)
			freedBytes += object.Size
		}
	}

	deletedCount := c.removeObjects(ctx, bucket, objectsToDelete, "删除对象失败")
	if deletedCount > 0 && c.log != nil {
		c.log.Info("MinIO bucket 清理完成",
			"bucket", bucket,
			"deleted_count", deletedCount,
			"freed_bytes", freedBytes)
	}
	return deletedCount, freedBytes
}

func (c *MinIOCleaner) deleteMVTByFingerprint(ctx context.Context, fingerprint string) (int, int64) {
	bucket := "manager"
	prefix := fmt.Sprintf("mvt-tiles/%s/", fingerprint)

	var freedBytes int64
	var objectsToDelete []string

	objectCh := c.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	for object := range objectCh {
		if object.Err != nil {
			if c.log != nil {
				c.log.Error("列出对象失败", "bucket", bucket, "prefix", prefix, "error", object.Err)
			}
			continue
		}
		objectsToDelete = append(objectsToDelete, object.Key)
		freedBytes += object.Size
	}

	deletedCount := c.removeObjects(ctx, bucket, objectsToDelete, "删除 MVT 瓦片失败")
	if deletedCount > 0 && c.log != nil {
		c.log.Info("MVT 瓦片已删除",
			"fingerprint", fingerprint,
			"deleted_count", deletedCount,
			"freed_bytes", freedBytes)
	}
	return deletedCount, freedBytes
}

func (c *MinIOCleaner) removeObjects(ctx context.Context, bucket string, keys []string, errorMessage string) int {
	if len(keys) == 0 {
		return 0
	}
	objectsCh := make(chan minio.ObjectInfo)
	go func() {
		defer close(objectsCh)
		for _, key := range keys {
			objectsCh <- minio.ObjectInfo{Key: key}
		}
	}()

	deletedCount := 0
	errorCh := c.client.RemoveObjects(ctx, bucket, objectsCh, minio.RemoveObjectsOptions{})
	for err := range errorCh {
		if err.Err != nil {
			if c.log != nil {
				c.log.Error(errorMessage, "bucket", bucket, "key", err.ObjectName, "error", err.Err)
			}
			continue
		}
		deletedCount++
	}
	return deletedCount
}

func (stats *MinIOGarbageStats) addBucket(bucket string, bucketStats *MinIOGarbageStats) {
	if bucketStats == nil {
		return
	}
	stats.TotalCount += bucketStats.TotalCount
	stats.TotalSizeBytes += bucketStats.TotalSizeBytes
	stats.ByBucket[bucket] = bucketStats.TotalCount
	stats.Samples = append(stats.Samples, bucketStats.Samples...)
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
