package s3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Plugin Amazon S3 对象存储插件
// 使用 MinIO SDK (S3 兼容)
type S3Plugin struct{}

func init() {
	plugin.Register(&S3Plugin{})
}

func (p *S3Plugin) Type() string {
	return "s3"
}

func (p *S3Plugin) DisplayName() string {
	return "Amazon S3"
}

func (p *S3Plugin) EngineCategory() string {
	return "standard"
}

func (p *S3Plugin) DefaultPort() int {
	return 443 // S3 默认使用 HTTPS
}

func (p *S3Plugin) RequiredFields() []string {
	return []string{"endpoint", "access_key", "secret_key"}
}

func (p *S3Plugin) SensitiveFields() []string {
	return []string{"access_key", "secret_key"}
}

func (p *S3Plugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewObjectCapabilities(p.Type())
}

func (p *S3Plugin) StoreSemantics() plugin.StoreSemantics {
	capabilities := p.Capabilities()
	return plugin.StoreSemantics{
		Families:     capabilities.Storage.Families,
		Semantics:    capabilities.Storage.Semantics,
		NotSupported: capabilities.Storage.NotSupported,
	}
}

func (p *S3Plugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.ObjectCatalogModel()
}

func (p *S3Plugin) fileSystemCatalogAdapter() plugin.FileSystemCatalogAdapter {
	return plugin.FileSystemCatalogAdapter{
		ListRootsFunc:       p.listRoots,
		ListDirectoryFunc:   p.listDirectory,
		GetFileMetadataFunc: p.getFileMetadata,
	}
}

func (p *S3Plugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListFileSystemCatalogChildren(ctx, p.fileSystemCatalogAdapter(), connInfo, parent.EngineID, parent, plugin.CatalogTermBucket, opts)
}

func (p *S3Plugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveFileSystemCatalogPath(ctx, p.fileSystemCatalogAdapter(), connInfo, path.EngineID, path, plugin.CatalogTermBucket)
}

func (p *S3Plugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeFileSystemItem(ctx, p.fileSystemCatalogAdapter(), connInfo, path.EngineID, path)
}

func (p *S3Plugin) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	return p.readFile(ctx, connInfo, path.StringPath())
}

func (p *S3Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *S3Plugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	// 对象存储返回 JSON 格式的连接信息
	bytes, err := json.Marshal(connInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal S3 connection info: %w", err)
	}
	return string(bytes), nil
}

func (p *S3Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	endpoint := plugin.GetString(connInfo, "endpoint")
	accessKey := plugin.GetString(connInfo, "access_key")
	secretKey := plugin.GetString(connInfo, "secret_key")
	useSSL := plugin.GetBool(connInfo, "use_ssl")
	bucket := plugin.GetString(connInfo, "bucket")

	// S3 默认使用 SSL
	if !plugin.Contains([]string{"use_ssl"}, "use_ssl") {
		useSSL = true
	}

	if endpoint == "" || accessKey == "" || secretKey == "" {
		return fmt.Errorf("missing required fields: endpoint, access_key, secret_key")
	}

	// 初始化 S3 客户端（使用 MinIO SDK，S3 兼容）
	client, err := miniogo.New(endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create s3 client: %w", err)
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

func (p *S3Plugin) defaultBucket() string {
	return ""
}

func (p *S3Plugin) supportsSSL() bool {
	return true
}

// === object storage helpers ===

// ListBuckets 列出所有 Bucket
func (p *S3Plugin) listBuckets(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.BucketInfo, error) {
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
func (p *S3Plugin) listObjects(ctx context.Context, connInfo plugin.ConnectionInfo, bucket, prefix string, recursive bool) ([]plugin.ObjectInfo, error) {
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
			ContentType:  p.inferContentType(object.Key),
			ETag:         object.ETag,
		})
	}

	return result, nil
}

