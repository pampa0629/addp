package minio

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/addp/common/config"
	"github.com/addp/common/contentio"
	"github.com/addp/common/infra/objectstore"
	sdk "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

type areaStore struct {
	client *sdk.Client
	bucket string
}

func Bind(ctx context.Context, area, bucket string, cfg config.BuiltinMinIOConfig) (objectstore.Area, error) {
	if err := s3utils.CheckValidBucketNameStrict(bucket); err != nil {
		return nil, fmt.Errorf("object area %q: invalid MinIO bucket %q: %w", area, bucket, err)
	}
	client, err := sdk.New(cfg.Endpoint, &sdk.Options{
		Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("object area %q, bucket %q: %w", area, bucket, err)
	}
	// Preserve non-file functionality and the binding while storage recovers.
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		log.Printf("object area %q, MinIO bucket %q: startup check failed: %v", area, bucket, err)
	} else if !exists {
		log.Printf("object area %q, MinIO bucket %q: bucket missing", area, bucket)
	}
	return New(client, bucket), nil
}

func (s *areaStore) Put(ctx context.Context, key string, body io.Reader, size int64, mediaType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, sdk.PutObjectOptions{ContentType: mediaType})
	return err
}

func (s *areaStore) Stat(ctx context.Context, key string) (*contentio.Stat, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, sdk.StatObjectOptions{})
	if sdk.ToErrorResponse(err).Code == "NoSuchKey" {
		return nil, contentio.ErrContentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &contentio.Stat{Ref: contentio.NewRef(key, contentio.RoleMain), Exists: true, Size: info.Size, ContentType: info.ContentType, ModifiedAt: &info.LastModified}, nil
}

func (s *areaStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, key, sdk.GetObjectOptions{})
}

func (s *areaStore) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, sdk.RemoveObjectOptions{})
}

func (s *areaStore) OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	opts := sdk.GetObjectOptions{}
	end := int64(-1)
	if length > 0 {
		end = offset + length - 1
	}
	if err := opts.SetRange(offset, end); err != nil {
		return nil, err
	}
	return s.client.GetObject(ctx, s.bucket, key, opts)
}
func (s *areaStore) List(ctx context.Context, prefix string) ([]contentio.Ref, error) {
	var refs []contentio.Ref
	for o := range s.client.ListObjects(ctx, s.bucket, sdk.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if o.Err != nil {
			return nil, o.Err
		}
		refs = append(refs, contentio.NewRef(o.Key, contentio.RoleMain))
	}
	return refs, ctx.Err()
}

// New wraps an existing client for callers that already own its transport settings.
func New(client *sdk.Client, bucket string) objectstore.ContentArea {
	return &areaStore{client: client, bucket: bucket}
}
