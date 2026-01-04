package minio

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/database/plugin"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOPlugin MinIO 对象存储插件
type MinIOPlugin struct{}

func init() {
	plugin.Register(&MinIOPlugin{})
}

func (p *MinIOPlugin) Type() string {
	return "minio"
}

func (p *MinIOPlugin) DisplayName() string {
	return "MinIO"
}

func (p *MinIOPlugin) ConnectionCategory() string {
	return "storage"
}

func (p *MinIOPlugin) DefaultPort() int {
	return 9000
}

func (p *MinIOPlugin) RequiredFields() []string {
	return []string{"endpoint", "access_key", "secret_key"}
}

func (p *MinIOPlugin) SensitiveFields() []string {
	return []string{"access_key", "secret_key"}
}

func (p *MinIOPlugin) GenerateCapabilities() string {
	return `{"storage":[{"type":"object_storage","engine":"minio"}]}`
}

func (p *MinIOPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *MinIOPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	// 对象存储返回 JSON 格式的连接信息
	bytes, err := json.Marshal(connInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal MinIO connection info: %w", err)
	}
	return string(bytes), nil
}

func (p *MinIOPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	// 规范化 endpoint
	endpoint := p.normalizeEndpoint(plugin.GetString(connInfo, "endpoint"))
	accessKey := plugin.GetString(connInfo, "access_key")
	secretKey := plugin.GetString(connInfo, "secret_key")
	useSSL := plugin.GetBool(connInfo, "use_ssl")
	bucket := plugin.GetString(connInfo, "bucket")

	if endpoint == "" || accessKey == "" || secretKey == "" {
		return fmt.Errorf("missing required fields: endpoint, access_key, secret_key")
	}

	// 初始化 MinIO 客户端
	client, err := miniogo.New(endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create minio client: %w", err)
	}

	// 设置超时
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 测试连接 - 列出存储桶
	buckets, err := client.ListBuckets(testCtx)
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	// 如果指定了 bucket，检查是否存在
	if bucket != "" {
		found := false
		for _, b := range buckets {
			if b.Name == bucket {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("bucket '%s' not found", bucket)
		}
	}

	return nil
}

// normalizeEndpoint 规范化 endpoint 中的 localhost
func (p *MinIOPlugin) normalizeEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}

	// 提取主机名和端口部分
	hostPart := endpoint
	portPart := ""

	for i := len(endpoint) - 1; i >= 0; i-- {
		if endpoint[i] == ':' {
			hostPart = endpoint[:i]
			portPart = endpoint[i:] // 包含冒号
			break
		}
	}

	// 如果是 localhost 或 127.0.0.1，使用 NormalizeHost 处理
	if hostPart == "localhost" || hostPart == "127.0.0.1" {
		return plugin.NormalizeHost(hostPart) + portPart
	}

	return endpoint
}

func (p *MinIOPlugin) DefaultBucket() string {
	return ""
}

func (p *MinIOPlugin) SupportsSSL() bool {
	return true
}

// === ObjectStoragePlugin 接口实现 ===

// SupportsMetadataQuery 实现 StoragePlugin 接口
func (p *MinIOPlugin) SupportsMetadataQuery() bool {
	return true
}

// ListBuckets 列出所有 Bucket
func (p *MinIOPlugin) ListBuckets(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.BucketInfo, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list buckets: %w", err)
	}

	result := make([]plugin.BucketInfo, len(buckets))
	for i, b := range buckets {
		result[i] = plugin.BucketInfo{
			Name:         b.Name,
			CreationDate: b.CreationDate,
		}
	}
	return result, nil
}

// ListObjects 列出对象（支持前缀过滤和递归）
func (p *MinIOPlugin) ListObjects(ctx context.Context, connInfo plugin.ConnectionInfo, bucket, prefix string, recursive bool) ([]plugin.ObjectInfo, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	objectCh := client.ListObjects(ctx, bucket, miniogo.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: recursive,
	})

	var result []plugin.ObjectInfo
	for object := range objectCh {
		if object.Err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", object.Err)
		}

		result = append(result, plugin.ObjectInfo{
			Bucket:       bucket,
			Key:          object.Key,
			Size:         object.Size,
			LastModified: object.LastModified,
			ContentType:  p.InferContentType(object.Key),
			ETag:         object.ETag,
		})
	}

	return result, nil
}

// GetObjectMetadata 获取单个对象的元数据
func (p *MinIOPlugin) GetObjectMetadata(ctx context.Context, connInfo plugin.ConnectionInfo, bucket, key string) (*plugin.ObjectInfo, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	objInfo, err := client.StatObject(ctx, bucket, key, miniogo.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object metadata: %w", err)
	}

	return &plugin.ObjectInfo{
		Bucket:       bucket,
		Key:          objInfo.Key,
		Size:         objInfo.Size,
		LastModified: objInfo.LastModified,
		ContentType:  objInfo.ContentType,
		ETag:         objInfo.ETag,
	}, nil
}

// InferContentType 根据对象键推断 MIME 类型
func (p *MinIOPlugin) InferContentType(objectKey string) string {
	ext := strings.ToLower(filepath.Ext(objectKey))

	// 1. 使用标准库
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}

	// 2. 自定义类型映射（空间数据格式）
	customTypes := map[string]string{
		".geojson": "application/geo+json",
		".shp":     "application/x-shapefile",
		".shx":     "application/x-shapefile",
		".dbf":     "application/x-dbf",
		".prj":     "application/x-shapefile-prj",
		".kml":     "application/vnd.google-earth.kml+xml",
		".kmz":     "application/vnd.google-earth.kmz",
		".gpx":     "application/gpx+xml",
		".gml":     "application/gml+xml",
		".tif":     "image/tiff",
		".tiff":    "image/tiff",
	}
	if mimeType, ok := customTypes[ext]; ok {
		return mimeType
	}

	return "application/octet-stream"
}

// createClient 创建 MinIO 客户端（辅助方法）
func (p *MinIOPlugin) createClient(connInfo plugin.ConnectionInfo) (*miniogo.Client, error) {
	endpoint := p.normalizeEndpoint(plugin.GetString(connInfo, "endpoint"))
	accessKey := plugin.GetString(connInfo, "access_key")
	secretKey := plugin.GetString(connInfo, "secret_key")
	useSSL := plugin.GetBool(connInfo, "use_ssl")

	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("missing required fields: endpoint, access_key, secret_key")
	}

	return miniogo.New(endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
}
