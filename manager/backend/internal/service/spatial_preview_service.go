package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/addp/common/logger"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// SpatialPreviewService MVT 空间数据预览服务
type SpatialPreviewService struct {
	minioClient *minio.Client
}

// NewSpatialPreviewService 创建空间预览服务
func NewSpatialPreviewService() *SpatialPreviewService {
	// MinIO 客户端稍后初始化（按需连接）
	return &SpatialPreviewService{}
}

// GetTileMetadata 获取瓦片元数据
func (s *SpatialPreviewService) GetTileMetadata(ctx context.Context, fingerprint string) (map[string]interface{}, error) {
	// 初始化 MinIO 客户端（如果未初始化）
	if err := s.ensureMinIOClient(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	// 尝试读取 metadata.json
	objectName := fmt.Sprintf("%s/metadata.json", fingerprint)
	obj, err := s.minioClient.GetObject(ctx, "mvt-tiles", objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("metadata not found for fingerprint %s: %w", fingerprint, err)
	}
	defer obj.Close()

	// 解析 JSON
	var metadata map[string]interface{}
	if err := json.NewDecoder(obj).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return metadata, nil
}

// GetTile 获取指定瓦片
func (s *SpatialPreviewService) GetTile(ctx context.Context, fingerprint string, z, x, y int) ([]byte, error) {
	// 初始化 MinIO 客户端（如果未初始化）
	if err := s.ensureMinIOClient(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	// 构建对象路径: {fingerprint}/tiles/z{z}/{x}_{y}.mvt.gz
	objectName := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	obj, err := s.minioClient.GetObject(ctx, "mvt-tiles", objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("tile not found: %w", err)
	}
	defer obj.Close()

	// 读取全部数据
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to read tile data: %w", err)
	}

	return data, nil
}

// CheckTileExists 检查瓦片是否存在
func (s *SpatialPreviewService) CheckTileExists(ctx context.Context, fingerprint string, z, x, y int) (bool, error) {
	// 初始化 MinIO 客户端（如果未初始化）
	if err := s.ensureMinIOClient(ctx); err != nil {
		return false, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	objectName := fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", fingerprint, z, x, y)

	_, err := s.minioClient.StatObject(ctx, "mvt-tiles", objectName, minio.StatObjectOptions{})
	if err != nil {
		// 对象不存在
		return false, nil
	}

	return true, nil
}

// ensureMinIOClient 确保 MinIO 客户端已初始化
func (s *SpatialPreviewService) ensureMinIOClient(ctx context.Context) error {
	if s.minioClient != nil {
		return nil
	}

	// 从环境变量或配置读取 MinIO 连接信息
	endpoint := os.Getenv("BUSINESS_MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:9002" // 默认值
	}

	accessKey := os.Getenv("BUSINESS_MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}

	secretKey := os.Getenv("BUSINESS_MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
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
