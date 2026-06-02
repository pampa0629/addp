package executor

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

type RawCopyPlan struct {
	Source           RawCopyEndpointPlan
	Target           RawCopyEndpointPlan
	DataType         string
	Format           format.FormatType
	ProgressCallback RawCopyProgressCallback
}

type RawCopyEndpointPlan struct {
	ConnInfo          engineplugin.ConnectionInfo
	Path              engineplugin.CatalogPath
	ContentRead       engineplugin.ReadOptions
	ContentWrite      engineplugin.WriteOptions
	DeleteBeforeWrite bool
}

type RawCopyProgressCallback func(context.Context, RawCopyProgressEvent) error

type RawCopyProgressEvent struct {
	BytesRead      int64
	BytesWritten   int64
	RecordsRead    int64
	RecordsWritten int64
	Final          bool
}

type RawCopyMetrics struct {
	BytesRead      int64
	BytesWritten   int64
	RecordsRead    int64
	RecordsWritten int64
}

type RawCopyExecutor struct {
	SourceContentReader  engineplugin.ContentReadableProvider
	TargetContentWriter  engineplugin.ContentWritableProvider
	TargetDeleteProvider engineplugin.ResourceDeleteProvider
}

func NewRawCopyExecutor(sourceEngineType, targetEngineType string) (*RawCopyExecutor, error) {
	sourcePlugin, err := engineplugin.Get(sourceEngineType)
	if err != nil {
		return nil, fmt.Errorf("get source engine plugin %q: %w", sourceEngineType, err)
	}
	targetPlugin, err := engineplugin.Get(targetEngineType)
	if err != nil {
		return nil, fmt.Errorf("get target engine plugin %q: %w", targetEngineType, err)
	}
	exec := &RawCopyExecutor{}
	exec.SourceContentReader, _ = sourcePlugin.(engineplugin.ContentReadableProvider)
	exec.TargetContentWriter, _ = targetPlugin.(engineplugin.ContentWritableProvider)
	exec.TargetDeleteProvider, _ = targetPlugin.(engineplugin.ResourceDeleteProvider)
	return exec, nil
}

func (e *RawCopyExecutor) Execute(ctx context.Context, plan RawCopyPlan) (*RawCopyMetrics, error) {
	if e == nil {
		return nil, fmt.Errorf("raw copy executor cannot be nil")
	}
	if e.SourceContentReader == nil {
		return nil, fmt.Errorf("raw copy source requires content reader")
	}
	if e.TargetContentWriter == nil {
		return nil, fmt.Errorf("raw copy target requires content writer")
	}
	if plan.Target.DeleteBeforeWrite {
		if e.TargetDeleteProvider == nil {
			return nil, fmt.Errorf("raw copy overwrite requires target engine resource delete")
		}
		if err := e.TargetDeleteProvider.DeleteResource(ctx, plan.Target.ConnInfo, plan.Target.Path); err != nil {
			return nil, fmt.Errorf("delete raw copy target before write: %w", err)
		}
	}

	source := contentadapter.NewMappedReader(e.SourceContentReader, plan.Source.ConnInfo, contentadapter.FixedPathMapper(plan.Source.Path), plan.Source.ContentRead)
	target := contentadapter.NewMappedWriter(e.TargetContentWriter, plan.Target.ConnInfo, contentadapter.FixedPathMapper(plan.Target.Path), plan.Target.ContentWrite)
	ref := contentRefFromCatalogPath(plan.Source.Path)
	input, err := source.Open(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("open raw copy source content: %w", err)
	}
	defer input.Close()

	output, err := target.Create(ctx, contentRefFromCatalogPath(plan.Target.Path))
	if err != nil {
		return nil, fmt.Errorf("create raw copy target content: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
	}()

	counter := &countingReader{reader: input}
	written, err := io.Copy(output, counter)
	if err != nil {
		return &RawCopyMetrics{BytesRead: counter.n, BytesWritten: written}, fmt.Errorf("copy raw content: %w", err)
	}
	if err := output.Close(); err != nil {
		return &RawCopyMetrics{BytesRead: counter.n, BytesWritten: written}, fmt.Errorf("close raw copy target content: %w", err)
	}
	closed = true

	metrics := &RawCopyMetrics{
		BytesRead:      counter.n,
		BytesWritten:   written,
		RecordsRead:    1,
		RecordsWritten: 1,
	}
	if plan.ProgressCallback != nil {
		if err := plan.ProgressCallback(ctx, RawCopyProgressEvent{
			BytesRead:      metrics.BytesRead,
			BytesWritten:   metrics.BytesWritten,
			RecordsRead:    metrics.RecordsRead,
			RecordsWritten: metrics.RecordsWritten,
			Final:          true,
		}); err != nil {
			return metrics, fmt.Errorf("update raw copy progress: %w", err)
		}
	}
	return metrics, nil
}

type countingReader struct {
	reader io.Reader
	n      int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.n += int64(n)
	return n, err
}
