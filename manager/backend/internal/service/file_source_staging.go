package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"io"
	"math"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

type fileInputObjectStore interface {
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
	GetBucketLifecycle(context.Context, string) (*lifecycle.Configuration, error)
	GetBucketVersioning(context.Context, string) (minio.BucketVersioningConfiguration, error)
}
type fileSourceStager struct {
	objectStore                                                            fileInputObjectStore
	minioEndpoint, minioAccessKey, minioSecretKey, defaultBucket, taskType string
	minioUseSSL                                                            bool
}

// This is an input I/O primitive, not an authorization boundary. It performs no
// Engine lookup, FTP protocol handling or catalog enumeration. The source adapter
// must supply the current execution's authorized, exact source composition.
type fileStagingFile struct {
	RelativePath string
	Role         string
	SizeBytes    int64
	Open         func(context.Context) (io.ReadCloser, error)
}

type fileStagingStore interface {
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

// withStagedModel3DSource scopes all temporary writes to this preparation. Even
// partial uploads and failed conversions are cleaned without using a cancelled
// execution context. No source credentials enter the returned access plan.
func (e *fileSourceStager) withSource(
	ctx context.Context,
	lease commonExecution.Lease,
	sourceFormat, sourceKind, entrypoint string,
	files []fileStagingFile,
	owner fileStagingOwnership,
	consume func(context.Context, workflowaccess.Source) error,
) (resultErr error) {
	if e == nil || e.objectStore == nil || consume == nil || owner == nil {
		return errors.New("model input staging is not configured")
	}
	store, ok := e.objectStore.(fileStagingStore)
	if !ok {
		return errors.New("model input staging requires object upload support")
	}
	files = append([]fileStagingFile(nil), files...)
	if err := validateFileStagingFiles(lease, sourceFormat, sourceKind, entrypoint, files); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	bucket := e.defaultBucket
	if bucket == "" {
		return errors.New("model input staging bucket is required")
	}
	if err := verifyTemporaryInputExpiration(ctx, e.objectStore, bucket); err != nil {
		return err
	}
	// A preparation nonce also isolates overlapping calls for the same attempt.
	preparationID := uuid.NewString()
	preparation := repository.FileInputPreparation{ExecutionID: lease.ExecutionID, Attempt: lease.Attempt, PreparationID: preparationID}
	prefixWithSlash, prefixErr := preparation.Prefix(uint(lease.TenantID), e.taskType)
	if prefixErr != nil {
		return prefixErr
	}
	prefix := strings.TrimSuffix(prefixWithSlash, "/")
	objectOrPrefix := prefix
	if sourceKind == workflowaccess.KindFile {
		objectOrPrefix += "/" + entrypoint
	}
	access, err := workflowaccess.ResolveObjectStoreTarget(plugin.ConnectionInfo{
		"endpoint": e.minioEndpoint, "access_key": e.minioAccessKey, "secret_key": e.minioSecretKey, "use_ssl": e.minioUseSSL,
	}, bucket, objectOrPrefix, sourceKind)
	if err != nil {
		return err
	}
	source := workflowaccess.Source{Kind: sourceKind, Format: sourceFormat, Entrypoint: entrypoint, Access: access}
	if sourceFormat == "envi" || sourceFormat == "tiff" {
		if e.taskType != commonExecution.TaskTypeRasterCOGGeneration {
			return errors.New("raster input requires the raster COG purpose")
		}
		source.Entrypoint = ""
		for _, file := range files {
			if file.RelativePath != entrypoint {
				header := access
				header.Object = prefix + "/" + file.RelativePath
				role := "header"
				if sourceFormat == "tiff" {
					role = file.Role
				}
				source.RelatedFiles = append(source.RelatedFiles, workflowaccess.RelatedFile{Role: role, Access: header})
			}
		}
	} else if e.taskType != commonExecution.TaskTypeModel3DGLBGeneration {
		return errors.New("model input requires the model GLB purpose")
	}

	if _, err := workflowaccess.NewSourcePlan(source); err != nil {
		return err
	}
	for _, file := range files {
		preparation.Objects = append(preparation.Objects, repository.FileInputObject{Bucket: bucket, Object: prefix + "/" + file.RelativePath})
	}
	if err := owner.prepare(ctx, preparation); err != nil {
		return fmt.Errorf("persist exact model input ownership: %w", err)
	}
	writesSettled := true
	var attempted []string
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		cleaned := true
		for _, object := range attempted {
			if err := e.objectStore.RemoveObject(cleanupCtx, bucket, object, minio.RemoveObjectOptions{}); err != nil {
				cleaned = false
				resultErr = errors.Join(resultErr, fmt.Errorf("clean temporary model input: %w", err))
			}
		}
		resultErr = errors.Join(resultErr, owner.finish(cleanupCtx, preparation, writesSettled, cleaned))
	}()
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		reader, err := file.Open(ctx)
		if err != nil {
			if reader != nil {
				err = errors.Join(err, reader.Close())
			}
			return fmt.Errorf("open model input: %w", err)
		}
		if reader == nil {
			return errors.New("model input reader is missing")
		}
		object := prefix + "/" + file.RelativePath
		// Record before PutObject: an error response does not prove no object was written.
		attempted = append(attempted, object)
		limited := &io.LimitedReader{R: &sourceContextReader{ctx: ctx, reader: reader}, N: file.SizeBytes + 1}
		writesSettled = false // A panic or unknown PUT response cannot settle the producer.
		info, putErr := store.PutObject(ctx, bucket, object, limited, file.SizeBytes, minio.PutObjectOptions{ContentType: "application/octet-stream"})
		writesSettled = putErr == nil
		var sizeErr error
		if putErr == nil {
			// A length-delimited uploader may stop before EOF. Probe one byte to
			// reject a changed source even when it reports the expected upload size.
			var extra [1]byte
			n, probeErr := io.ReadFull(limited, extra[:])
			if limited.N != 1 || info.Size != file.SizeBytes || n != 0 || probeErr != io.EOF {
				sizeErr = errors.New("model input size changed during staging")
				if probeErr != nil && probeErr != io.EOF {
					sizeErr = errors.Join(sizeErr, probeErr)
				}
			}
		}
		closeErr := reader.Close()
		if putErr != nil || sizeErr != nil || closeErr != nil {
			return errors.Join(putErr, sizeErr, closeErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return consume(ctx, source)
}

func validateFileStagingFiles(lease commonExecution.Lease, sourceFormat, sourceKind, entrypoint string, files []fileStagingFile) error {
	executionID, err := uuid.Parse(lease.ExecutionID)
	if err != nil || executionID == uuid.Nil || executionID.String() != lease.ExecutionID || lease.TenantID <= 0 || lease.Attempt <= 0 {
		return errors.New("model input staging requires an exact execution attempt")
	}
	if strings.TrimSpace(sourceFormat) == "" || sourceFormat != strings.TrimSpace(sourceFormat) || !validFileStagingPath(entrypoint) || len(files) == 0 {
		return errors.New("model input composition is incomplete")
	}
	if sourceKind != workflowaccess.KindFile && sourceKind != workflowaccess.KindDirectory {
		return errors.New("model input kind must be file or directory")
	}
	if sourceKind == workflowaccess.KindFile && ((sourceFormat != "envi" && sourceFormat != "tiff" && len(files) != 1) || (sourceFormat == "envi" && len(files) != 2) || entrypoint != path.Base(entrypoint)) {
		return errors.New("single model input must contain only its entrypoint")
	}
	if sourceFormat == "tiff" {
		for _, file := range files {
			if sourceKind != workflowaccess.KindFile || file.RelativePath != path.Base(file.RelativePath) {
				return errors.New("TIFF staging requires its exact same-directory composition")
			}
		}
	}
	if sourceFormat == "envi" {
		headers := 0
		for _, file := range files {
			if file.RelativePath != entrypoint {
				if path.Base(file.RelativePath) != file.RelativePath || !strings.EqualFold(path.Ext(file.RelativePath), ".hdr") {
					return errors.New("ENVI staging requires one exact same-directory header")
				}
				headers++
			}
		}
		if sourceKind != workflowaccess.KindFile || headers != 1 {
			return errors.New("ENVI staging requires one primary and one header")
		}
	}
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		if !validFileStagingPath(file.RelativePath) || file.Open == nil || file.SizeBytes < 0 || file.SizeBytes == math.MaxInt64 || seen[file.RelativePath] {
			return errors.New("model input contains an invalid or duplicate file")
		}
		seen[file.RelativePath] = true
	}
	if !seen[entrypoint] {
		return errors.New("model input entrypoint is missing")
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return errors.New("model input file conflicts with a directory")
			}
		}
	}
	return nil
}

