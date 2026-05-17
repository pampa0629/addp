package executor

import (
	"context"
	"fmt"
	"io"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

type TablePipelineMetrics struct {
	RecordsRead    int64
	RecordsWritten int64
	Batches        int64
}

type TableBatchSource interface {
	Open(ctx context.Context) (TableBatchReader, error)
}

type TableBatchReader interface {
	Schema() *format.TableInfo
	ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error)
	Close(ctx context.Context) error
}

type TableBatchTarget interface {
	Open(ctx context.Context, schema *format.TableInfo) (TableBatchWriter, error)
}

type TableBatchWriter interface {
	WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error
	Close(ctx context.Context) error
	Abort(ctx context.Context) error
}

type componentTableSourceProvider interface {
	format.ComponentTableProvider
	format.ComponentSpecProvider
}

type TablePipeline struct {
	Source    TableBatchSource
	Target    TableBatchTarget
	BatchSize int
}

func (p *TablePipeline) Execute(ctx context.Context) (*TablePipelineMetrics, error) {
	if p == nil {
		return nil, fmt.Errorf("table pipeline cannot be nil")
	}
	if p.Source == nil {
		return nil, fmt.Errorf("table pipeline requires source")
	}
	if p.Target == nil {
		return nil, fmt.Errorf("table pipeline requires target")
	}
	batchSize := p.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	reader, err := p.Source.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("open table source: %w", err)
	}
	defer reader.Close(ctx)

	schema := reader.Schema()
	var firstBatch *engineplugin.BatchData
	if tableSchemaEmpty(schema) {
		firstBatch, err = reader.ReadBatch(ctx, batchSize)
		if err != nil {
			return nil, err
		}
		if firstBatch != nil && len(firstBatch.Rows) > 0 {
			schema = tableInfoFromBatch(firstBatch)
		}
	}
	if schema == nil {
		schema = &format.TableInfo{}
	}

	writer, err := p.Target.Open(ctx, schema)
	if err != nil {
		return nil, fmt.Errorf("open table target: %w", err)
	}
	writerClosed := false
	defer func() {
		if !writerClosed {
			_ = writer.Abort(context.Background())
		}
	}()

	metrics := &TablePipelineMetrics{}
	for {
		batch := firstBatch
		if firstBatch != nil {
			firstBatch = nil
		} else {
			var err error
			batch, err = reader.ReadBatch(ctx, batchSize)
			if err != nil {
				return metrics, err
			}
		}
		if batch == nil || len(batch.Rows) == 0 {
			break
		}
		if err := writer.WriteBatch(ctx, batch); err != nil {
			return metrics, err
		}
		rowCount := int64(len(batch.Rows))
		metrics.RecordsRead += rowCount
		metrics.RecordsWritten += rowCount
		metrics.Batches++
	}
	if err := writer.Close(ctx); err != nil {
		return metrics, err
	}
	writerClosed = true
	return metrics, nil
}

func tableSchemaEmpty(schema *format.TableInfo) bool {
	return schema == nil || len(schema.Fields) == 0
}

type EncodedTableTransferPlan struct {
	SourceConnInfo engineplugin.ConnectionInfo
	SourcePath     engineplugin.CatalogPath
	SourceRead     engineplugin.ReadOptions
	SourceFormat   format.FormatType
	ParseOptions   *format.ParseOptions

	TargetConnInfo engineplugin.ConnectionInfo
	TargetPath     engineplugin.CatalogPath
	TargetWrite    engineplugin.WriteOptions
	TargetFormat   format.FormatType
	WriteOptions   *format.WriteOptions

	BatchSize int
}

type EncodedTableTransferExecutor struct {
	Reader            engineplugin.ContentReadableProvider
	Writer            engineplugin.ContentWritableProvider
	TableReadProvider format.TableReaderProvider
	ComponentProvider componentTableSourceProvider
	FormatProvider    format.TableWriterProvider
	ComponentWriter   format.ComponentTableWriterProvider
}

