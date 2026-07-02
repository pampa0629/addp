package service

import (
	"context"
	"strings"

	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/minio/minio-go/v7"
)

type MinIOPointCloudCOPCCleaner struct {
	minioClient   *minio.Client
	defaultBucket string
}

func NewMinIOPointCloudCOPCCleaner(minioClient *minio.Client, bucket string) *MinIOPointCloudCOPCCleaner {
	return &MinIOPointCloudCOPCCleaner{minioClient: minioClient, defaultBucket: bucket}
}

func (c *MinIOPointCloudCOPCCleaner) DeleteByStorageRef(ctx context.Context, storageRef string) error {
	if c == nil || c.minioClient == nil || strings.TrimSpace(storageRef) == "" {
		return nil
	}
	bucket, objectName, err := rastercogref.ObjectLocation(storageRef, c.defaultBucket)
	if err != nil {
		return err
	}
	return c.minioClient.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})
}
