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
	Reader               engineplugin.ContentReadableProvider
	Preparer             engineplugin.TableWritePreparer
	InfoProvider         format.TableInfoProvider
	Writer               engineplugin.BatchWritableProvider
	TableSessionProvider engineplugin.TableWriteSessionProvider
	FormatProvider       format.TableSampleReader
	TableReadProvider    format.TableReaderProvider
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

	return &TableImportExecutor{
		Reader:               reader,
		Preparer:             preparer,
		InfoProvider:         infoProvider,
		Writer:               writer,
		TableSessionProvider: tableSessionProvider,
		FormatProvider:       formatProvider,
		TableReadProvider:    tableReadProvider,
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
	preparedFields, err := e.prepareTargetTable(ctx, plan)
	if err != nil {
		return nil, err
	}
	if len(preparedFields) > 0 {
		plan.TargetPrepare.Fields = preparedFields
	}

	batchSize := plan.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	batchWriter := e.importBatchWriter(ctx, plan)
	if e.TableReadProvider != nil {
		return e.executeWithTableReader(ctx, plan, batchSize, batchWriter)
	}
	return e.executeWithSampleReader(ctx, plan, batchSize, batchWriter)
}

func (e *TableImportExecutor) executeWithTableReader(ctx context.Context, plan TableImportPlan, batchSize int, batchWriter importBatchWriter) (*TableImportMetrics, error) {
	metrics := &TableImportMetrics{}
	input, err := e.Reader.OpenContent(ctx, plan.SourceConnInfo, plan.SourcePath, plan.SourceRead)
	if err != nil {
		return metrics, fmt.Errorf("open source content: %w", err)
	}
	defer input.Close()

	tableReader, err := e.TableReadProvider.OpenTableReader(ctx, input, plan.ParseOptions)
	if err != nil {
		return metrics, fmt.Errorf("open table reader: %w", err)
	}
	defer tableReader.Close(ctx)

	offset := int64(0)
	for {
		rows, err := tableReader.ReadRows(ctx, batchSize)
		if err != nil {
			return metrics, fmt.Errorf("read source table rows at offset %d: %w", offset, err)
		}
		if len(rows) == 0 {
			break
		}
		if err := batchWriter.write(ctx, e, plan, metrics, rows, offset); err != nil {
			return metrics, err
		}
		offset += int64(len(rows))
	}
	if err := batchWriter.close(ctx); err != nil {
		return metrics, err
	}
	return metrics, nil
}

func (e *TableImportExecutor) executeWithSampleReader(ctx context.Context, plan TableImportPlan, batchSize int, batchWriter importBatchWriter) (*TableImportMetrics, error) {
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
		if err := batchWriter.write(ctx, e, plan, metrics, rows, offset); err != nil {
			return metrics, err
		}
		offset += int64(len(rows))

		if len(rows) < batchSize {
			break
		}
	}
	if err := batchWriter.close(ctx); err != nil {
		return metrics, err
	}
	return metrics, nil
}

type importBatchWriter interface {
	write(ctx context.Context, e *TableImportExecutor, plan TableImportPlan, metrics *TableImportMetrics, rows []map[string]interface{}, offset int64) error
	close(ctx context.Context) error
}

func (e *TableImportExecutor) importBatchWriter(ctx context.Context, plan TableImportPlan) importBatchWriter {
	if e.TableSessionProvider == nil || !isCopyWriteMethod(plan.TargetWrite.Method) {
		return directImportBatchWriter{}
	}
	return &sessionImportBatchWriter{}
}

type directImportBatchWriter struct{}

func (w directImportBatchWriter) write(ctx context.Context, e *TableImportExecutor, plan TableImportPlan, metrics *TableImportMetrics, rows []map[string]interface{}, offset int64) error {
	return e.writeImportBatch(ctx, plan, metrics, rows, offset)
}

func (w directImportBatchWriter) close(context.Context) error {
	return nil
}

type sessionImportBatchWriter struct {
	session engineplugin.TableWriteSession
	fields  []engineplugin.FieldInfo
}

func (w *sessionImportBatchWriter) write(ctx context.Context, e *TableImportExecutor, plan TableImportPlan, metrics *TableImportMetrics, rows []map[string]interface{}, offset int64) error {
	if w.session == nil {
		fields := importSessionFields(plan, rows)
		session, err := e.TableSessionProvider.OpenTableWriteSession(ctx, plan.TargetConnInfo, plan.TargetPath, engineplugin.TableWriteSessionOptions{
			Mode:   plan.TargetWrite.Mode,
			Method: plan.TargetWrite.Method,
			Fields: fields,
		})
		if err != nil {
			return fmt.Errorf("open target table write session: %w", err)
		}
		w.session = session
		w.fields = fields
	}
	batch := &engineplugin.BatchData{
		Rows:   rows,
		Fields: w.fields,
		Offset: offset,
	}
	if err := w.session.WriteBatch(ctx, batch); err != nil {
		_ = w.session.Abort(ctx)
		return fmt.Errorf("write target table session batch at offset %d: %w", offset, err)
	}
	rowCount := int64(len(rows))
	metrics.RecordsRead += rowCount
	metrics.RecordsWritten += rowCount
	metrics.Batches++
	return nil
}

func (w *sessionImportBatchWriter) close(ctx context.Context) error {
	if w.session == nil {
		return nil
	}
	if err := w.session.Close(ctx); err != nil {
		return fmt.Errorf("close target table write session: %w", err)
	}
	return nil
}

func (e *TableImportExecutor) writeImportBatch(ctx context.Context, plan TableImportPlan, metrics *TableImportMetrics, rows []map[string]interface{}, offset int64) error {
	batch := &engineplugin.BatchData{
		Rows:   rows,
		Fields: fieldsFromRows(rows),
		Offset: offset,
	}
	if err := e.Writer.WriteBatch(ctx, plan.TargetConnInfo, plan.TargetPath, batch, plan.TargetWrite); err != nil {
		return fmt.Errorf("write target batch at offset %d: %w", offset, err)
	}

	rowCount := int64(len(rows))
	metrics.RecordsRead += rowCount
	metrics.RecordsWritten += rowCount
	metrics.Batches++
	return nil
}

func importSessionFields(plan TableImportPlan, rows []map[string]interface{}) []engineplugin.FieldInfo {
	if len(plan.TargetPrepare.Fields) > 0 {
		return append([]engineplugin.FieldInfo(nil), plan.TargetPrepare.Fields...)
	}
	return fieldsFromRows(rows)
}

func isCopyWriteMethod(method string) bool {
	switch method {
	case "copy", "postgres_copy":
		return true
	default:
		return false
	}
}

func (e *TableImportExecutor) prepareTargetTable(ctx context.Context, plan TableImportPlan) ([]engineplugin.FieldInfo, error) {
	mode := normalizeImportPrepareMode(plan.TargetPrepare.Mode)
	if mode == "" {
		return nil, nil
	}
	if e.Preparer == nil {
		return nil, fmt.Errorf("target engine does not implement table write prepare for mode %q", mode)
	}
	opts := engineplugin.TableWriteOptions{Mode: mode}
	if mode == "create_if_not_exists" {
		fields, err := e.describeSourceFields(ctx, plan)
		if err != nil {
			return nil, err
		}
		opts.Fields = fields
	}
	if err := e.Preparer.PrepareTableWrite(ctx, plan.TargetConnInfo, plan.TargetPath, opts); err != nil {
		return nil, fmt.Errorf("prepare target table write: %w", err)
	}
	return opts.Fields, nil
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
