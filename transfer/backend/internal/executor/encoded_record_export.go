package executor

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
)

type EncodedRecordExportPlan struct {
	Source           EncodedRecordExportEndpointPlan
	Target           EncodedRecordExportEndpointPlan
	Format           string
	BatchSize        int
	BeforeEncode     engineplugin.EncodedRecordTransform
	ProgressCallback EncodedRecordExportProgressCallback
}

type EncodedRecordExportEndpointPlan struct {
	ConnInfo          engineplugin.ConnectionInfo
	Path              engineplugin.EngineCatalogPath
	ContentWrite      engineplugin.WriteOptions
	DeleteBeforeWrite bool
}

type EncodedRecordExportProgressCallback func(context.Context, EncodedRecordExportProgressEvent) error

type EncodedRecordExportProgressEvent struct {
	BatchIndex     int64
	SourceOffset   int64
	BatchRecords   int64
	RecordsRead    int64
	RecordsWritten int64
	BytesRead      int64
	BytesWritten   int64
	Final          bool
}

type EncodedRecordExportMetrics struct {
	RecordsRead    int64
	RecordsWritten int64
	BytesRead      int64
	BytesWritten   int64
}

type EncodedRecordExportExecutor struct {
	SourceRecordReader  engineplugin.EncodedRecordReadSessionProvider
	TargetContentWriter engineplugin.ContentWritableProvider
	TargetDelete        engineplugin.ResourceDeleteProvider
}

func NewEncodedRecordExportExecutor(sourceEngineType, targetEngineType string) (*EncodedRecordExportExecutor, error) {
	sourcePlugin, err := engineplugin.Get(sourceEngineType)
	if err != nil {
		return nil, fmt.Errorf("get source engine plugin %q: %w", sourceEngineType, err)
	}
	targetPlugin, err := engineplugin.Get(targetEngineType)
	if err != nil {
		return nil, fmt.Errorf("get target engine plugin %q: %w", targetEngineType, err)
	}
	exec := &EncodedRecordExportExecutor{}
	exec.SourceRecordReader, _ = sourcePlugin.(engineplugin.EncodedRecordReadSessionProvider)
	exec.TargetContentWriter, _ = targetPlugin.(engineplugin.ContentWritableProvider)
	exec.TargetDelete, _ = targetPlugin.(engineplugin.ResourceDeleteProvider)
	return exec, nil
}

func (e *EncodedRecordExportExecutor) Execute(ctx context.Context, plan EncodedRecordExportPlan) (*EncodedRecordExportMetrics, error) {
	if e == nil || e.SourceRecordReader == nil {
		return nil, fmt.Errorf("encoded record export requires source record reader")
	}
	if e.TargetContentWriter == nil {
		return nil, fmt.Errorf("encoded record export requires target content writer")
	}
	if plan.BatchSize <= 0 {
		return nil, fmt.Errorf("encoded record export batch size must be positive")
	}
	if plan.Target.DeleteBeforeWrite {
		if e.TargetDelete == nil {
			return nil, fmt.Errorf("encoded record export replace requires target resource delete")
		}
		if err := e.TargetDelete.DeleteResource(ctx, plan.Target.ConnInfo, plan.Target.Path); err != nil {
			return nil, fmt.Errorf("delete encoded record export target before write: %w", err)
		}
	}

	session, err := e.SourceRecordReader.OpenEncodedRecordReadSession(ctx, plan.Source.ConnInfo, plan.Source.Path, engineplugin.EncodedRecordReadSessionOptions{
		Format: plan.Format, BeforeEncode: plan.BeforeEncode,
	})
	if err != nil {
		return nil, fmt.Errorf("open encoded record source session: %w", err)
	}
	defer session.Close(context.Background()) //nolint:errcheck

	target := contentadapter.NewMappedWriter(e.TargetContentWriter, plan.Target.ConnInfo, contentadapter.FixedPathMapper(plan.Target.Path), plan.Target.ContentWrite)
	output, err := target.Create(ctx, contentRefFromCatalogPath(plan.Target.Path))
	if err != nil {
		return nil, fmt.Errorf("create encoded record export target: %w", err)
	}
	closed := false
	succeeded := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
		if !succeeded && e.TargetDelete != nil {
			_ = e.TargetDelete.DeleteResource(context.Background(), plan.Target.ConnInfo, plan.Target.Path)
		}
	}()

	metrics := &EncodedRecordExportMetrics{}
	var batchIndex int64
	for {
		batch, err := session.ReadBatch(ctx, plan.BatchSize)
		if err != nil {
			return metrics, fmt.Errorf("read encoded record batch: %w", err)
		}
		if batch == nil {
			return metrics, fmt.Errorf("encoded record reader returned nil batch")
		}
		if batch.Records == 0 {
			if len(batch.Content) != 0 {
				return metrics, fmt.Errorf("encoded record reader returned content without records")
			}
			break
		}
		if len(batch.Content) == 0 {
			return metrics, fmt.Errorf("encoded record reader returned records without content")
		}
		written, err := output.Write(batch.Content)
		metrics.RecordsRead += batch.Records
		metrics.BytesRead += int64(len(batch.Content))
		metrics.BytesWritten += int64(written)
		if err != nil {
			return metrics, fmt.Errorf("write encoded record batch: %w", err)
		}
		if written != len(batch.Content) {
			return metrics, fmt.Errorf("write encoded record batch: short write")
		}
		metrics.RecordsWritten += batch.Records
		batchIndex++
		if plan.ProgressCallback != nil {
			if err := plan.ProgressCallback(ctx, EncodedRecordExportProgressEvent{
				BatchIndex: batchIndex, SourceOffset: batch.Offset, BatchRecords: batch.Records,
				RecordsRead: metrics.RecordsRead, RecordsWritten: metrics.RecordsWritten,
				BytesRead: metrics.BytesRead, BytesWritten: metrics.BytesWritten,
			}); err != nil {
				return metrics, fmt.Errorf("update encoded record export progress: %w", err)
			}
		}
	}
	if err := output.Close(); err != nil {
		return metrics, fmt.Errorf("close encoded record export target: %w", err)
	}
	closed = true
	if plan.ProgressCallback != nil {
		if err := plan.ProgressCallback(ctx, EncodedRecordExportProgressEvent{
			BatchIndex: batchIndex, RecordsRead: metrics.RecordsRead, RecordsWritten: metrics.RecordsWritten,
			BytesRead: metrics.BytesRead, BytesWritten: metrics.BytesWritten, Final: true,
		}); err != nil {
			return metrics, fmt.Errorf("update encoded record export final progress: %w", err)
		}
	}
	succeeded = true
	return metrics, nil
}
