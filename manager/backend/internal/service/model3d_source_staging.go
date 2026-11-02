package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
)

func (e *ManagerModel3DGLBExecutor) withStagedModel3DSource(ctx context.Context, lease commonExecution.Lease, sourceFormat, sourceKind, entrypoint string, files []fileStagingFile, owner fileStagingOwnership, consume func(context.Context, workflowaccess.Source) error) error {
	if e == nil {
		return errors.New("model input staging is not configured")
	}
	stager := fileSourceStager{objectStore: e.objectStore, minioEndpoint: e.minioEndpoint, minioAccessKey: e.minioAccessKey, minioSecretKey: e.minioSecretKey, minioUseSSL: e.minioUseSSL, defaultBucket: e.defaultBucket, taskType: commonExecution.TaskTypeModel3DGLBGeneration}
	return stager.withSource(ctx, lease, sourceFormat, sourceKind, entrypoint, files, owner, consume)
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
	facts, err := provider.DescribeModel3D(ctx, io.TeeReader(&sourceContextReader{ctx: ctx, reader: main}, &buffered), nil)
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