func NewEncodedTableTransferExecutor(sourceEngineType, targetEngineType string, sourceFormat, targetFormat format.FormatType) (*EncodedTableTransferExecutor, error) {
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
	writer, ok := targetPlugin.(engineplugin.ContentWritableProvider)
	if !ok {
		return nil, fmt.Errorf("target engine %q does not implement content write", targetEngineType)
	}

	tableReadProvider, tableReaderErr := format.GetTableReaderProvider(sourceFormat)
	var sourceComponentProvider componentTableSourceProvider
	sourceComponentReader, componentReaderErr := format.GetComponentTableProvider(sourceFormat)
	if componentReaderErr == nil {
		var ok bool
		sourceComponentProvider, ok = sourceComponentReader.(componentTableSourceProvider)
		if !ok {
			componentReaderErr = fmt.Errorf("component table provider %q does not declare component specs", sourceFormat)
		}
	}
	if tableReaderErr != nil && componentReaderErr != nil {
		return nil, fmt.Errorf("get table reader provider %q: %w", sourceFormat, tableReaderErr)
	}
	if tableReadProvider != nil {
		sourceComponentProvider = nil
	}
	targetFormatProvider, tableWriterErr := format.GetTableWriterProvider(targetFormat)
	targetComponentWriter, componentWriterErr := format.GetComponentTableWriterProvider(targetFormat)
	if tableWriterErr != nil && componentWriterErr != nil {
		return nil, fmt.Errorf("get table writer provider %q: %w", targetFormat, tableWriterErr)
	}
	if targetFormatProvider != nil {
		targetComponentWriter = nil
	}

	return &EncodedTableTransferExecutor{
		Reader:            reader,
		Writer:            writer,
		TableReadProvider: tableReadProvider,
		ComponentProvider: sourceComponentProvider,
		FormatProvider:    targetFormatProvider,
		ComponentWriter:   targetComponentWriter,
	}, nil
}

func (e *EncodedTableTransferExecutor) Execute(ctx context.Context, plan EncodedTableTransferPlan) (*TablePipelineMetrics, error) {
	if e == nil {
		return nil, fmt.Errorf("encoded table transfer executor cannot be nil")
	}
	if e.Reader == nil || e.Writer == nil || (e.TableReadProvider == nil && e.ComponentProvider == nil) {
		return nil, fmt.Errorf("encoded table transfer executor is missing source or target provider")
	}
	if e.FormatProvider == nil && e.ComponentWriter == nil {
		return nil, fmt.Errorf("encoded table transfer executor requires table writer provider")
	}
	pipeline := &TablePipeline{
		Source: &encodedContentTableSource{
			reader:            e.Reader,
			tableProvider:     e.TableReadProvider,
			componentProvider: e.ComponentProvider,
			connInfo:          plan.SourceConnInfo,
			path:              plan.SourcePath,
			readOptions:       plan.SourceRead,
			parseOptions:      plan.ParseOptions,
		},
		Target: &encodedContentTableTarget{
			writer:            e.Writer,
			formatProvider:    e.FormatProvider,
			componentProvider: e.ComponentWriter,
			connInfo:          plan.TargetConnInfo,
			path:              plan.TargetPath,
			writeOptions:      plan.TargetWrite,
			formatOptions:     plan.WriteOptions,
		},
		BatchSize: plan.BatchSize,
	}
	return pipeline.Execute(ctx)
}

type nativeTableBatchSource struct {
	reader               engineplugin.BatchReadableProvider
	tableSessionProvider engineplugin.TableReadSessionProvider
	connInfo             engineplugin.ConnectionInfo
	path                 engineplugin.CatalogPath
	query                string
	readOptions          map[string]interface{}
}

func (s *nativeTableBatchSource) Open(ctx context.Context) (TableBatchReader, error) {
	if s.tableSessionProvider != nil {
		session, err := s.tableSessionProvider.OpenTableReadSession(ctx, s.connInfo, s.path, engineplugin.TableReadSessionOptions{
			Query:    s.query,
			Metadata: s.readOptions,
		})
		if err != nil {
			return nil, fmt.Errorf("open native table read session: %w", err)
		}
		return &nativeTableSessionBatchReader{session: session}, nil
	}
	if s.reader == nil {
		return nil, fmt.Errorf("native table source requires batch reader")
	}
	return &nativeOffsetBatchReader{
		reader:   s.reader,
		connInfo: s.connInfo,
		path:     s.path,
		query:    s.query,
	}, nil
}

