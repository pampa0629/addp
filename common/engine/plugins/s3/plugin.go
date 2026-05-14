package s3

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

func (p *S3Plugin) EngineOrigin() string {
	return "general"
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
		Semantics:    capabilities.Storage.Semantics,
		NotSupported: capabilities.Storage.NotSupported,
	}
}

func (p *S3Plugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.ObjectCatalogModel()
}

func (p *S3Plugin) objectCatalogCallbacks() plugin.ObjectCatalogCallbacks {
	return plugin.ObjectCatalogCallbacks{
		ListRootsFunc:         p.listRoots,
		ListDirectoryFunc:     p.listDirectory,
		GetObjectMetadataFunc: p.getFileMetadata,
	}
}

func (p *S3Plugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListObjectCatalogChildren(ctx, p.objectCatalogCallbacks(), connInfo, parent.EngineID, parent, opts)
}

func (p *S3Plugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveObjectCatalogPath(ctx, p.objectCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *S3Plugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeObjectItem(ctx, p.objectCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *S3Plugin) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	return p.readFile(ctx, connInfo, path.StringPath(), opts)
}

func (p *S3Plugin) OpenRange(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Length <= 0 {
		return nil, fmt.Errorf("range read requires positive length")
	}
	return p.readFile(ctx, connInfo, path.StringPath(), opts)
}

func (p *S3Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *S3Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	return objectstore.TestConnection(ctx, connInfo, true, false)
}

func (p *S3Plugin) defaultBucket() string {
	return ""
}

func (p *S3Plugin) supportsSSL() bool {
	return true
}

// === object storage helpers ===

// InferContentType 根据对象键推断 MIME 类型
func (p *S3Plugin) inferContentType(objectKey string) string {
	return objectstore.InferContentType(objectKey)
}

// createClient 创建 S3 客户端（辅助方法）
func (p *S3Plugin) createClient(connInfo plugin.ConnectionInfo) (*miniogo.Client, error) {
	return objectstore.NewClient(connInfo, true, false)
}

func (p *S3Plugin) parseConnInfo(connInfo plugin.ConnectionInfo) (endpoint, accessKey, secretKey string, useSSL bool, err error) {
	cfg, parseErr := objectstore.ParseClientConfig(connInfo, true, false)
	if parseErr != nil {
		err = parseErr
		return "", "", "", false, err
	}
	return cfg.Endpoint, cfg.AccessKey, cfg.SecretKey, cfg.UseSSL, nil
}

// === 文件系统底层 helper ===

// listDirectory 列出路径下的直接子内容（非递归）
// path 格式：bucket/prefix/
func (p *S3Plugin) listDirectory(ctx context.Context, connInfo plugin.ConnectionInfo, path string) ([]plugin.FileEntry, []plugin.DirEntry, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, nil, err
	}

	bucket, prefix := objectstore.SplitBucketDirectory(path)
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
func (p *S3Plugin) readFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string, opts plugin.ReadOptions) (io.ReadCloser, error) {
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

// getFileMetadata 获取文件元数据
// path 格式：bucket/key
func (p *S3Plugin) getFileMetadata(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (*plugin.FileMetadata, error) {
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
