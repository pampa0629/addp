package service

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/service/internal/models"
	"github.com/minio/minio-go/v7"
)

// ============================================================================
// StaticTileService 静态瓦片服务
// ============================================================================

// StaticTileService 处理静态瓦片的读取
type StaticTileService struct {
	minioClient *minio.Client
	bucket      string
}

// NewStaticTileService 创建静态瓦片服务实例
func NewStaticTileService(minioClient *minio.Client, bucket string) *StaticTileService {
	return &StaticTileService{
		minioClient: minioClient,
		bucket:      bucket,
	}
}

// GetStaticTile 从 MinIO 读取静态瓦片
func (s *StaticTileService) GetStaticTile(
	ctx context.Context,
	layer *models.TileServiceLayer,
	z, x, y int,
	format string,
) ([]byte, error) {
	// 1. 解析 layer_config
	config := layer.LayerConfig
	if config == nil {
		return nil, fmt.Errorf("layer config is nil")
	}

	source, ok := config["source"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid source in layer config")
	}

	tilePath, ok := config["tile_path"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid tile_path in layer config")
	}

	// 2. 验证缩放级别
	if zoomRange, ok := config["zoom_range"].([]interface{}); ok {
		if len(zoomRange) == 2 {
			minZoom := int(zoomRange[0].(float64))
			maxZoom := int(zoomRange[1].(float64))
			if z < minZoom || z > maxZoom {
				return nil, fmt.Errorf("zoom level %d out of range [%d, %d]", z, minZoom, maxZoom)
			}
		}
	}

	// 3. 构建瓦片路径
	var objectName string
	if source == "external" {
		// 外部静态瓦片：从指定的 MinIO 路径读取
		objectName = fmt.Sprintf("%s/%d/%d/%d.%s", tilePath, z, x, y, format)
	} else if source == "internal" {
		// 内部缓存瓦片：从 service bucket 读取
		objectName = fmt.Sprintf("%s/%d/%d/%d.%s", tilePath, z, x, y, format)
	} else {
		return nil, fmt.Errorf("unsupported source type: %s", source)
	}

	// 4. 从 MinIO 读取
	object, err := s.minioClient.GetObject(ctx, s.bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object from MinIO: %w", err)
	}
	defer object.Close()

	// 5. 读取数据
	data, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("failed to read tile data: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty tile data")
	}

	return data, nil
}
