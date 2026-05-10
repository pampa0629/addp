package resource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
	miniogo "github.com/minio/minio-go/v7"
)

type ObjectStoreReader struct {
	client *miniogo.Client
}

type MaterializeResult struct {
	LocalDir string
	Files    map[ResourceRef]string
}

func NewObjectStoreReaderFromConnectionInfo(connInfo plugin.ConnectionInfo) (*ObjectStoreReader, error) {
	client, err := objectstore.NewClient(connInfo, false, true)
	if err != nil {
		return nil, err
	}
	return &ObjectStoreReader{client: client}, nil
}

func (r *ObjectStoreReader) Open(ctx context.Context, ref ResourceRef) (io.ReadCloser, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("object store reader is not initialized")
	}
	bucket, key := splitObjectStoreRef(ref)
	if bucket == "" || key == "" {
		return nil, fmt.Errorf("invalid object resource path: %s", ref.Path)
	}
	obj, err := r.client.GetObject(ctx, bucket, key, miniogo.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (r *ObjectStoreReader) Stat(ctx context.Context, ref ResourceRef) (*ResourceMetadata, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("object store reader is not initialized")
	}
	bucket, key := splitObjectStoreRef(ref)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object resource path: %s", ref.Path)
	}
	if key == "" {
		exists, err := r.client.BucketExists(ctx, bucket)
		if err != nil {
			return nil, err
		}
		return &ResourceMetadata{Ref: ref, Exists: exists}, nil
	}

	info, err := r.client.StatObject(ctx, bucket, key, miniogo.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	modifiedAt := info.LastModified
	return &ResourceMetadata{
		Ref:         ref,
		Size:        info.Size,
		ContentType: info.ContentType,
		ModifiedAt:  nonZeroTimePtr(modifiedAt),
		Exists:      true,
	}, nil
}

func (r *ObjectStoreReader) List(ctx context.Context, scope ResourceRef) ([]ResourceRef, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("object store reader is not initialized")
	}
	bucket, prefix := splitObjectStoreRef(scope)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object resource scope: %s", scope.Path)
	}

	objectCh := r.client.ListObjects(ctx, bucket, miniogo.ListObjectsOptions{
		Prefix:    ensurePrefix(prefix),
		Recursive: true,
	})

	refs := []ResourceRef{}
	for obj := range objectCh {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if strings.HasSuffix(obj.Key, "/") {
			continue
		}
		refs = append(refs, NewResourceRef(bucket+"/"+obj.Key, ResourceRoleMain))
	}
	if len(refs) == 0 {
		return nil, ErrResourceNotFound
	}
	return refs, nil
}

func (r *ObjectStoreReader) Put(ctx context.Context, ref ResourceRef, content io.Reader, contentType string, size int64) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("object store reader is not initialized")
	}
	bucket, key := splitObjectStoreRef(ref)
	if bucket == "" || key == "" {
		return fmt.Errorf("invalid object resource path: %s", ref.Path)
	}
	if contentType == "" {
		contentType = objectstore.InferContentType(key)
	}
	if size < 0 {
		data, err := io.ReadAll(content)
		if err != nil {
			return err
		}
		content = bytes.NewReader(data)
		size = int64(len(data))
	}

	exists, err := r.client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		if err := r.client.MakeBucket(ctx, bucket, miniogo.MakeBucketOptions{}); err != nil {
			return err
		}
	}

	_, err = r.client.PutObject(ctx, bucket, key, content, size, miniogo.PutObjectOptions{ContentType: contentType})
	return err
}

func (r *ObjectStoreReader) ReadText(ctx context.Context, ref ResourceRef) (string, error) {
	rc, err := r.Open(ctx, ref)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func MaterializeResourceScope(ctx context.Context, reader ResourceReader, scope ResourceRef, localDir string) (*MaterializeResult, error) {
	if reader == nil {
		return nil, fmt.Errorf("resource reader is required")
	}
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return nil, err
	}

	refs, err := reader.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	scopePrefix := ensurePrefix(strings.Trim(scope.Path, "/"))
	result := &MaterializeResult{
		LocalDir: localDir,
		Files:    make(map[ResourceRef]string, len(refs)),
	}
	for _, ref := range refs {
		relativePath := strings.TrimPrefix(strings.Trim(ref.Path, "/"), scopePrefix)
		if relativePath == "" {
			continue
		}
		localPath := filepath.Join(localDir, filepath.FromSlash(relativePath))
		if err := materializeResource(ctx, reader, ref, localPath); err != nil {
			return nil, err
		}
		result.Files[ref] = localPath
	}
	if len(result.Files) == 0 {
		return nil, ErrResourceNotFound
	}
	return result, nil
}

func materializeResource(ctx context.Context, reader ResourceReader, ref ResourceRef, localPath string) error {
	rc, err := reader.Open(ctx, ref)
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	file, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, rc)
	return err
}

func splitObjectStoreRef(ref ResourceRef) (bucket, key string) {
	return objectstore.SplitBucketPrefix(ref.Path)
}

func ensurePrefix(prefix string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" || strings.HasSuffix(prefix, "/") {
		return prefix
	}
	return prefix + "/"
}

func nonZeroTimePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
