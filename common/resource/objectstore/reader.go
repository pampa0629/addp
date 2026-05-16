package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	engineobjectstore "github.com/addp/common/engine/plugins/objectstore"
	"github.com/addp/common/resource"
	miniogo "github.com/minio/minio-go/v7"
)

type Reader struct {
	client *miniogo.Client
}

func NewReaderFromConnectionInfo(connInfo plugin.ConnectionInfo) (*Reader, error) {
	client, err := engineobjectstore.NewClient(connInfo, false, true)
	if err != nil {
		return nil, err
	}
	return &Reader{client: client}, nil
}

func (r *Reader) Open(ctx context.Context, ref resource.ResourceRef) (io.ReadCloser, error) {
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

func (r *Reader) Stat(ctx context.Context, ref resource.ResourceRef) (*resource.ResourceMetadata, error) {
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
		return &resource.ResourceMetadata{Ref: ref, Exists: exists}, nil
	}

	info, err := r.client.StatObject(ctx, bucket, key, miniogo.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	return &resource.ResourceMetadata{
		Ref:         ref,
		Size:        info.Size,
		ContentType: info.ContentType,
		ModifiedAt:  nonZeroTimePtr(info.LastModified),
		Exists:      true,
	}, nil
}

func (r *Reader) List(ctx context.Context, scope resource.ResourceRef) ([]resource.ResourceRef, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("object store reader is not initialized")
	}
	bucket, prefix := splitObjectStoreRef(scope)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object resource scope: %s", scope.Path)
	}

	objectCh := r.client.ListObjects(ctx, bucket, miniogo.ListObjectsOptions{
		Prefix:    resource.EnsurePrefix(prefix),
		Recursive: true,
	})

	refs := []resource.ResourceRef{}
	for obj := range objectCh {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if strings.HasSuffix(obj.Key, "/") {
			continue
		}
		refs = append(refs, resource.NewResourceRef(bucket+"/"+obj.Key, resource.ResourceRoleMain))
	}
	if len(refs) == 0 {
		return nil, resource.ErrResourceNotFound
	}
	return refs, nil
}

func (r *Reader) Put(ctx context.Context, ref resource.ResourceRef, content io.Reader, contentType string, size int64) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("object store reader is not initialized")
	}
	bucket, key := splitObjectStoreRef(ref)
	if bucket == "" || key == "" {
		return fmt.Errorf("invalid object resource path: %s", ref.Path)
	}
	if contentType == "" {
		contentType = engineobjectstore.InferContentType(key)
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

func (r *Reader) ReadText(ctx context.Context, ref resource.ResourceRef) (string, error) {
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

func splitObjectStoreRef(ref resource.ResourceRef) (bucket, key string) {
	return engineobjectstore.SplitBucketPrefix(ref.Path)
}

func nonZeroTimePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
