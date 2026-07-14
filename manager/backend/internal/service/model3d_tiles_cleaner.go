package service

import (
	"context"
	"errors"
	"strings"

	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/minio/minio-go/v7"
)

type MinIOModel3DTilesCleaner struct {
	objectStore   model3DTilesObjectStore
	defaultBucket string
}

func NewMinIOModel3DTilesCleaner(objectStore *minio.Client, defaultBucket string) *MinIOModel3DTilesCleaner {
	return &MinIOModel3DTilesCleaner{objectStore: objectStore, defaultBucket: strings.TrimSpace(defaultBucket)}
}

func (c *MinIOModel3DTilesCleaner) DeleteByStorageRef(ctx context.Context, storageRef string) error {
	if c == nil || c.objectStore == nil {
		return errors.New("model3d tiles MinIO cleaner is not configured")
	}
	bucket, prefix, err := rastercogref.ObjectLocation(storageRef, c.defaultBucket)
	if err != nil {
		return err
	}
	if strings.Trim(prefix, "/") == "" {
		return errors.New("model3d tiles storage prefix is required")
	}
	return deleteModel3DTilesPrefix(ctx, c.objectStore, bucket, prefix)
}
