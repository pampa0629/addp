package executor

import (
	"context"
	"fmt"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

type TableEndpointKind string

const (
	TableEndpointNative  TableEndpointKind = "native"
	TableEndpointEncoded TableEndpointKind = "encoded"
)

type TableSourcePlan struct {
	Kind         TableEndpointKind
	ConnInfo     engineplugin.ConnectionInfo
	Path         engineplugin.CatalogPath
	Query        string
	ReadOptions  map[string]interface{}
	ContentRead  engineplugin.ReadOptions
	Format       format.FormatType
	ParseOptions *format.ParseOptions
}

type TableTargetPlan struct {
	Kind          TableEndpointKind
	ConnInfo      engineplugin.ConnectionInfo
	Path          engineplugin.CatalogPath
	ContentWrite  engineplugin.WriteOptions
	TablePrepare  engineplugin.TableWriteOptions
	TableWrite    engineplugin.BatchWriteOptions
	Format        format.FormatType
	FormatOptions *format.WriteOptions
}

type TableTransferPlan struct {
	Source    TableSourcePlan
	Target    TableTargetPlan
	BatchSize int
}

type TableTransferExecutor struct {
	SourceNativeReader         engineplugin.BatchReadableProvider
	SourceTableSessionProvider engineplugin.TableReadSessionProvider
	SourceContentReader        engineplugin.ContentReadableProvider
	SourceFormatProvider       format.TableSampleReader
	SourceTableReadProvider    format.TableReaderProvider
	SourceInfoProvider         format.TableInfoProvider
	SourceComponentProvider    componentTableSourceProvider
	TargetContentWriter        engineplugin.ContentWritableProvider
	TargetFormatProvider       format.TableWriterProvider
	TargetComponentProvider    format.ComponentTableWriterProvider
	TargetNativePreparer       engineplugin.TableWritePreparer
	TargetNativeWriter         engineplugin.BatchWritableProvider
	TargetTableSessionProvider engineplugin.TableWriteSessionProvider
}

func NewTableTransferExecutor(sourceEngineType, targetEngineType string, sourceFormat, targetFormat format.FormatType) (*TableTransferExecutor, error) {
	sourcePlugin, err := engineplugin.Get(sourceEngineType)
	if err != nil {
		return nil, fmt.Errorf("get source engine plugin %q: %w", sourceEngineType, err)
	}
	targetPlugin, err := engineplugin.Get(targetEngineType)
	if err != nil {
		return nil, fmt.Errorf("get target engine plugin %q: %w", targetEngineType, err)
	}

	executor := &TableTransferExecutor{}
	if reader, ok := sourcePlugin.(engineplugin.BatchReadableProvider); ok {
		executor.SourceNativeReader = reader
	}
	executor.SourceTableSessionProvider, _ = sourcePlugin.(engineplugin.TableReadSessionProvider)
	if reader, ok := sourcePlugin.(engineplugin.ContentReadableProvider); ok {
		executor.SourceContentReader = reader
	}
	if sourceFormat != "" {
		executor.SourceFormatProvider, _ = format.GetTableSampleProvider(sourceFormat)
		executor.SourceInfoProvider, _ = format.GetTableInfoProvider(sourceFormat)
		executor.SourceTableReadProvider, _ = format.GetTableReaderProvider(sourceFormat)
		if componentReader, err := format.GetComponentTableProvider(sourceFormat); err == nil {
			executor.SourceComponentProvider, _ = componentReader.(componentTableSourceProvider)
		}
		if executor.SourceTableReadProvider != nil {
			executor.SourceComponentProvider = nil
		}
	}

	if writer, ok := targetPlugin.(engineplugin.ContentWritableProvider); ok {
		executor.TargetContentWriter = writer
	}
	if targetFormat != "" {
		executor.TargetFormatProvider, _ = format.GetTableWriterProvider(targetFormat)
		executor.TargetComponentProvider, _ = format.GetComponentTableWriterProvider(targetFormat)
		if executor.TargetFormatProvider != nil {
			executor.TargetComponentProvider = nil
		}
	}
	executor.TargetNativePreparer, _ = targetPlugin.(engineplugin.TableWritePreparer)
	if writer, ok := targetPlugin.(engineplugin.BatchWritableProvider); ok {
		executor.TargetNativeWriter = writer
	}
	executor.TargetTableSessionProvider, _ = targetPlugin.(engineplugin.TableWriteSessionProvider)

	return executor, nil
}

func (e *TableTransferExecutor) Execute(ctx context.Context, plan TableTransferPlan) (*TablePipelineMetrics, error) {
	if e == nil {
		return nil, fmt.Errorf("table transfer executor cannot be nil")
	}
	source, err := e.openSource(plan.Source)
	if err != nil {
		return nil, err
	}
	target, err := e.openTarget(plan.Target)
	if err != nil {
		return nil, err
	}
	return (&TablePipeline{
		Source:    source,
		Target:    target,
		BatchSize: plan.BatchSize,
	}).Execute(ctx)
}

func (e *TableTransferExecutor) openSource(plan TableSourcePlan) (TableBatchSource, error) {
	switch plan.Kind {
	case TableEndpointNative:
		if e.SourceNativeReader == nil {
			return nil, fmt.Errorf("native table source requires batch reader")
		}
		return &nativeTableBatchSource{
			reader:               e.SourceNativeReader,
			tableSessionProvider: e.SourceTableSessionProvider,
			connInfo:             plan.ConnInfo,
			path:                 plan.Path,
			query:                plan.Query,
			readOptions:          plan.ReadOptions,
		}, nil
	case TableEndpointEncoded:
		if e.SourceContentReader == nil {
			return nil, fmt.Errorf("encoded table source requires content reader")
		}
		if e.SourceTableReadProvider == nil && e.SourceComponentProvider == nil && e.SourceFormatProvider == nil {
			return nil, fmt.Errorf("encoded table source requires table reader provider")
		}
		return &encodedContentTableSource{
			reader:            e.SourceContentReader,
			tableProvider:     e.SourceTableReadProvider,
			componentProvider: e.SourceComponentProvider,
			sampleProvider:    e.SourceFormatProvider,
			infoProvider:      e.SourceInfoProvider,
			connInfo:          plan.ConnInfo,
			path:              plan.Path,
			readOptions:       plan.ContentRead,
			parseOptions:      plan.ParseOptions,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported table source kind %q", plan.Kind)
	}
}

func (e *TableTransferExecutor) openTarget(plan TableTargetPlan) (TableBatchTarget, error) {
	switch plan.Kind {
	case TableEndpointEncoded:
		if e.TargetContentWriter == nil {
			return nil, fmt.Errorf("encoded table target requires content writer")
		}
		if e.TargetFormatProvider == nil && e.TargetComponentProvider == nil {
			return nil, fmt.Errorf("encoded table target requires table writer provider")
		}
		return &encodedContentTableTarget{
			writer:            e.TargetContentWriter,
			formatProvider:    e.TargetFormatProvider,
			componentProvider: e.TargetComponentProvider,
			connInfo:          plan.ConnInfo,
			path:              plan.Path,
			writeOptions:      plan.ContentWrite,
			formatOptions:     plan.FormatOptions,
		}, nil
	case TableEndpointNative:
		if e.TargetNativeWriter == nil {
			return nil, fmt.Errorf("native table target requires batch writer")
		}
		if normalizeImportPrepareMode(plan.TablePrepare.Mode) != "" && e.TargetNativePreparer == nil {
			return nil, fmt.Errorf("target engine does not implement table write prepare for mode %q", normalizeImportPrepareMode(plan.TablePrepare.Mode))
		}
		return &nativeTableBatchTarget{
			preparer:             e.TargetNativePreparer,
			writer:               e.TargetNativeWriter,
			tableSessionProvider: e.TargetTableSessionProvider,
			connInfo:             plan.ConnInfo,
			path:                 plan.Path,
			prepareOptions:       plan.TablePrepare,
			writeOptions:         plan.TableWrite,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported table target kind %q", plan.Kind)
	}
}
