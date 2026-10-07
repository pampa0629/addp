package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/minio/minio-go/v7"
)

type MinIOManagedObjectCleaner struct {
	minioClient *minio.Client
	bucket      string
}

func NewMinIOManagedObjectCleaner(minioClient *minio.Client, bucket string) *MinIOManagedObjectCleaner {
	return &MinIOManagedObjectCleaner{
		minioClient: minioClient,
		bucket:      strings.TrimSpace(bucket),
	}
}

func (c *MinIOManagedObjectCleaner) DeleteByStorageRef(ctx context.Context, storageRef string) error {
	if c == nil || c.minioClient == nil {
		return errors.New("managed object MinIO cleaner is not configured")
	}
	bucket, objectName, err := rastercogref.ObjectLocation(storageRef, c.bucket)
	if err != nil {
		return err
	}
	if err := c.minioClient.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete managed object %q/%q: %w", bucket, objectName, err)
	}
	return nil
}
