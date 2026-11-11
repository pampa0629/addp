package service

import (
	"context"
	"crypto/sha256"
	"io"
	"path"

	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
)

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
		// These are read-capability constraints on the formal Meta composition,
		// not a second classification or a new authorization object.
		if (ref.Primary && ref.Role != "image") || (!ref.Primary && (ref.Role != "auxiliary_metadata" || ref.Target.StringPath() != primary.StringPath()+".aux.xml")) {
			return ErrFileSourceAuthorizationRequired
		}
		found := false
		for _, version := range req.Config.Raster.ContentVersions {
			if version.Path != ref.Target.StringPath() {
				continue
			}
			size, err := reader.stat(ctx, ref.Target)
			if err != nil {
				return err
			}
			if size != version.SizeBytes {
				return ErrFileSourceAuthorizationRequired
			}
			target, expected := ref.Target, version.SHA256
			files = append(files, fileStagingFile{RelativePath: path.Base(version.Path), SizeBytes: size, Open: func(ctx context.Context) (io.ReadCloser, error) {
				stream, err := reader.open(ctx, target)
				if err != nil {
					return nil, err
				}
				return &fileVersionReader{ReadCloser: stream, digest: sha256.New(), expected: expected}, nil
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
	return stager.withSource(ctx, lease, "tiff", workflowaccess.KindFile, path.Base(primary.StringPath()), files, req.inputOwner, func(ctx context.Context, source workflowaccess.Source) error {
		if err := auth.Checkpoint(ctx); err != nil {
			return err
		}
		return consume(ctx, source)
	})
}
