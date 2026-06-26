package service

import (
	"context"
	"errors"
	"strings"

	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/minio/minio-go/v7"
)

type MinIORasterCOGCleaner struct {
	minioClient *minio.Client
	bucket      string
}

func NewMinIORasterCOGCleaner(minioClient *minio.Client, bucket string) *MinIORasterCOGCleaner {
	return &MinIORasterCOGCleaner{
		minioClient: minioClient,
		bucket:      strings.TrimSpace(bucket),
	}
}

func (c *MinIORasterCOGCleaner) DeleteByStorageRef(ctx context.Context, storageRef string) error {
	if c == nil || c.minioClient == nil {
		return errors.New("raster COG MinIO cleaner is not configured")
	}
	bucket, objectName, err := rastercogref.ObjectLocation(storageRef, c.bucket)
	if err != nil {
		return err
	}
	return c.minioClient.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})
}
