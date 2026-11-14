package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	miniogo "github.com/minio/minio-go/v7"
)

type workflowExportGate interface {
	PrepareWorkflowResultExport(context.Context, uint, []dataprotection.ResourceReference) error
	ExportArtifactCurrent(context.Context, int64, string) (bool, error)
}

// Every actual open verifies the bounded JSON bytes consumed by the existing codec.
type workflowInputReader struct {
	plugin.StoreProvider
	binding *client.WorkflowResultExportSource
	path    plugin.EngineCatalogPath
}

func (r workflowInputReader) OpenContent(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if path.StringPath() != r.path.StringPath() || !time.Now().Before(r.binding.ExpiresAt) {
		return nil, fmt.Errorf("workflow input expired or changed")
	}
	storage, err := objectstore.NewClient(conn, false, true)
	if err != nil {
		return nil, err
	}
	bucket, key := objectstore.SplitBucketPrefix(path.StringPath())
	stream, err := storage.GetObject(ctx, bucket, key, miniogo.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(stream, 64<<10+1))
	closeErr := stream.Close()
	if readErr != nil || closeErr != nil || len(data) > 64<<10 || client.WorkflowResultRowsDigest(data) != r.binding.SHA256 || !time.Now().Before(r.binding.ExpiresAt) {
		return nil, fmt.Errorf("workflow input digest or deadline mismatch")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *ExecutionEngineService) configureWorkflowResultExport(ctx context.Context, task *models.TransferTask, item *commonExecution.TaskExecution, spec planner.TableExportTaskSpec, build *planner.TableTransferBuildResult, table *executor.TableTransferExecutor) error {
	if item.Source != "develop" || item.SourceTaskID != nil || item.Metadata[models.ExportProtectionMetadataKey] == nil || planner.ValidateWorkflowResultExport(spec, task.TenantID) != nil || !time.Now().Before(spec.WorkflowResult.ExpiresAt) {
		return fmt.Errorf("%w: invalid workflow export", executor.ErrProtectionPreparation)
	}
	gate, ok := s.protectionGate.(workflowExportGate)
	if !ok {
		return executor.ErrProtectionPreparation
	}
	if err := gate.PrepareWorkflowResultExport(ctx, task.TenantID, spec.WorkflowResult.Targets); err != nil {
		return err
	}
	table.SourceContentReader = workflowInputReader{StoreProvider: table.SourceContentReader, binding: spec.WorkflowResult, path: build.Plan.Source.Path}
	return nil
}

func (s *ExecutionEngineService) cleanupWorkflowResultInput(ctx context.Context, item *commonExecution.TaskExecution) {
	spec, err := planner.ParseTableExportTaskSpec(item.ExecutionConfig, 1000)
	if err != nil || item.Source != "develop" || item.SourceTaskID != nil || planner.ValidateWorkflowResultExport(spec, uint(item.TenantID)) != nil {
		return
	}
	loc, err := planner.ParseInfraLocatorURI(spec.Source.Locator)
	if err != nil {
		return
	}
	binding, err := s.infraEngineResolver().ResolveEngine(loc.EngineRef())
	if err != nil {
		return
	}
	provider, err := plugin.Get(binding.Type)
	if err != nil {
		return
	}
	deleter, ok := provider.(plugin.ResourceDeleteProvider)
	if !ok {
		return
	}
	path, err := loc.EngineCatalogPath()
	if err != nil {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := deleter.DeleteResource(cleanup, binding.ConnInfo, path); err != nil {
		s.logger.Error("workflow export private input cleanup failed", "execution_id", item.ExecutionID)
	}
}
