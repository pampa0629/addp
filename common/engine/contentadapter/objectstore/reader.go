package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/plugin"
	engineobjectstore "github.com/addp/common/engine/plugins/objectstore"
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

func (r *Reader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
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

func (r *Reader) Stat(ctx context.Context, ref contentio.Ref) (*contentio.Metadata, error) {
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
		return &contentio.Metadata{Ref: ref, Exists: exists}, nil
	}

	info, err := r.client.StatObject(ctx, bucket, key, miniogo.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	return &contentio.Metadata{
		Ref:         ref,
		Size:        info.Size,
		ContentType: info.ContentType,
		ModifiedAt:  nonZeroTimePtr(info.LastModified),
		Exists:      true,
	}, nil
}

func (r *Reader) List(ctx context.Context, scope contentio.Ref) ([]contentio.Ref, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("object store reader is not initialized")
	}
	bucket, prefix := splitObjectStoreRef(scope)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object resource scope: %s", scope.Path)
	}

	objectCh := r.client.ListObjects(ctx, bucket, miniogo.ListObjectsOptions{
		Prefix:    contentio.EnsurePrefix(prefix),
		Recursive: true,
	})

	refs := []contentio.Ref{}
	for obj := range objectCh {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if strings.HasSuffix(obj.Key, "/") {
			continue
		}
		refs = append(refs, contentio.NewRef(bucket+"/"+obj.Key, contentio.RoleMain))
	}
	if len(refs) == 0 {
		return nil, contentio.ErrContentNotFound
	}
	return refs, nil
}

func (r *Reader) Put(ctx context.Context, ref contentio.Ref, content io.Reader, contentType string, size int64) error {
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

func (r *Reader) ReadText(ctx context.Context, ref contentio.Ref) (string, error) {
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

func splitObjectStoreRef(ref contentio.Ref) (bucket, key string) {
	return engineobjectstore.SplitBucketPrefix(ref.Path)
}

func nonZeroTimePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