type nativeTableSessionBatchReader struct {
	session engineplugin.TableReadSession
	schema  *format.TableInfo
	closed  bool
}

func (r *nativeTableSessionBatchReader) Schema() *format.TableInfo {
	return r.schema
}

func (r *nativeTableSessionBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	if r.closed {
		return &engineplugin.BatchData{}, nil
	}
	batch, err := r.session.ReadBatch(ctx, limit)
	if err != nil {
		_ = r.Close(ctx)
		return nil, err
	}
	if !tableSchemaEmpty(tableInfoFromBatch(batch)) {
		r.schema = tableInfoFromBatch(batch)
	}
	if batch == nil || len(batch.Rows) == 0 || len(batch.Rows) < limit {
		if err := r.Close(ctx); err != nil {
			return batch, err
		}
	}
	return batch, nil
}

func (r *nativeTableSessionBatchReader) Close(ctx context.Context) error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.session.Close(ctx)
}

type nativeOffsetBatchReader struct {
	reader   engineplugin.BatchReadableProvider
	connInfo engineplugin.ConnectionInfo
	path     engineplugin.CatalogPath
	query    string
	offset   int64
	schema   *format.TableInfo
	done     bool
}

func (r *nativeOffsetBatchReader) Schema() *format.TableInfo {
	return r.schema
}

func (r *nativeOffsetBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	if r.done {
		return &engineplugin.BatchData{}, nil
	}
	batch, err := r.reader.ReadBatch(ctx, r.connInfo, r.path, engineplugin.BatchReadOptions{
		Limit:  limit,
		Offset: r.offset,
		Query:  r.query,
	})
	if err != nil {
		return nil, fmt.Errorf("read native source batch at offset %d: %w", r.offset, err)
	}
	if batch != nil {
		r.offset += int64(len(batch.Rows))
		if !tableSchemaEmpty(tableInfoFromBatch(batch)) {
			r.schema = tableInfoFromBatch(batch)
		}
		if len(batch.Rows) < limit {
			r.done = true
		}
	}
	return batch, nil
}

func (r *nativeOffsetBatchReader) Close(context.Context) error {
	return nil
}

type encodedContentTableSource struct {
	reader            engineplugin.ContentReadableProvider
	tableProvider     format.TableReaderProvider
	componentProvider componentTableSourceProvider
	sampleProvider    format.TableSampleReader
	infoProvider      format.TableInfoProvider
	connInfo          engineplugin.ConnectionInfo
	path              engineplugin.CatalogPath
	readOptions       engineplugin.ReadOptions
	parseOptions      *format.ParseOptions
}

func (s *encodedContentTableSource) Open(ctx context.Context) (TableBatchReader, error) {
	if s.componentProvider != nil {
		resourceReader := newEngineResourceReader(s.reader, s.connInfo, s.path, s.readOptions)
		components := resource.SameBasenameComponents(s.path.StringPath(), s.componentProvider.ComponentSpecs())
		componentReader := resource.NewStaticComponentReader(resourceReader, components)
		schema, err := s.componentProvider.DescribeTableComponents(ctx, componentReader, s.parseOptions)
		if err != nil {
			return nil, fmt.Errorf("describe encoded source table components: %w", err)
		}
		return &componentEncodedTableBatchReader{
			componentReader: componentReader,
			provider:        s.componentProvider,
			schema:          schema,
			parseOptions:    s.parseOptions,
		}, nil
	}

	schema, err := s.describeSchema(ctx)
	if err != nil {
		return nil, err
	}
	if s.tableProvider == nil {
		if s.sampleProvider == nil {
			return nil, fmt.Errorf("encoded source requires table reader or sample provider")
		}
		return &sampleEncodedTableBatchReader{
			source: s,
			schema: schema,
		}, nil
	}

	input, err := s.reader.OpenContent(ctx, s.connInfo, s.path, s.readOptions)
	if err != nil {
		return nil, fmt.Errorf("open encoded source content: %w", err)
	}
	tableReader, err := s.tableProvider.OpenTableReader(ctx, input, s.parseOptions)
	if err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("open encoded source table reader: %w", err)
	}
	if tableSchemaEmpty(schema) {
		schema = tableReader.Schema()
	}
	return &encodedTableBatchReader{input: input, tableReader: tableReader, schema: schema}, nil
}

