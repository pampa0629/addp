package objectstore

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	miniogo "github.com/minio/minio-go/v7"
)

// Protocol implements the shared S3 catalog and content contracts.
// Each engine owns its identity, connection defaults and capability declaration.
type Protocol struct{ DefaultSSL bool }

func (p Protocol) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.ObjectCatalogModel()
}

func (p Protocol) objectCatalogCallbacks() plugin.ObjectCatalogCallbacks {
	return plugin.ObjectCatalogCallbacks{
		ListBucketsFunc:           p.listBuckets,
		ListDirectoryFunc:         p.listDirectory,
		GetObjectStorageFactsFunc: p.getStorageObjectFacts,
	}
}

func (p Protocol) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return plugin.ListObjectCatalogChildren(ctx, p.objectCatalogCallbacks(), connInfo, parent.EngineID, parent, opts)
}

func (p Protocol) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return plugin.ResolveObjectCatalogPath(ctx, p.objectCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p Protocol) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return plugin.DescribeObjectCatalogFacts(ctx, p.objectCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p Protocol) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return nil, err
	}
	return p.readFile(ctx, connInfo, objectPath, opts)
}

func (p Protocol) OpenRange(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Length <= 0 {
		return nil, fmt.Errorf("range read requires positive length")
	}
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return nil, err
	}
	return p.readFile(ctx, connInfo, objectPath, opts)
}

func (p Protocol) CreateContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.WriteOptions) (io.WriteCloser, error) {
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return nil, err
	}
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}
	return CreateContent(ctx, client, objectPath, opts)
}

func (p Protocol) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
	objectPath, err := plugin.RequireObjectLeafPath(path)
	if err != nil {
		return err
	}
	client, err := p.createClient(connInfo)
	if err != nil {
		return err
	}
	return DeleteResource(ctx, client, objectPath)
}

func (p Protocol) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	return TestConnection(ctx, connInfo, p.DefaultSSL, false)
}

func (p Protocol) createClient(connInfo plugin.ConnectionInfo) (*miniogo.Client, error) {
	return NewClient(connInfo, p.DefaultSSL, false)
}

// === 文件系统底层 helper ===

// listDirectory 列出路径下的直接子内容（非递归）
// path 格式：bucket/prefix/
func (p Protocol) listDirectory(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	path := parent.StringPath()
	bucket, prefix := SplitBucketDirectory(path)
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
				ContentType: InferContentType(obj.Key),
			}))
		}
	}

	return nodes, nil
}

// readFile 流式读取文件内容
// path 格式：bucket/key
func (p Protocol) readFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string, opts plugin.ReadOptions) (io.ReadCloser, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	bucket, key := SplitBucketPrefix(path)
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
func (p Protocol) getStorageObjectFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (*plugin.StorageObjectFacts, error) {
	client, err := p.createClient(connInfo)
	if err != nil {
		return nil, err
	}

	bucket, key := SplitBucketPrefix(path)
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
func (p Protocol) listBuckets(ctx context.Context, connInfo plugin.ConnectionInfo, root plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
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
