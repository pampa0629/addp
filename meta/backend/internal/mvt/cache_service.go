package mvt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/addp/common/logger"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// CacheService MinIO 缓存服务
// 负责将预处理的 MVT 瓦片持久化到 MinIO
type CacheService struct {
	client *minio.Client
	bucket string
}

// MinIOConfig MinIO 配置
type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string
}

// NewCacheService 创建缓存服务
func NewCacheService(cfg MinIOConfig) (*CacheService, error) {
	// 初始化 MinIO 客户端
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 检查 bucket 是否存在
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		return nil, fmt.Errorf("bucket '%s' does not exist", cfg.Bucket)
	}

	logger.L().Info("MinIO cache service initialized",
		"endpoint", cfg.Endpoint,
		"bucket", cfg.Bucket)

	return &CacheService{
		client: client,
		bucket: cfg.Bucket,
	}, nil
}

// PutTile 存储瓦片到 MinIO
// 参数:
//   - ctx: 上下文
//   - fingerprint: 数据指纹（目录名）
//   - z, x, y: 瓦片坐标
//   - data: 瓦片数据（已 gzip 压缩）
func (s *CacheService) PutTile(
	ctx context.Context,
	fingerprint string,
	z, x, y int,
	data []byte,
) error {
	objectPath := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	_, err := s.client.PutObject(ctx, s.bucket, objectPath, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{
			ContentType: "application/octet-stream",
			UserMetadata: map[string]string{
				"z":          fmt.Sprintf("%d", z),
				"x":          fmt.Sprintf("%d", x),
				"y":          fmt.Sprintf("%d", y),
				"encoding":   "gzip",
				"created_at": time.Now().Format(time.RFC3339),
			},
		})

	if err != nil {
		return fmt.Errorf("failed to put tile to minio: %w", err)
	}

	return nil
}

// GetTile 从 MinIO 获取瓦片
func (s *CacheService) GetTile(
	ctx context.Context,
	fingerprint string,
	z, x, y int,
) ([]byte, error) {
	objectPath := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	obj, err := s.client.GetObject(ctx, s.bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get tile from minio: %w", err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to read tile data: %w", err)
	}

	return data, nil
}

// DeleteTile 删除 MinIO 中的瓦片
func (s *CacheService) DeleteTile(
	ctx context.Context,
	fingerprint string,
	z, x, y int,
) error {
	objectPath := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	err := s.client.RemoveObject(ctx, s.bucket, objectPath, minio.RemoveObjectOptions{})
	if err != nil {
		// 忽略对象不存在的错误
		return nil
	}

	return nil
}

// DeleteAllTiles 删除指定 fingerprint 下的所有瓦片
func (s *CacheService) DeleteAllTiles(ctx context.Context, fingerprint string) error {
	// 列出所有对象
	objectCh := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    fmt.Sprintf("%s/tiles/", fingerprint),
		Recursive: true,
	})

	// 删除所有对象
	for obj := range objectCh {
		if obj.Err != nil {
			logger.L().Error("Failed to list object", "error", obj.Err)
			continue
		}

		err := s.client.RemoveObject(ctx, s.bucket, obj.Key, minio.RemoveObjectOptions{})
		if err != nil {
			logger.L().Error("Failed to delete object", "key", obj.Key, "error", err)
		}
	}

	return nil
}

// PutMetadata 存储预处理元数据到 MinIO
func (s *CacheService) PutMetadata(
	ctx context.Context,
	fingerprint string,
	metadata *PreprocessMetadata,
) error {
	objectPath := fmt.Sprintf("%s/metadata.json", fingerprint)

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = s.client.PutObject(ctx, s.bucket, objectPath, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{
			ContentType: "application/json",
		})

	if err != nil {
		return fmt.Errorf("failed to put metadata to minio: %w", err)
	}

	return nil
}

// GetMetadata 从 MinIO 获取预处理元数据
func (s *CacheService) GetMetadata(
	ctx context.Context,
	fingerprint string,
) (*PreprocessMetadata, error) {
	objectPath := fmt.Sprintf("%s/metadata.json", fingerprint)

	obj, err := s.client.GetObject(ctx, s.bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata from minio: %w", err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata PreprocessMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &metadata, nil
}

// TileExists 检查瓦片是否存在
func (s *CacheService) TileExists(
	ctx context.Context,
	fingerprint string,
	z, x, y int,
) bool {
	objectPath := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	_, err := s.client.StatObject(ctx, s.bucket, objectPath, minio.StatObjectOptions{})
	return err == nil
}