func (s *encodedContentTableSource) describeSchema(ctx context.Context) (*format.TableInfo, error) {
	if s.infoProvider == nil {
		return nil, nil
	}
	input, err := s.reader.OpenContent(ctx, s.connInfo, s.path, s.readOptions)
	if err != nil {
		return nil, fmt.Errorf("open encoded source content for table info: %w", err)
	}
	info, describeErr := s.infoProvider.DescribeTable(ctx, input, s.parseOptions)
	closeErr := input.Close()
	if describeErr != nil {
		return nil, fmt.Errorf("describe encoded source table: %w", describeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close encoded source content after table info: %w", closeErr)
	}
	return info, nil
}

type encodedTableBatchReader struct {
	input       io.Closer
	tableReader format.TableReader
	schema      *format.TableInfo
	offset      int64
}

func (r *encodedTableBatchReader) Schema() *format.TableInfo {
	if !tableSchemaEmpty(r.schema) {
		return r.schema
	}
	return r.tableReader.Schema()
}

func (r *encodedTableBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	rows, err := r.tableReader.ReadRows(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("read encoded source rows at offset %d: %w", r.offset, err)
	}
	batch := &engineplugin.BatchData{
		Rows:   rows,
		Fields: tableInfoFields(r.Schema()),
		Offset: r.offset,
	}
	r.offset += int64(len(rows))
	return batch, nil
}

func (r *encodedTableBatchReader) Close(ctx context.Context) error {
	var firstErr error
	if r.tableReader != nil {
		firstErr = r.tableReader.Close(ctx)
	}
	if r.input != nil {
		if err := r.input.Close(); firstErr == nil && err != nil {
			firstErr = err
		}
	}
	return firstErr
}

type componentEncodedTableBatchReader struct {
	componentReader resource.ComponentReader
	provider        format.ComponentTableProvider
	schema          *format.TableInfo
	parseOptions    *format.ParseOptions
	offset          int64
	done            bool
}

func (r *componentEncodedTableBatchReader) Schema() *format.TableInfo {
	return r.schema
}

func (r *componentEncodedTableBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	if r.done {
		return &engineplugin.BatchData{}, nil
	}
	rows, err := r.provider.SampleTableComponents(ctx, r.componentReader, r.offset, int64(limit), r.parseOptions)
	if err != nil {
		return nil, fmt.Errorf("sample encoded source table components at offset %d: %w", r.offset, err)
	}
	batch := &engineplugin.BatchData{
		Rows:   rows,
		Fields: tableInfoFields(r.schema),
		Offset: r.offset,
	}
	r.offset += int64(len(rows))
	if len(rows) < limit {
		r.done = true
	}
	return batch, nil
}

func (r *componentEncodedTableBatchReader) Close(context.Context) error {
	return nil
}

type sampleEncodedTableBatchReader struct {
	source *encodedContentTableSource
	schema *format.TableInfo
	offset int64
	done   bool
}

func (r *sampleEncodedTableBatchReader) Schema() *format.TableInfo {
	return r.schema
}

func (r *sampleEncodedTableBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	if r.done {
		return &engineplugin.BatchData{}, nil
	}
	input, err := r.source.reader.OpenContent(ctx, r.source.connInfo, r.source.path, r.source.readOptions)
	if err != nil {
		return nil, fmt.Errorf("open encoded source content at offset %d: %w", r.offset, err)
	}
	rows, sampleErr := r.source.sampleProvider.SampleTable(ctx, input, r.offset, int64(limit), r.source.parseOptions)
	closeErr := input.Close()
	if sampleErr != nil {
		return nil, fmt.Errorf("sample encoded source table at offset %d: %w", r.offset, sampleErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close encoded source content at offset %d: %w", r.offset, closeErr)
	}
	batch := &engineplugin.BatchData{
		Rows:   rows,
		Fields: tableInfoFields(r.schema),
		Offset: r.offset,
	}
	r.offset += int64(len(rows))
	if len(rows) < limit {
		r.done = true
	}
	return batch, nil
}

func (r *sampleEncodedTableBatchReader) Close(context.Context) error {
	return nil
}

type nativeTableBatchTarget struct {
	preparer             engineplugin.TableWritePreparer
	writer               engineplugin.BatchWritableProvider
	tableSessionProvider engineplugin.TableWriteSessionProvider
	connInfo             engineplugin.ConnectionInfo
	path                 engineplugin.CatalogPath
	prepareOptions       engineplugin.TableWriteOptions
	writeOptions         engineplugin.BatchWriteOptions
}

func (t *nativeTableBatchTarget) Open(ctx context.Context, schema *format.TableInfo) (TableBatchWriter, error) {
	if err := t.prepare(ctx, schema); err != nil {
		return nil, err
	}
	fields := tableInfoFields(schema)
	if t.tableSessionProvider != nil && isCopyWriteMethod(t.writeOptions.Method) {
		session, err := t.tableSessionProvider.OpenTableWriteSession(ctx, t.connInfo, t.path, engineplugin.TableWriteSessionOptions{
			Mode:   t.writeOptions.Mode,
			Method: t.writeOptions.Method,
			Fields: fields,
		})
		if err != nil {
			return nil, fmt.Errorf("open native table write session: %w", err)
		}
		return &nativeTableSessionBatchWriter{session: session}, nil
	}
	if t.writer == nil {
		return nil, fmt.Errorf("native table target requires batch writer")
	}
	return &nativeDirectBatchWriter{
		writer:       t.writer,
		connInfo:     t.connInfo,
		path:         t.path,
		writeOptions: t.writeOptions,
		fields:       fields,
	}, nil
}

func (t *nativeTableBatchTarget) prepare(ctx context.Context, schema *format.TableInfo) error {
	mode := normalizeImportPrepareMode(t.prepareOptions.Mode)
	if mode == "" {
		return nil
	}
	if t.preparer == nil {
		return fmt.Errorf("target engine does not implement table write prepare for mode %q", mode)
	}
	opts := engineplugin.TableWriteOptions{
		Mode:   mode,
		Fields: tableInfoFields(schema),
	}
	if err := t.preparer.PrepareTableWrite(ctx, t.connInfo, t.path, opts); err != nil {
		return fmt.Errorf("prepare native table write: %w", err)
	}
	return nil
}

type nativeDirectBatchWriter struct {
	writer       engineplugin.BatchWritableProvider
	connInfo     engineplugin.ConnectionInfo
	path         engineplugin.CatalogPath
	writeOptions engineplugin.BatchWriteOptions
	fields       []engineplugin.FieldInfo
}

func (w *nativeDirectBatchWriter) WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if len(batch.Fields) == 0 && len(w.fields) > 0 {
		copyBatch := *batch
		copyBatch.Fields = append([]engineplugin.FieldInfo(nil), w.fields...)
		batch = &copyBatch
	}
	if err := w.writer.WriteBatch(ctx, w.connInfo, w.path, batch, w.writeOptions); err != nil {
		return fmt.Errorf("write native table batch at offset %d: %w", batch.Offset, err)
	}
	return nil
}

func (w *nativeDirectBatchWriter) Close(context.Context) error {
	return nil
}

func (w *nativeDirectBatchWriter) Abort(context.Context) error {
	return nil
}

type nativeTableSessionBatchWriter struct {
	session engineplugin.TableWriteSession
	closed  bool
}

func (w *nativeTableSessionBatchWriter) WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if err := w.session.WriteBatch(ctx, batch); err != nil {
		_ = w.session.Abort(ctx)
		w.closed = true
		return fmt.Errorf("write native table session batch at offset %d: %w", batch.Offset, err)
	}
	return nil
}

