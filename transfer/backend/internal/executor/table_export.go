package executor

import (
	"context"
	"fmt"
	"sort"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

const defaultBatchSize = 1000

type TableExportPlan struct {
	SourceConnInfo engineplugin.ConnectionInfo
	SourcePath     engineplugin.CatalogPath
	SourceQuery    string

	TargetConnInfo engineplugin.ConnectionInfo
	TargetPath     engineplugin.CatalogPath
	TargetWrite    engineplugin.WriteOptions

	Format       format.FormatType
	BatchSize    int
	WriteOptions *format.WriteOptions
}

type TableExportMetrics struct {
	RecordsRead    int64
	RecordsWritten int64
	Batches        int64
}

type TableExportExecutor struct {
	Reader         engineplugin.BatchReadableProvider
	Writer         engineplugin.ContentWritableProvider
	FormatProvider format.TableWriterProvider
}

func NewTableExportExecutor(sourceEngineType, targetEngineType string, formatType format.FormatType) (*TableExportExecutor, error) {
	sourcePlugin, err := engineplugin.Get(sourceEngineType)
	if err != nil {
		return nil, fmt.Errorf("get source engine plugin %q: %w", sourceEngineType, err)
	}
	reader, ok := sourcePlugin.(engineplugin.BatchReadableProvider)
	if !ok {
		return nil, fmt.Errorf("source engine %q does not implement batch table read", sourceEngineType)
	}

	targetPlugin, err := engineplugin.Get(targetEngineType)
	if err != nil {
		return nil, fmt.Errorf("get target engine plugin %q: %w", targetEngineType, err)
	}
	writer, ok := targetPlugin.(engineplugin.ContentWritableProvider)
	if !ok {
		return nil, fmt.Errorf("target engine %q does not implement content write", targetEngineType)
	}

	formatProvider, err := format.GetTableWriterProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("get table writer provider %q: %w", formatType, err)
	}

	return &TableExportExecutor{
		Reader:         reader,
		Writer:         writer,
		FormatProvider: formatProvider,
	}, nil
}

func (e *TableExportExecutor) Execute(ctx context.Context, plan TableExportPlan) (*TableExportMetrics, error) {
	if err := validateTableExportExecutor(e); err != nil {
		return nil, err
	}
	if err := validateTableExportPlan(plan); err != nil {
		return nil, err
	}
	if plan.Format != "" && plan.Format != e.FormatProvider.Format() {
		return nil, fmt.Errorf("table export format %q does not match table writer provider format %q", plan.Format, e.FormatProvider.Format())
	}

	output, err := e.Writer.CreateContent(ctx, plan.TargetConnInfo, plan.TargetPath, plan.TargetWrite)
	if err != nil {
		return nil, fmt.Errorf("create target content: %w", err)
	}
	outputClosed := false
	defer func() {
		if !outputClosed {
			_ = output.Close()
		}
	}()

	batchSize := plan.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	var tableWriter format.TableWriter
	metrics := &TableExportMetrics{}
	offset := int64(0)

	for {
		batch, err := e.Reader.ReadBatch(ctx, plan.SourceConnInfo, plan.SourcePath, engineplugin.BatchReadOptions{
			Limit:  batchSize,
			Offset: offset,
			Query:  plan.SourceQuery,
		})
		if err != nil {
			return metrics, fmt.Errorf("read source batch at offset %d: %w", offset, err)
		}
		if batch == nil || len(batch.Rows) == 0 {
			break
		}

		if tableWriter == nil {
			schema := tableInfoFromBatch(batch)
			opened, err := e.FormatProvider.OpenTableWriter(ctx, output, schema, plan.WriteOptions)
			if err != nil {
				return metrics, fmt.Errorf("open table writer: %w", err)
			}
			tableWriter = opened
		}

		if err := tableWriter.WriteRows(ctx, batch.Rows); err != nil {
			return metrics, fmt.Errorf("write table rows at offset %d: %w", offset, err)
		}
		rowCount := int64(len(batch.Rows))
		metrics.RecordsRead += rowCount
		metrics.RecordsWritten += rowCount
		metrics.Batches++
		offset += rowCount

		if len(batch.Rows) < batchSize {
			break
		}
	}

	if tableWriter != nil {
		if err := tableWriter.Close(ctx); err != nil {
			return metrics, fmt.Errorf("close table writer: %w", err)
		}
	}
	if err := output.Close(); err != nil {
		return metrics, fmt.Errorf("close target content: %w", err)
	}
	outputClosed = true

	return metrics, nil
}

func validateTableExportExecutor(e *TableExportExecutor) error {
	if e == nil {
		return fmt.Errorf("table export executor cannot be nil")
	}
	if e.Reader == nil {
		return fmt.Errorf("table export executor requires source batch reader")
	}
	if e.Writer == nil {
		return fmt.Errorf("table export executor requires target content writer")
	}
	if e.FormatProvider == nil {
		return fmt.Errorf("table export executor requires table writer provider")
	}
	if e.FormatProvider.Format() == "" || e.FormatProvider.Format() == format.FormatUnknown {
		return fmt.Errorf("table export executor requires concrete table writer format")
	}
	return nil
}

func validateTableExportPlan(plan TableExportPlan) error {
	if plan.BatchSize < 0 {
		return fmt.Errorf("table export batch size cannot be negative")
	}
	if plan.Format != "" && plan.Format == format.FormatUnknown {
		return fmt.Errorf("table export format cannot be unknown")
	}
	return nil
}

func tableInfoFromBatch(batch *engineplugin.BatchData) *format.TableInfo {
	info := &format.TableInfo{}
	if batch == nil {
		return info
	}
	info.Fields = make([]format.FieldInfo, 0, len(batch.Fields))
	for _, field := range batch.Fields {
		name := field.Name
		if name == "" {
			continue
		}
		info.Fields = append(info.Fields, format.FieldInfo{
			Name:         name,
			Type:         format.FieldType(field.Type),
			OriginalType: field.NativeType,
			Nullable:     field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		})
	}
	if len(info.Fields) == 0 && len(batch.Rows) > 0 {
		names := make([]string, 0, len(batch.Rows[0]))
		for name := range batch.Rows[0] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			info.Fields = append(info.Fields, format.FieldInfo{Name: name, Type: format.FieldTypeUnknown})
		}
	}
	return info
}
