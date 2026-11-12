package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path"

	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/minio/minio-go/v7"
)

var ErrTIFFSourceCompositionChanged = commoni18n.NewMessageError(manageri18n.MsgTIFFSourceCompositionChanged)

func tiffSourceReadError(err error) error {
	var response minio.ErrorResponse
	if errors.Is(err, os.ErrNotExist) || (errors.As(err, &response) && response.Code == "NoSuchKey") {
		return errors.Join(ErrTIFFSourceCompositionChanged, err)
	}
	return err
}

func (e *ManagerRasterCOGExecutor) withTIFFSource(ctx context.Context, req RasterCOGExecutionRequest, consume func(context.Context, workflowaccess.Source) error) error {
	auth := req.SourceAuthorization
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if e == nil || req.Task == nil || auth == nil || auth.validateConfig(req.Config) != nil || req.inputOwner == nil || consume == nil || !ok || lease.ExecutionID != req.ExecutionID || lease.TenantID != int(req.Task.TenantID) {
		return ErrFileSourceAuthorizationRequired
	}
	composition := auth.Composition()
	if composition == nil || composition.Format != "tiff" || len(req.Config.Raster.ContentVersions) != len(composition.Refs) {
		return ErrFileSourceAuthorizationRequired
	}
	reader, err := newRasterSourceReader(ctx, auth, req.Task.TenantID, req.Config.Target.SourceEngineID)
	if err != nil {
		return err
	}
	primary, _ := composition.PrimaryTarget()
	files := make([]fileStagingFile, 0, len(composition.Refs))
	for _, ref := range composition.Refs {
		found := false
		for _, version := range req.Config.Raster.ContentVersions {
			if version.Path != ref.Target.StringPath() {
				continue
			}
			size, err := reader.stat(ctx, ref.Target)
			if err != nil {
				return tiffSourceReadError(err)
			}
			if size != version.SizeBytes {
				return ErrTIFFSourceCompositionChanged
			}
			target, expected := ref.Target, version.SHA256
			files = append(files, fileStagingFile{RelativePath: path.Base(version.Path), Role: ref.Role, SizeBytes: size, Open: func(ctx context.Context) (io.ReadCloser, error) {
				stream, err := reader.open(ctx, target)
				if err != nil {
					return nil, err
				}
				return &fileVersionReader{ReadCloser: stream, digest: sha256.New(), expected: expected, mismatch: ErrTIFFSourceCompositionChanged}, nil
			}})
			found = true
			break
		}
		if !found {
			return ErrFileSourceAuthorizationRequired
		}
	}
	store, ok := e.objectStore.(fileInputObjectStore)
	if !ok {
		return ErrFileSourceAuthorizationUnavailable
	}
	stager := fileSourceStager{objectStore: store, minioEndpoint: e.infraEndpoint, minioAccessKey: e.infraAccessKey, minioSecretKey: e.infraSecretKey, minioUseSSL: e.infraUseSSL, defaultBucket: e.defaultBucket, taskType: commonExecution.TaskTypeRasterCOGGeneration}
	err = stager.withSource(ctx, lease, "tiff", workflowaccess.KindFile, path.Base(primary.StringPath()), files, req.inputOwner, func(ctx context.Context, source workflowaccess.Source) error {
		if err := auth.Checkpoint(ctx); err != nil {
			return err
		}
		return consume(ctx, source)
	})
	return tiffSourceReadError(err)
}
