package minio

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
	miniogo "github.com/minio/minio-go/v7"
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

func (p *MinIOPlugin) EngineOrigin() string {
	return "general"
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

func (p *MinIOPlugin) ConnectionIdentityFields() []string {
	return []string{"endpoint"}
}

func (p *MinIOPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewObjectCapabilities(p.Type())
}

func (p *MinIOPlugin) StoreSemantics() plugin.StoreSemantics {
	capabilities := p.Capabilities()
	return plugin.StoreSemantics{
		Semantics:    capabilities.Storage.Semantics,
		NotSupported: capabilities.Storage.NotSupported,
	}
}

func (p *MinIOPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.ObjectCatalogModel()
}

func (p *MinIOPlugin) objectCatalogCallbacks() plugin.ObjectCatalogCallbacks {
	return plugin.ObjectCatalogCallbacks{
		ListBucketsFunc:           p.listBuckets,
		ListDirectoryFunc:         p.listDirectory,
		GetObjectStorageFactsFunc: p.getStorageObjectFacts,
	}
}

func (p *MinIOPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return plugin.ListObjectCatalogChildren(ctx, p.objectCatalogCallbacks(), connInfo, parent.EngineID, parent, opts)
}

func (p *MinIOPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return plugin.ResolveObjectCatalogPath(ctx, p.objectCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *MinIOPlugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return plugin.DescribeObjectCatalogFacts(ctx, p.objectCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *MinIOPlugin) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return nil, err
	}
	return p.readFile(ctx, connInfo, objectPath, opts)
}

func (p *MinIOPlugin) OpenRange(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Length <= 0 {
		return nil, fmt.Errorf("range read requires positive length")
	}
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return nil, err
	}
	return p.readFile(ctx, connInfo, objectPath, opts)
}

func (p *MinIOPlugin) CreateContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.WriteOptions) (io.WriteCloser, error) {
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return nil, err
	}
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}
	return objectstore.CreateContent(ctx, client, objectPath, opts)
}

func (p *MinIOPlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return err
	}
	client, err := p.createClient(connInfo)
	if err != nil {
		return err
	}
	return objectstore.DeleteResource(ctx, client, objectPath)
}

func (p *MinIOPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *MinIOPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	return objectstore.TestConnection(ctx, connInfo, false, true)
}

func (p *MinIOPlugin) defaultBucket() string {
	return ""
}

func (p *MinIOPlugin) supportsSSL() bool {
	return true
}

// === object storage helpers ===

// InferContentType 根据对象键推断 MIME 类型
func (p *MinIOPlugin) inferContentType(objectKey string) string {
	return objectstore.InferContentType(objectKey)
}

// createClient 创建 MinIO 客户端（辅助方法）
func (p *MinIOPlugin) createClient(connInfo plugin.ConnectionInfo) (*miniogo.Client, error) {
	return objectstore.NewClient(connInfo, false, true)
}

// listDirectory 列出路径下的直接子内容（非递归）
// path 格式：bucket/prefix/
func (p *MinIOPlugin) listDirectory(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	// 解析 bucket 和 prefix
	path := parent.StringPath()
	bucket, prefix := objectstore.SplitBucketDirectory(path)
	if bucket == "" {
		return nil, fmt.Errorf("invalid path: %s (expected bucket/prefix/)", path)
	}

	objectCh := client.ListObjects(ctx, bucket, miniogo.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	})

	var nodes []plugin.EngineCatalogEntry

	for obj := range objectCh {
		if obj.Err != nil {
			return nil, fmt.Errorf("failed to list directory: %w", obj.Err)
		}
		// 以 "/" 结尾的是目录前缀
		if strings.HasSuffix(obj.Key, "/") {
			name := strings.TrimSuffix(strings.TrimPrefix(obj.Key, prefix), "/")
			if name == "" {
				continue
			}
			nodes = append(nodes, plugin.ObjectPrefixCatalogEntry(parent, name, bucket+"/"+obj.Key))
		} else {
			name := strings.TrimPrefix(obj.Key, prefix)
			if name == "" || strings.Contains(name, "/") {
				continue
			}
			nodes = append(nodes, plugin.ObjectLeafCatalogEntry(parent, plugin.StorageObjectFacts{
				Name:        name,
				Path:        bucket + "/" + obj.Key,
				Size:        obj.Size,
				ModifiedAt:  obj.LastModified,
				ContentType: p.inferContentType(obj.Key),
			}))
		}
	}

	return nodes, nil
}

// readFile 流式读取文件内容
// path 格式：bucket/key
func (p *MinIOPlugin) readFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string, opts plugin.ReadOptions) (io.ReadCloser, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	bucket, key := objectstore.SplitBucketPrefix(path)
	if bucket == "" || key == "" {
		return nil, fmt.Errorf("invalid path: %s (expected bucket/key)", path)
	}

	getOpts := miniogo.GetObjectOptions{}
	if opts.Offset > 0 || opts.Length > 0 {
		end := int64(0)
		if opts.Length > 0 {
			end = opts.Offset + opts.Length - 1
		}
		if err := getOpts.SetRange(opts.Offset, end); err != nil {
			return nil, fmt.Errorf("invalid range read options: %w", err)
		}
	}

	obj, err := client.GetObject(ctx, bucket, key, getOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return obj, nil
}

// getStorageObjectFacts 获取存储对象事实
// path 格式：bucket/key
func (p *MinIOPlugin) getStorageObjectFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (*plugin.StorageObjectFacts, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	bucket, key := objectstore.SplitBucketPrefix(path)
	if bucket == "" || key == "" {
		return nil, fmt.Errorf("invalid path: %s (expected bucket/key)", path)
	}

	info, err := client.StatObject(ctx, bucket, key, miniogo.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get storage object facts %s: %w", path, err)
	}

	return &plugin.StorageObjectFacts{
		Name:        filepath.Base(key),
		Path:        path,
		Size:        info.Size,
		ModifiedAt:  info.LastModified,
		ContentType: info.ContentType,
		ETag:        info.ETag,
	}, nil
}

// listBuckets 列出 service root 下的第一层业务 bucket。
func (p *MinIOPlugin) listBuckets(ctx context.Context, connInfo plugin.ConnectionInfo, root plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list buckets: %w", err)
	}

	result := make([]plugin.EngineCatalogEntry, len(buckets))
	for i, b := range buckets {
		result[i] = plugin.ObjectBucketCatalogEntry(root, b.Name)
	}
	return result, nil
}
