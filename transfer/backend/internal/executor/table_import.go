package executor

import (
	"context"
	"fmt"
	"sort"

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
	Reader         engineplugin.ContentReadableProvider
	Preparer       engineplugin.TableWritePreparer
	InfoProvider   format.TableInfoProvider
	Writer         engineplugin.BatchWritableProvider
	FormatProvider format.TableSampleReader
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

	formatProvider, err := format.GetTableSampleProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("get table sample provider %q: %w", formatType, err)
	}
	infoProvider, _ := format.GetTableInfoProvider(formatType)

	return &TableImportExecutor{
		Reader:         reader,
		Preparer:       preparer,
		InfoProvider:   infoProvider,
		Writer:         writer,
		FormatProvider: formatProvider,
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
		return nil, fmt.Errorf("table import format %q does not match table sample provider format %q", plan.Format, e.FormatProvider.Format())
	}
	if err := e.prepareTargetTable(ctx, plan); err != nil {
		return nil, err
	}

	batchSize := plan.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	metrics := &TableImportMetrics{}
	offset := int64(0)
	for {
		input, err := e.Reader.OpenContent(ctx, plan.SourceConnInfo, plan.SourcePath, plan.SourceRead)
		if err != nil {
			return metrics, fmt.Errorf("open source content at offset %d: %w", offset, err)
		}
		rows, sampleErr := e.FormatProvider.SampleTable(ctx, input, offset, int64(batchSize), plan.ParseOptions)
		closeErr := input.Close()
		if sampleErr != nil {
			return metrics, fmt.Errorf("sample source table at offset %d: %w", offset, sampleErr)
		}
		if closeErr != nil {
			return metrics, fmt.Errorf("close source content at offset %d: %w", offset, closeErr)
		}
		if len(rows) == 0 {
			break
		}

		batch := &engineplugin.BatchData{
			Rows:   rows,
			Fields: fieldsFromRows(rows),
			Offset: offset,
		}
		if err := e.Writer.WriteBatch(ctx, plan.TargetConnInfo, plan.TargetPath, batch, plan.TargetWrite); err != nil {
			return metrics, fmt.Errorf("write target batch at offset %d: %w", offset, err)
		}

		rowCount := int64(len(rows))
		metrics.RecordsRead += rowCount
		metrics.RecordsWritten += rowCount
		metrics.Batches++
		offset += rowCount

		if len(rows) < batchSize {
			break
		}
	}
	return metrics, nil
}

func (e *TableImportExecutor) prepareTargetTable(ctx context.Context, plan TableImportPlan) error {
	mode := normalizeImportPrepareMode(plan.TargetPrepare.Mode)
	if mode == "" {
		return nil
	}
	if e.Preparer == nil {
		return fmt.Errorf("target engine does not implement table write prepare for mode %q", mode)
	}
	opts := engineplugin.TableWriteOptions{Mode: mode}
	if mode == "create_if_not_exists" {
		fields, err := e.describeSourceFields(ctx, plan)
		if err != nil {
			return err
		}
		opts.Fields = fields
	}
	if err := e.Preparer.PrepareTableWrite(ctx, plan.TargetConnInfo, plan.TargetPath, opts); err != nil {
		return fmt.Errorf("prepare target table write: %w", err)
	}
	return nil
}

func (e *TableImportExecutor) describeSourceFields(ctx context.Context, plan TableImportPlan) ([]engineplugin.FieldInfo, error) {
	if e.InfoProvider == nil {
		return nil, fmt.Errorf("table import format %q does not implement table info provider required for create_if_not_exists", plan.Format)
	}
	input, err := e.Reader.OpenContent(ctx, plan.SourceConnInfo, plan.SourcePath, plan.SourceRead)
	if err != nil {
		return nil, fmt.Errorf("open source content for table info: %w", err)
	}
	info, describeErr := e.InfoProvider.DescribeTable(ctx, input, plan.ParseOptions)
	closeErr := input.Close()
	if describeErr != nil {
		return nil, fmt.Errorf("describe source table: %w", describeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close source content after table info: %w", closeErr)
	}
	fields := tableInfoFields(info)
	if len(fields) == 0 {
		return nil, fmt.Errorf("source table info has no fields")
	}
	return fields, nil
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

func fieldsFromRows(rows []map[string]interface{}) []engineplugin.FieldInfo {
	if len(rows) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(rows[0]))
	for _, row := range rows {
		for name := range row {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	sort.Strings(names)
	fields := make([]engineplugin.FieldInfo, 0, len(names))
	for _, name := range names {
		fields = append(fields, engineplugin.FieldInfo{Name: name})
	}
	return fields
}

func normalizeImportPrepareMode(mode string) string {
	switch mode {
	case "truncate_insert", "create_if_not_exists":
		return mode
	default:
		return ""
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
