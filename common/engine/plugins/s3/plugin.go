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

func (p *S3Plugin) ConnectionSpec() plugin.ConnectionSpec {
	spec := plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "endpoint", LabelKey: "storageEngine.endpoint", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "s3.amazonaws.com", PlaceholderKey: "storageEngine.endpointPlaceholder"},
		plugin.ConnectionFieldSpec{Key: "access_key", LabelKey: "storageEngine.accessKey", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "secret_key", LabelKey: "storageEngine.secretKey", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "bucket", LabelKey: "storageEngine.bucket", Input: plugin.ConnectionFieldText, PlaceholderKey: "storageEngine.bucketPlaceholder"},
		plugin.ConnectionFieldSpec{Key: "use_ssl", LabelKey: "storageEngine.useSsl", Input: plugin.ConnectionFieldBoolean, Default: true},
	)
	spec.DefaultPort = 443
	return spec
}

func (p *S3Plugin) DefaultPort() int {
	return p.ConnectionSpec().DefaultPortValue()
}

func (p *S3Plugin) RequiredFields() []string {
	return p.ConnectionSpec().RequiredFields()
}

func (p *S3Plugin) SensitiveFields() []string {
	return p.ConnectionSpec().SensitiveFields()
}

func (p *S3Plugin) ConnectionIdentityFields() []string {
	return p.ConnectionSpec().IdentityFields()
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

func (p *S3Plugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.ObjectCatalogModel()
}

func (p *S3Plugin) objectCatalogCallbacks() plugin.ObjectCatalogCallbacks {
	return plugin.ObjectCatalogCallbacks{
		ListBucketsFunc:           p.listBuckets,
		ListDirectoryFunc:         p.listDirectory,
		GetObjectStorageFactsFunc: p.getStorageObjectFacts,
	}
}

func (p *S3Plugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return plugin.ListObjectCatalogChildren(ctx, p.objectCatalogCallbacks(), connInfo, parent.EngineID, parent, opts)
}

func (p *S3Plugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return plugin.ResolveObjectCatalogPath(ctx, p.objectCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *S3Plugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return plugin.DescribeObjectCatalogFacts(ctx, p.objectCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *S3Plugin) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return nil, err
	}
	return p.readFile(ctx, connInfo, objectPath, opts)
}

func (p *S3Plugin) OpenRange(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Length <= 0 {
		return nil, fmt.Errorf("range read requires positive length")
	}
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return nil, err
	}
	return p.readFile(ctx, connInfo, objectPath, opts)
}

func (p *S3Plugin) CreateContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.WriteOptions) (io.WriteCloser, error) {
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

func (p *S3Plugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
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
func (p *S3Plugin) listDirectory(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

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

// getStorageObjectFacts 获取存储对象事实
// path 格式：bucket/key
func (p *S3Plugin) getStorageObjectFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (*plugin.StorageObjectFacts, error) {
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
func (p *S3Plugin) listBuckets(ctx context.Context, connInfo plugin.ConnectionInfo, root plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
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
