package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/tilecache"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

type PostGISPMTilesArchiveGenerator interface {
	Generate(context.Context, mvt.QuickViewConfig, mvt.ProgressSink) (*mvt.GeneratedPMTilesArchive, error)
}

type ManagerPostGISVectorTileCacheExecutor struct {
	generator   PostGISPMTilesArchiveGenerator
	objectStore *minio.Client
	bucket      string
}

func NewManagerPostGISVectorTileCacheExecutor(generator PostGISPMTilesArchiveGenerator, objectStore *minio.Client, bucket string) *ManagerPostGISVectorTileCacheExecutor {
	return &ManagerPostGISVectorTileCacheExecutor{generator: generator, objectStore: objectStore, bucket: strings.TrimSpace(bucket)}
}

func (e *ManagerPostGISVectorTileCacheExecutor) GenerateMixed(ctx context.Context, cfg mvt.QuickViewConfig, progress mvt.ProgressSink) (*mvt.GenerateResult, error) {
	if e == nil || e.generator == nil || e.objectStore == nil {
		return nil, errors.New("PostGIS vector tile cache executor is not fully configured")
	}
	archive, err := e.generator.Generate(ctx, cfg, progress)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	if err := validatePMTilesFile(archive.Path); err != nil {
		return nil, fmt.Errorf("validate generated PostGIS PMTiles: %w", err)
	}
	bucket, objectName, err := tilecache.ObjectLocation(cfg.StorageRef, e.bucket)
	if err != nil {
		return nil, err
	}
	if err := ensurePMTilesBucket(ctx, e.objectStore, bucket); err != nil {
		return nil, err
	}
	tempObject := objectName + "." + uuid.NewString() + ".partial"
	file, err := os.Open(archive.Path)
	if err != nil {
		return nil, err
	}
	_, putErr := e.objectStore.PutObject(ctx, bucket, tempObject, file, archive.Size, minio.PutObjectOptions{ContentType: "application/vnd.pmtiles"})
	closeErr := file.Close()
	if putErr != nil {
		_ = e.objectStore.RemoveObject(ctx, bucket, tempObject, minio.RemoveObjectOptions{})
		return nil, putErr
	}
	if closeErr != nil {
		_ = e.objectStore.RemoveObject(ctx, bucket, tempObject, minio.RemoveObjectOptions{})
		return nil, closeErr
	}
	if _, _, err := validatePMTilesObject(ctx, e.objectStore, bucket, tempObject); err != nil {
		_ = e.objectStore.RemoveObject(ctx, bucket, tempObject, minio.RemoveObjectOptions{})
		return nil, fmt.Errorf("validate uploaded PostGIS PMTiles: %w", err)
	}
	_, err = e.objectStore.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: bucket, Object: objectName},
		minio.CopySrcOptions{Bucket: bucket, Object: tempObject},
	)
	_ = e.objectStore.RemoveObject(ctx, bucket, tempObject, minio.RemoveObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("commit PostGIS PMTiles cache: %w", err)
	}
	archive.Result.ArchiveHeaderHash = archive.HeaderHash
	archive.Result.ArchiveSizeBytes = archive.Size
	return archive.Result, nil
}

func ensurePMTilesBucket(ctx context.Context, client *minio.Client, bucket string) error {
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check PMTiles bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create PMTiles bucket: %w", err)
	}
	return nil
}