// GetObjectMetadata 获取单个对象的元数据
func (p *S3Plugin) getObjectMetadata(ctx context.Context, connInfo plugin.ConnectionInfo, bucket, key string) (*plugin.ObjectInfo, error) {
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
func (p *S3Plugin) inferContentType(objectKey string) string {
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

// createClient 创建 S3 客户端（辅助方法）
func (p *S3Plugin) createClient(connInfo plugin.ConnectionInfo) (*miniogo.Client, error) {
	endpoint := plugin.GetString(connInfo, "endpoint")
	accessKey := plugin.GetString(connInfo, "access_key")
	secretKey := plugin.GetString(connInfo, "secret_key")
	useSSL := plugin.GetBool(connInfo, "use_ssl")

	// S3 默认使用 SSL
	if !plugin.Contains([]string{"use_ssl"}, "use_ssl") {
		useSSL = true
	}

	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("missing required fields: endpoint, access_key, secret_key")
	}

	return miniogo.New(endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
}

// === 文件系统底层 helper ===

// listDirectory 列出路径下的直接子内容（非递归）
// path 格式：bucket/prefix/
func (p *S3Plugin) listDirectory(ctx context.Context, connInfo plugin.ConnectionInfo, path string) ([]plugin.FileEntry, []plugin.DirEntry, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, nil, err
	}

	bucket, prefix := splitBucketPrefix(path)
	if bucket == "" {
		return nil, nil, fmt.Errorf("invalid path: %s (expected bucket/prefix/)", path)
	}

	objectCh := client.ListObjects(ctx, bucket, miniogo.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	})

	var files []plugin.FileEntry
	var dirs []plugin.DirEntry

	for obj := range objectCh {
		if obj.Err != nil {
			return nil, nil, fmt.Errorf("failed to list directory: %w", obj.Err)
		}
		if strings.HasSuffix(obj.Key, "/") {
			name := strings.TrimSuffix(strings.TrimPrefix(obj.Key, prefix), "/")
			if name == "" {
				continue
			}
			dirs = append(dirs, plugin.DirEntry{
				Name: name,
				Path: bucket + "/" + obj.Key,
			})
		} else {
			name := strings.TrimPrefix(obj.Key, prefix)
			if name == "" || strings.Contains(name, "/") {
				continue
			}
			files = append(files, plugin.FileEntry{
				Name:        name,
				Path:        bucket + "/" + obj.Key,
				Size:        obj.Size,
				ModifiedAt:  obj.LastModified,
				ContentType: p.inferContentType(obj.Key),
			})
		}
	}

	return files, dirs, nil
}

// readFile 流式读取文件内容
// path 格式：bucket/key
func (p *S3Plugin) readFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (io.ReadCloser, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	bucket, key := splitBucketPrefix(path)
	if bucket == "" || key == "" {
		return nil, fmt.Errorf("invalid path: %s (expected bucket/key)", path)
	}

	obj, err := client.GetObject(ctx, bucket, key, miniogo.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return obj, nil
}

// getFileMetadata 获取文件元数据
// path 格式：bucket/key
func (p *S3Plugin) getFileMetadata(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (*plugin.FileMetadata, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	bucket, key := splitBucketPrefix(path)
	if bucket == "" || key == "" {
		return nil, fmt.Errorf("invalid path: %s (expected bucket/key)", path)
	}

	info, err := client.StatObject(ctx, bucket, key, miniogo.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata %s: %w", path, err)
	}

	return &plugin.FileMetadata{
		Name:        filepath.Base(key),
		Path:        path,
		Size:        info.Size,
		ModifiedAt:  info.LastModified,
		ContentType: info.ContentType,
		ETag:        info.ETag,
	}, nil
}

// listRoots 列出根节点（对象存储 = Bucket 列表）
func (p *S3Plugin) listRoots(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.RootEntry, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list buckets: %w", err)
	}

	result := make([]plugin.RootEntry, len(buckets))
	for i, b := range buckets {
		result[i] = plugin.RootEntry{
			Name: b.Name,
			Path: b.Name + "/",
		}
	}
	return result, nil
}

// splitBucketPrefix 将 "bucket/prefix/..." 拆分为 bucket 和 prefix
func splitBucketPrefix(path string) (bucket, prefix string) {
	path = strings.TrimPrefix(path, "/")
	idx := strings.Index(path, "/")
	if idx < 0 {
		return path, ""
	}
	return path[:idx], path[idx+1:]
}
