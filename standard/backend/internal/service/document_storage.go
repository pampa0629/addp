package service

import (
	"context"
	"io"

	minio "github.com/minio/minio-go/v7"
)

// documentObjectStore isolates document lifecycle logic from the MinIO SDK.
type documentObjectStore interface {
	BucketExists(context.Context, string) (bool, error)
	MakeBucket(context.Context, string, minio.MakeBucketOptions) error
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
	GetObject(context.Context, string, string, minio.GetObjectOptions) (io.ReadCloser, error)
}

type minioDocumentObjectStore struct {
	client *minio.Client
}

func newMinioDocumentObjectStore(client *minio.Client) documentObjectStore {
	if client == nil {
		return nil
	}
	return &minioDocumentObjectStore{client: client}
}

func (s *minioDocumentObjectStore) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return s.client.BucketExists(ctx, bucket)
}

func (s *minioDocumentObjectStore) MakeBucket(ctx context.Context, bucket string, opts minio.MakeBucketOptions) error {
	return s.client.MakeBucket(ctx, bucket, opts)
}

func (s *minioDocumentObjectStore) PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return s.client.PutObject(ctx, bucket, key, reader, size, opts)
}

func (s *minioDocumentObjectStore) RemoveObject(ctx context.Context, bucket, key string, opts minio.RemoveObjectOptions) error {
	return s.client.RemoveObject(ctx, bucket, key, opts)
}

func (s *minioDocumentObjectStore) StatObject(ctx context.Context, bucket, key string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return s.client.StatObject(ctx, bucket, key, opts)
}

func (s *minioDocumentObjectStore) GetObject(ctx context.Context, bucket, key string, opts minio.GetObjectOptions) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, bucket, key, opts)
}