func validFileStagingPath(name string) bool {
	return name != "" && name != "." && utf8.ValidString(name) && !strings.ContainsAny(name, "\\\x00\r\n:") &&
		!strings.HasPrefix(name, "/") && name != ".." && !strings.HasPrefix(name, "../") && path.Clean(name) == name
}

func verifyTemporaryInputExpiration(ctx context.Context, store fileInputObjectStore, bucket string) error {
	versioning, err := store.GetBucketVersioning(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check model temporary bucket versioning: %w", err)
	}
	if versioning.Status != "" {
		return errors.New("model temporary bucket must be unversioned for permanent object expiration")
	}
	config, err := store.GetBucketLifecycle(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check model temporary object expiration: %w", err)
	}
	if config != nil {
		for _, rule := range config.Rules {
			filter := rule.RuleFilter
			if rule.Status == "Enabled" && rule.Prefix == "" && filter.Prefix == "temp/" &&
				filter.And.IsEmpty() && filter.Tag.IsEmpty() &&
				filter.ObjectSizeLessThan == 0 && filter.ObjectSizeGreaterThan == 0 &&
				rule.Expiration.Days == 7 && rule.Expiration.Date.IsZero() {
				return nil
			}
		}
	}
	return errors.New("model temporary objects require the Infra temp/ 7-day expiration rule on the target bucket")
}