func (w *nativeTableSessionBatchWriter) Close(ctx context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	if err := w.session.Close(ctx); err != nil {
		return fmt.Errorf("close native table write session: %w", err)
	}
	return nil
}

func (w *nativeTableSessionBatchWriter) Abort(ctx context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.session.Abort(ctx)
}

type encodedContentTableTarget struct {
	writer            engineplugin.ContentWritableProvider
	formatProvider    format.TableWriterProvider
	componentProvider format.ComponentTableWriterProvider
	connInfo          engineplugin.ConnectionInfo
	path              engineplugin.CatalogPath
	writeOptions      engineplugin.WriteOptions
	formatOptions     *format.WriteOptions
}

func (t *encodedContentTableTarget) Open(ctx context.Context, schema *format.TableInfo) (TableBatchWriter, error) {
	applySpatialInfoFromOptions(schema, t.formatOptions)
	if tableSchemaEmpty(schema) && t.componentProvider == nil {
		output, err := t.writer.CreateContent(ctx, t.connInfo, t.path, t.writeOptions)
		if err != nil {
			return nil, fmt.Errorf("create empty encoded target content: %w", err)
		}
		return &emptyContentBatchWriter{output: output}, nil
	}
	if t.componentProvider != nil {
		componentWriter := newContentComponentWriter(t.writer, t.connInfo, t.path, t.writeOptions, t.componentProvider.ComponentSpecs())
		tableWriter, err := t.componentProvider.OpenComponentTableWriter(ctx, componentWriter, resourceRefFromCatalogPath(t.path), schema, t.formatOptions)
		if err != nil {
			_ = componentWriter.AbortComponents(ctx)
			return nil, fmt.Errorf("open encoded component table writer: %w", err)
		}
		return &componentTableBatchWriter{componentWriter: componentWriter, tableWriter: tableWriter}, nil
	}

	output, err := t.writer.CreateContent(ctx, t.connInfo, t.path, t.writeOptions)
	if err != nil {
		return nil, fmt.Errorf("create encoded target content: %w", err)
	}
	tableWriter, err := t.formatProvider.OpenTableWriter(ctx, output, schema, t.formatOptions)
	if err != nil {
		_ = output.Close()
		return nil, fmt.Errorf("open encoded target table writer: %w", err)
	}
	return &contentTableBatchWriter{output: output, tableWriter: tableWriter}, nil
}

