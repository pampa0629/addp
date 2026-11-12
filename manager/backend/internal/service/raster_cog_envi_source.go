package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"path"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format/plugins/envi"
	commonModels "github.com/addp/common/models"
)

func (e *ManagerRasterCOGExecutor) withENVISource(ctx context.Context, req RasterCOGExecutionRequest, consume func(context.Context, workflowaccess.Source) error) error {
	auth := req.SourceAuthorization
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if e == nil || req.Task == nil || auth == nil || auth.validateConfig(req.Config) != nil || req.inputOwner == nil || consume == nil || !ok || lease.ExecutionID != req.ExecutionID || lease.TenantID != int(req.Task.TenantID) || !authorizationfacts.ValidItemFingerprint(req.Config.Raster.HeaderSHA256) || !authorizationfacts.ValidItemFingerprint(req.Config.Raster.RasterSHA256) {
		return ErrFileSourceAuthorizationRequired
	}
	composition := auth.Composition()
	if composition == nil || composition.Format != "envi" {
		return ErrFileSourceAuthorizationRequired
	}
	primary, err := composition.PrimaryTarget()
	if err != nil {
		return err
	}
	var header plugin.EngineCatalogPath
	for _, ref := range composition.Refs {
		if ref.Role == "header" {
			header = ref.Target
		}
	}
	reader, err := newRasterSourceReader(ctx, auth, req.Task.TenantID, req.Config.Target.SourceEngineID)
	if err != nil {
		return err
	}
	stat := func(target plugin.EngineCatalogPath) (int64, error) { return reader.stat(ctx, target) }
	open := reader.open
	headerSize, err := stat(header)
	if err != nil {
		return err
	}
	if headerSize <= 0 || headerSize > envi.MaxHeaderBytes {
		return ErrFileSourceAuthorizationRequired
	}
	stream, err := open(ctx, header)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(stream, envi.MaxHeaderBytes+1))
	closeErr := stream.Close()
	parsed, parseErr := envi.ParseHeader(ctx, raw)
	if readErr != nil || closeErr != nil || parseErr != nil || int64(len(raw)) != headerSize || parsed.SHA256 != req.Config.Raster.HeaderSHA256 {
		return ErrFileSourceAuthorizationRequired
	}
	size, err := stat(primary)
	if err != nil {
		return err
	}
	if size != parsed.FileBytes || size+headerSize != req.Config.Raster.SourceSizeBytes {
		return ErrFileSourceAuthorizationRequired
	}
	store, ok := e.objectStore.(fileInputObjectStore)
	if !ok {
		return ErrFileSourceAuthorizationUnavailable
	}
	stager := fileSourceStager{objectStore: store, minioEndpoint: e.infraEndpoint, minioAccessKey: e.infraAccessKey, minioSecretKey: e.infraSecretKey, minioUseSSL: e.infraUseSSL, defaultBucket: e.defaultBucket, taskType: commonExecution.TaskTypeRasterCOGGeneration}
	files := []fileStagingFile{
		{RelativePath: path.Base(primary.StringPath()), SizeBytes: size, Open: func(ctx context.Context) (io.ReadCloser, error) {
			reader, err := open(ctx, primary)
			if err != nil {
				return nil, err
			}
			return &fileVersionReader{ReadCloser: reader, digest: sha256.New(), expected: req.Config.Raster.RasterSHA256, mismatch: ErrFileSourceAuthorizationRequired}, nil
		}},
		{RelativePath: path.Base(header.StringPath()), SizeBytes: headerSize, Open: func(ctx context.Context) (io.ReadCloser, error) {
			if err := auth.Checkpoint(ctx); err != nil {
				return nil, err
			}
			return io.NopCloser(bytes.NewReader(raw)), nil
		}},
	}
	return stager.withSource(ctx, lease, "envi", workflowaccess.KindFile, path.Base(primary.StringPath()), files, req.inputOwner, func(ctx context.Context, source workflowaccess.Source) error {
		source.Metadata = commonModels.JSONMap{"expected_header_sha256": req.Config.Raster.HeaderSHA256}
		if err := auth.Checkpoint(ctx); err != nil {
			return err
		}
		return consume(ctx, source)
	})
}

type fileVersionReader struct {
	io.ReadCloser
	digest   hash.Hash
	expected string
	mismatch error
}

func (r *fileVersionReader) Read(buffer []byte) (int, error) {
	n, err := r.ReadCloser.Read(buffer)
	if n > 0 {
		_, _ = r.digest.Write(buffer[:n])
	}
	if err == io.EOF && hex.EncodeToString(r.digest.Sum(nil)) != r.expected {
		return n, r.mismatch
	}
	return n, err
}
