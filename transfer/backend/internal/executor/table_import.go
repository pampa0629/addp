package executor

import (
	"context"
	"fmt"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

type TableImportPlan struct {
	SourceConnInfo engineplugin.ConnectionInfo
	SourcePath     engineplugin.CatalogPath
	SourceRead     engineplugin.ReadOptions

	TargetConnInfo engineplugin.ConnectionInfo
	TargetPath     engineplugin.CatalogPath
	TargetPrepare  engineplugin.TableWriteOptions
	TargetWrite    engineplugin.BatchWriteOptions

	Format       format.FormatType
	BatchSize    int
	ParseOptions *format.ParseOptions
}

type TableImportMetrics struct {
	RecordsRead    int64
	RecordsWritten int64
	Batches        int64
}

type TableImportExecutor struct {
	Reader               engineplugin.ContentReadableProvider
	Preparer             engineplugin.TableWritePreparer
	InfoProvider         format.TableInfoProvider
	Writer               engineplugin.BatchWritableProvider
	TableSessionProvider engineplugin.TableWriteSessionProvider
	FormatProvider       format.TableSampleReader
	TableReadProvider    format.TableReaderProvider
	ComponentProvider    componentTableSourceProvider
}

func NewTableImportExecutor(sourceEngineType, targetEngineType string, formatType format.FormatType) (*TableImportExecutor, error) {
	sourcePlugin, err := engineplugin.Get(sourceEngineType)
	if err != nil {
		return nil, fmt.Errorf("get source engine plugin %q: %w", sourceEngineType, err)
	}
	reader, ok := sourcePlugin.(engineplugin.ContentReadableProvider)
	if !ok {
		return nil, fmt.Errorf("source engine %q does not implement content read", sourceEngineType)
	}

	targetPlugin, err := engineplugin.Get(targetEngineType)
	if err != nil {
		return nil, fmt.Errorf("get target engine plugin %q: %w", targetEngineType, err)
	}
	writer, ok := targetPlugin.(engineplugin.BatchWritableProvider)
	if !ok {
		return nil, fmt.Errorf("target engine %q does not implement batch table write", targetEngineType)
	}
	preparer, _ := targetPlugin.(engineplugin.TableWritePreparer)
	tableSessionProvider, _ := targetPlugin.(engineplugin.TableWriteSessionProvider)

	formatProvider, err := format.GetTableSampleProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("get table sample provider %q: %w", formatType, err)
	}
	infoProvider, _ := format.GetTableInfoProvider(formatType)
	tableReadProvider, _ := format.GetTableReaderProvider(formatType)
	componentReader, _ := format.GetComponentTableProvider(formatType)
	componentProvider, _ := componentReader.(componentTableSourceProvider)

	return &TableImportExecutor{
		Reader:               reader,
		Preparer:             preparer,
		InfoProvider:         infoProvider,
		Writer:               writer,
		TableSessionProvider: tableSessionProvider,
		FormatProvider:       formatProvider,
		TableReadProvider:    tableReadProvider,
		ComponentProvider:    componentProvider,
	}, nil
}

func (e *TableImportExecutor) Execute(ctx context.Context, plan TableImportPlan) (*TableImportMetrics, error) {
	if err := validateTableImportExecutor(e); err != nil {
		return nil, err
	}
	if err := validateTableImportPlan(plan); err != nil {
		return nil, err
	}
	if plan.Format != "" && plan.Format != e.FormatProvider.Format() {
		return nil, fmt.Errorf("table import format %q does not match table reader format %q", plan.Format, e.FormatProvider.Format())
	}
	if normalizeImportPrepareMode(plan.TargetPrepare.Mode) != "" && e.Preparer == nil {
		return nil, fmt.Errorf("target engine does not implement table write prepare for mode %q", normalizeImportPrepareMode(plan.TargetPrepare.Mode))
	}

	var infoProvider format.TableInfoProvider
	if normalizeImportPrepareMode(plan.TargetPrepare.Mode) != "" {
		infoProvider = e.InfoProvider
	}

	metrics, err := (&TablePipeline{
		Source: &encodedContentTableSource{
			reader:            e.Reader,
			tableProvider:     e.TableReadProvider,
			componentProvider: e.ComponentProvider,
			sampleProvider:    e.FormatProvider,
			infoProvider:      infoProvider,
			connInfo:          plan.SourceConnInfo,
			path:              plan.SourcePath,
			readOptions:       plan.SourceRead,
			parseOptions:      plan.ParseOptions,
		},
		Target: &nativeTableBatchTarget{
			preparer:             e.Preparer,
			writer:               e.Writer,
			tableSessionProvider: e.TableSessionProvider,
			connInfo:             plan.TargetConnInfo,
			path:                 plan.TargetPath,
			prepareOptions:       plan.TargetPrepare,
			writeOptions:         plan.TargetWrite,
		},
		BatchSize: plan.BatchSize,
	}).Execute(ctx)
	return tableImportMetrics(metrics), err
}

func isCopyWriteMethod(method string) bool {
	switch method {
	case "copy", "postgres_copy":
		return true
	default:
		return false
	}
}

func validateTableImportExecutor(e *TableImportExecutor) error {
	if e == nil {
		return fmt.Errorf("table import executor cannot be nil")
	}
	if e.Reader == nil {
		return fmt.Errorf("table import executor requires source content reader")
	}
	if e.Writer == nil {
		return fmt.Errorf("table import executor requires target batch writer")
	}
	if e.FormatProvider == nil {
		return fmt.Errorf("table import executor requires table sample provider")
	}
	if e.FormatProvider.Format() == "" || e.FormatProvider.Format() == format.FormatUnknown {
		return fmt.Errorf("table import executor requires concrete table sample format")
	}
	return nil
}

func validateTableImportPlan(plan TableImportPlan) error {
	if plan.BatchSize < 0 {
		return fmt.Errorf("table import batch size cannot be negative")
	}
	if plan.Format != "" && plan.Format == format.FormatUnknown {
		return fmt.Errorf("table import format cannot be unknown")
	}
	return nil
}

func normalizeImportPrepareMode(mode string) string {
	switch mode {
	case "append", "create_if_not_exists":
		return "append"
	case "overwrite", "truncate_insert":
		return "overwrite"
	case "":
		return ""
	default:
		return mode
	}
}

func tableImportMetrics(metrics *TablePipelineMetrics) *TableImportMetrics {
	if metrics == nil {
		return nil
	}
	return &TableImportMetrics{
		RecordsRead:    metrics.RecordsRead,
		RecordsWritten: metrics.RecordsWritten,
		Batches:        metrics.Batches,
	}
}

func tableInfoFields(info *format.TableInfo) []engineplugin.FieldInfo {
	if info == nil {
		return nil
	}
	fields := make([]engineplugin.FieldInfo, 0, len(info.Fields))
	for _, field := range info.Fields {
		if field.Name == "" {
			continue
		}
		fields = append(fields, engineplugin.FieldInfo{
			Name:       field.Name,
			Type:       string(field.Type),
			NativeType: field.OriginalType,
			Nullable:   field.Nullable,
			PrimaryKey: field.IsPrimaryKey,
			Comment:    field.Comment,
		})
	}
	return fields
}
