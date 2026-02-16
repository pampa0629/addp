package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/addp/common/logger"
	"github.com/minio/minio-go/v7"
)

// TileCacheService 瓦片缓存服务（基于 MinIO）
type TileCacheService struct {
	minioClient  *minio.Client
	bucket       string
	tileBasePath string
}

// NewTileCacheService 创建瓦片缓存服务
func NewTileCacheService(minioClient *minio.Client, bucket string, tileBasePath string) *TileCacheService {
	return &TileCacheService{
		minioClient:  minioClient,
		bucket:       bucket,
		tileBasePath: tileBasePath,
	}
}

// GetCachedTile 从 MinIO 读取缓存瓦片
func (s *TileCacheService) GetCachedTile(
	ctx context.Context,
	tenantID uint,
	serviceID, layerID uint,
	z, x, y int,
) ([]byte, error) {
	// 构建对象路径（包含租户隔离）
	objectName := fmt.Sprintf("%s/%d/%d/%d/cache/%d/%d/%d.mvt.gz",
		s.tileBasePath, tenantID, serviceID, layerID, z, x, y)

	// 从 MinIO 读取
	object, err := s.minioClient.GetObject(ctx, s.bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()

	// 检查对象是否存在
	_, err = object.Stat()
	if err != nil {
		return nil, err
	}

	// Gzip 解压
	gzipReader, err := gzip.NewReader(object)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	data, err := io.ReadAll(gzipReader)
	if err != nil {
		return nil, err
	}

	logger.L().Debug("✅ 缓存命中",
		"service_id", serviceID,
		"layer_id", layerID,
		"z", z, "x", x, "y", y,
		"size", len(data))

	return data, nil
}

// PutCachedTile 保存瓦片到 MinIO（异步）
func (s *TileCacheService) PutCachedTile(
	ctx context.Context,
	tenantID uint,
	serviceID, layerID uint,
	z, x, y int,
	tileData []byte,
	ttl int,
) error {
	// Gzip 压缩
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	_, err := gzipWriter.Write(tileData)
	if err != nil {
		return err
	}
	gzipWriter.Close()

	// 构建对象路径（包含租户隔离）
	objectName := fmt.Sprintf("%s/%d/%d/%d/cache/%d/%d/%d.mvt.gz",
		s.tileBasePath, tenantID, serviceID, layerID, z, x, y)

	// 上传到 MinIO
	_, err = s.minioClient.PutObject(
		ctx,
		s.bucket,
		objectName,
		bytes.NewReader(buf.Bytes()),
		int64(buf.Len()),
		minio.PutObjectOptions{
			ContentType:     "application/vnd.mapbox-vector-tile",
			ContentEncoding: "gzip",
			UserMetadata: map[string]string{
				"ttl":        strconv.Itoa(ttl),
				"created_at": time.Now().Format(time.RFC3339),
			},
		},
	)

	if err != nil {
		logger.L().Error("缓存保存失败",
			"service_id", serviceID,
			"layer_id", layerID,
			"z", z, "x", x, "y", y,
			"error", err)
		return err
	}

	logger.L().Debug("✅ 瓦片已缓存",
		"service_id", serviceID,
		"layer_id", layerID,
		"z", z, "x", x, "y", y,
		"original_size", len(tileData),
		"compressed_size", buf.Len(),
		"ttl", ttl)

	return nil
}

// ClearLayerCache 清理图层的所有缓存
func (s *TileCacheService) ClearLayerCache(
	ctx context.Context,
	tenantID uint,
	serviceID, layerID uint,
) error {
	// 删除 tiles/{tenantID}/{serviceID}/{layerID}/cache/ 下的所有对象
	prefix := fmt.Sprintf("%s/%d/%d/%d/cache/", s.tileBasePath, tenantID, serviceID, layerID)

	objectsCh := s.minioClient.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	deletedCount := 0
	for object := range objectsCh {
		if object.Err != nil {
			return object.Err
		}

		err := s.minioClient.RemoveObject(ctx, s.bucket, object.Key, minio.RemoveObjectOptions{})
		if err != nil {
			return err
		}
		deletedCount++
	}

	logger.L().Info("图层缓存已清理",
		"service_id", serviceID,
		"layer_id", layerID,
		"deleted_count", deletedCount)

	return nil
}

// ClearServiceCache 清理服务的所有缓存
func (s *TileCacheService) ClearServiceCache(
	ctx context.Context,
	tenantID uint,
	serviceID uint,
) error {
	// 删除 tiles/{tenantID}/{serviceID}/ 下的所有对象
	prefix := fmt.Sprintf("%s/%d/%d/", s.tileBasePath, tenantID, serviceID)

	objectsCh := s.minioClient.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	deletedCount := 0
	for object := range objectsCh {
		if object.Err != nil {
			return object.Err
		}

		err := s.minioClient.RemoveObject(ctx, s.bucket, object.Key, minio.RemoveObjectOptions{})
		if err != nil {
			return err
		}
		deletedCount++
	}

	logger.L().Info("服务缓存已清理",
		"service_id", serviceID,
		"deleted_count", deletedCount)

	return nil
}