type emptyContentBatchWriter struct {
	output io.Closer
	closed bool
}

func (w *emptyContentBatchWriter) WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	return fmt.Errorf("empty encoded target cannot accept non-empty batch")
}

func (w *emptyContentBatchWriter) Close(context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.output.Close()
}

func (w *emptyContentBatchWriter) Abort(context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.output.Close()
}

type contentTableBatchWriter struct {
	output      io.Closer
	tableWriter format.TableWriter
	closed      bool
}

func (w *contentTableBatchWriter) WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if err := w.tableWriter.WriteRows(ctx, batch.Rows); err != nil {
		return fmt.Errorf("write encoded target rows at offset %d: %w", batch.Offset, err)
	}
	return nil
}

func (w *contentTableBatchWriter) Close(ctx context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	if err := w.tableWriter.Close(ctx); err != nil {
		return fmt.Errorf("close encoded target table writer: %w", err)
	}
	if err := w.output.Close(); err != nil {
		return fmt.Errorf("close encoded target content: %w", err)
	}
	return nil
}

func (w *contentTableBatchWriter) Abort(context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.output.Close()
}

type componentTableBatchWriter struct {
	componentWriter resource.ComponentWriter
	tableWriter     format.TableWriter
	closed          bool
}

func (w *componentTableBatchWriter) WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if err := w.tableWriter.WriteRows(ctx, batch.Rows); err != nil {
		return fmt.Errorf("write encoded component target rows at offset %d: %w", batch.Offset, err)
	}
	return nil
}

func (w *componentTableBatchWriter) Close(ctx context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.tableWriter.Close(ctx)
}

func (w *componentTableBatchWriter) Abort(ctx context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.componentWriter.AbortComponents(ctx)
}
