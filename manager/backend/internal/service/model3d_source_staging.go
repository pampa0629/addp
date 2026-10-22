package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

// This is an input I/O primitive, not an authorization boundary. It performs no
// Engine lookup, FTP protocol handling or catalog enumeration. The source adapter
// must supply the current execution's authorized, exact source composition.
type model3DStagingFile struct {
	RelativePath string
	SizeBytes    int64
	Open         func(context.Context) (io.ReadCloser, error)
}

type model3DStagingStore interface {
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

// withStagedModel3DSource scopes all temporary writes to this preparation. Even
// partial uploads and failed conversions are cleaned without using a cancelled
// execution context. No source credentials enter the returned access plan.
func (e *ManagerModel3DGLBExecutor) withStagedModel3DSource(
	ctx context.Context,
	lease commonExecution.Lease,
	sourceFormat, sourceKind, entrypoint string,
	files []model3DStagingFile,
	owner model3DStagingOwnership,
	consume func(context.Context, workflowaccess.Source) error,
) (resultErr error) {
	if e == nil || e.objectStore == nil || consume == nil || owner == nil {
		return errors.New("model input staging is not configured")
	}
	store, ok := e.objectStore.(model3DStagingStore)
	if !ok {
		return errors.New("model input staging requires object upload support")
	}
	files = append([]model3DStagingFile(nil), files...)
	if err := validateModel3DStagingFiles(lease, sourceFormat, sourceKind, entrypoint, files); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	bucket := e.defaultBucket
	if bucket == "" {
		return errors.New("model input staging bucket is required")
	}
	if err := e.verifyTemporaryModelExpiration(ctx, bucket); err != nil {
		return err
	}
	// A preparation nonce also isolates overlapping calls for the same attempt.
	preparationID := uuid.NewString()
	prefix := fmt.Sprintf("temp/model3d-input/tenant_%d/%s/attempt_%d/%s", lease.TenantID, lease.ExecutionID, lease.Attempt, preparationID)
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
	if _, err := workflowaccess.NewSourcePlan(source); err != nil {
		return err
	}
	preparation := repository.Model3DGLBInputPreparation{ExecutionID: lease.ExecutionID, Attempt: lease.Attempt, PreparationID: preparationID}
	for _, file := range files {
		preparation.Objects = append(preparation.Objects, repository.Model3DGLBInputObject{Bucket: bucket, Object: prefix + "/" + file.RelativePath})
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
		limited := &io.LimitedReader{R: &model3DStagingContextReader{ctx: ctx, reader: reader}, N: file.SizeBytes + 1}
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

func validateModel3DStagingFiles(lease commonExecution.Lease, sourceFormat, sourceKind, entrypoint string, files []model3DStagingFile) error {
	executionID, err := uuid.Parse(lease.ExecutionID)
	if err != nil || executionID == uuid.Nil || executionID.String() != lease.ExecutionID || lease.TenantID <= 0 || lease.Attempt <= 0 {
		return errors.New("model input staging requires an exact execution attempt")
	}
	if strings.TrimSpace(sourceFormat) == "" || sourceFormat != strings.TrimSpace(sourceFormat) || !validModel3DStagingPath(entrypoint) || len(files) == 0 {
		return errors.New("model input composition is incomplete")
	}
	if sourceKind != workflowaccess.KindFile && sourceKind != workflowaccess.KindDirectory {
		return errors.New("model input kind must be file or directory")
	}
	if sourceKind == workflowaccess.KindFile && (len(files) != 1 || entrypoint != path.Base(entrypoint)) {
		return errors.New("single model input must contain only its entrypoint")
	}
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		if !validModel3DStagingPath(file.RelativePath) || file.Open == nil || file.SizeBytes < 0 || file.SizeBytes == math.MaxInt64 || seen[file.RelativePath] {
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

func validModel3DStagingPath(name string) bool {
	return name != "" && name != "." && utf8.ValidString(name) && !strings.ContainsAny(name, "\\\x00\r\n:") &&
		!strings.HasPrefix(name, "/") && name != ".." && !strings.HasPrefix(name, "../") && path.Clean(name) == name
}

type model3DStagingContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *model3DStagingContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// bufferExchangeModelStagingEntrypoint accepts the caller's already-authorized main-file
// stream, not a locator or authorization proof. The shared parser bounds input.
// Only these parsed bytes may subsequently be staged; reopening the source would
// invalidate the reference check. No dependency is opened by this primitive.
func bufferExchangeModelStagingEntrypoint(ctx context.Context, main io.Reader, formatName string, declaredRefs []string) ([]byte, error) {
	if main == nil || declaredRefs == nil || (formatName != "3ds" && formatName != "dae") {
		return nil, errors.New("model input requires complete frozen reference declarations")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected := slices.Clone(declaredRefs)
	slices.Sort(expected)
	for i, ref := range expected {
		if ref == "" || (i > 0 && ref == expected[i-1]) {
			return nil, errors.New("model input reference declarations are invalid")
		}
	}
	provider, err := format.GetModel3DInfoProvider(format.FormatType(formatName))
	if err != nil {
		return nil, fmt.Errorf("resolve model input parser: %w", err)
	}
	var buffered bytes.Buffer
	facts, err := provider.DescribeModel3D(ctx, io.TeeReader(&model3DStagingContextReader{ctx: ctx, reader: main}, &buffered), nil)
	if err != nil {
		return nil, fmt.Errorf("inspect model input references: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if facts == nil || facts.FormatInfo["scan_complete"] != true {
		return nil, errors.New("model input reference scan is incomplete")
	}
	actual, ok := facts.FormatInfo["texture_refs"].([]string)
	if !ok || actual == nil {
		return nil, errors.New("model input reference declarations are missing")
	}
	actual = slices.Clone(actual)
	slices.Sort(actual)
	if !slices.Equal(actual, expected) {
		return nil, errors.New("model input references changed from the frozen composition")
	}
	return buffered.Bytes(), nil
}
