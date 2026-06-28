package service

import (
	"context"
	"errors"

	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/minio/minio-go/v7"
)

type MinIOModel3DQuickViewCleaner struct {
	minioClient   *minio.Client
	defaultBucket string
}

func NewMinIOModel3DQuickViewCleaner(minioClient *minio.Client, bucket string) *MinIOModel3DQuickViewCleaner {
	return &MinIOModel3DQuickViewCleaner{minioClient: minioClient, defaultBucket: bucket}
}

func (c *MinIOModel3DQuickViewCleaner) DeleteByStorageRef(ctx context.Context, storageRef string) error {
	if c == nil || c.minioClient == nil {
		return errors.New("model 3d quick view MinIO cleaner is not configured")
	}
	bucket, objectName, err := rastercogref.ObjectLocation(storageRef, c.defaultBucket)
	if err != nil {
		return err
	}
	return c.minioClient.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})
}
