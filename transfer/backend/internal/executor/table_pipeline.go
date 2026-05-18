package executor

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
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

type multiTableSourceProvider interface {
	format.MultiTableProvider
	format.RelatedRefSpecProvider
}

type targetResourceDeleter interface {
	DeleteTarget(ctx context.Context) error
}

type engineTargetResourceDeleter struct {
	provider engineplugin.ResourceDeleteProvider
	connInfo engineplugin.ConnectionInfo
	path     engineplugin.CatalogPath
}

func (d *engineTargetResourceDeleter) DeleteTarget(ctx context.Context) error {
	if d == nil || d.provider == nil {
		return nil
	}
	return d.provider.DeleteResource(ctx, d.connInfo, d.path)
}

type multiTargetResourceDeleter struct {
	provider engineplugin.ResourceDeleteProvider
	connInfo engineplugin.ConnectionInfo
	basePath engineplugin.CatalogPath
	refs     []contentio.Ref
}

func (d *multiTargetResourceDeleter) DeleteTarget(ctx context.Context) error {
	if d == nil || d.provider == nil {
		return nil
	}
	for _, ref := range d.refs {
		path, err := contentadapter.CatalogPath(d.basePath, ref)
		if err != nil {
			return err
		}
		if err := d.provider.DeleteResource(ctx, d.connInfo, path); err != nil {
			return err
		}
	}
	return nil
}

type TablePipeline struct {
	Source           TableBatchSource
	Target           TableBatchTarget
	Transforms       []TableTransformPlan
	BatchSize        int
	ProgressCallback TableProgressCallback
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
	transforms, err := buildTableTransforms(p.Transforms)
	if err != nil {
		return nil, err
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
	schema, err = applySchemaTransforms(schema, transforms)
	if err != nil {
		return nil, fmt.Errorf("transform table schema: %w", err)
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
		sourceOffset := batch.Offset
		if len(transforms) > 0 {
			batch, err = applyBatchTransforms(ctx, batch, transforms)
			if err != nil {
				return metrics, fmt.Errorf("transform table batch at offset %d: %w", sourceOffset, err)
			}
		}
		if err := writer.WriteBatch(ctx, batch); err != nil {
			return metrics, err
		}
		rowCount := int64(len(batch.Rows))
		metrics.RecordsRead += rowCount
		metrics.RecordsWritten += rowCount
		metrics.Batches++
		if p.ProgressCallback != nil {
			if err := p.ProgressCallback(ctx, TableProgressEvent{
				BatchIndex:     metrics.Batches,
				SourceOffset:   sourceOffset,
				BatchRows:      rowCount,
				RecordsRead:    metrics.RecordsRead,
				RecordsWritten: metrics.RecordsWritten,
			}); err != nil {
				return metrics, fmt.Errorf("update table transfer progress at batch %d: %w", metrics.Batches, err)
			}
		}
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
	reader              engineplugin.ContentReadableProvider
	tableProvider       format.TableReaderProvider
	multiReaderProvider format.MultiTableReaderProvider
	multiProvider       multiTableSourceProvider
	sampleProvider      format.TableSampleReader
	infoProvider        format.TableInfoProvider
	connInfo            engineplugin.ConnectionInfo
	path                engineplugin.CatalogPath
	readOptions         engineplugin.ReadOptions
	parseOptions        *format.ParseOptions
}

func (s *encodedContentTableSource) Open(ctx context.Context) (TableBatchReader, error) {
	if s.multiReaderProvider != nil {
		reader, refs := s.refReader(s.multiReaderProvider.RelatedRefSpecs())
		tableReader, err := s.multiReaderProvider.OpenMultiTableReader(ctx, reader, refs, s.parseOptions)
		if err != nil {
			return nil, fmt.Errorf("open encoded source multi table reader: %w", err)
		}
		return &multiTableBatchReader{
			tableReader: tableReader,
			schema:      tableReader.Schema(),
		}, nil
	}

	if s.multiProvider != nil {
		reader, refs := s.refReader(s.multiProvider.RelatedRefSpecs())
		schema, err := s.multiProvider.DescribeMultiTable(ctx, reader, refs, s.parseOptions)
		if err != nil {
			return nil, fmt.Errorf("describe encoded source table refs: %w", err)
		}
		return &multiEncodedTableBatchReader{
			reader:       reader,
			refs:         refs,
			provider:     s.multiProvider,
			schema:       schema,
			parseOptions: s.parseOptions,
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

	input, err := s.contentReader().Open(ctx, contentRefFromCatalogPath(s.path))
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

type multiTableBatchReader struct {
	tableReader format.TableReader
	schema      *format.TableInfo
	offset      int64
}

func (r *multiTableBatchReader) Schema() *format.TableInfo {
	if !tableSchemaEmpty(r.schema) {
		return r.schema
	}
	return r.tableReader.Schema()
}

func (r *multiTableBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	rows, err := r.tableReader.ReadRows(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("read encoded source multi table rows at offset %d: %w", r.offset, err)
	}
	batch := &engineplugin.BatchData{
		Rows:   rows,
		Fields: tableInfoFields(r.Schema()),
		Offset: r.offset,
	}
	r.offset += int64(len(rows))
	return batch, nil
}

func (r *multiTableBatchReader) Close(ctx context.Context) error {
	if r.tableReader == nil {
		return nil
	}
	return r.tableReader.Close(ctx)
}

func (s *encodedContentTableSource) describeSchema(ctx context.Context) (*format.TableInfo, error) {
	if s.infoProvider == nil {
		return nil, nil
	}
	input, err := s.contentReader().Open(ctx, contentRefFromCatalogPath(s.path))
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

type multiEncodedTableBatchReader struct {
	reader       contentio.Reader
	refs         []contentio.Ref
	provider     format.MultiTableProvider
	schema       *format.TableInfo
	parseOptions *format.ParseOptions
	offset       int64
	done         bool
}

func (r *multiEncodedTableBatchReader) Schema() *format.TableInfo {
	return r.schema
}

func (r *multiEncodedTableBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	if r.done {
		return &engineplugin.BatchData{}, nil
	}
	rows, err := r.provider.SampleMultiTable(ctx, r.reader, r.refs, r.offset, int64(limit), r.parseOptions)
	if err != nil {
		return nil, fmt.Errorf("sample encoded source table refs at offset %d: %w", r.offset, err)
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

func (r *multiEncodedTableBatchReader) Close(context.Context) error {
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
	input, err := r.source.contentReader().Open(ctx, contentRefFromCatalogPath(r.source.path))
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

func (s *encodedContentTableSource) contentReader() contentio.Reader {
	return contentadapter.NewMappedReader(s.reader, s.connInfo, contentadapter.FixedPathMapper(s.path), s.readOptions)
}

func (s *encodedContentTableSource) refReader(specs []contentio.RelatedRefSpec) (contentio.Reader, []contentio.Ref) {
	return contentadapter.NewReader(s.reader, s.connInfo, s.path, s.readOptions), contentio.SameBasenameRefs(s.path.StringPath(), specs)
}

type nativeTableBatchTarget struct {
	deleter              *engineTargetResourceDeleter
	preparer             engineplugin.TableWritePreparer
	writer               engineplugin.BatchWritableProvider
	tableSessionProvider engineplugin.TableWriteSessionProvider
	connInfo             engineplugin.ConnectionInfo
	path                 engineplugin.CatalogPath
	prepareOptions       engineplugin.TableWriteOptions
	writeOptions         engineplugin.BatchWriteOptions
}

func (t *nativeTableBatchTarget) Open(ctx context.Context, schema *format.TableInfo) (TableBatchWriter, error) {
	if t.deleter != nil {
		if err := t.deleter.DeleteTarget(ctx); err != nil {
			return nil, fmt.Errorf("delete native table target before write: %w", err)
		}
	}
	if err := t.prepare(ctx, schema); err != nil {
		return nil, err
	}
	fields := tableInfoFields(schema)
	if t.tableSessionProvider != nil && isCopyWriteMethod(t.writeOptions.Method) {
		session, err := t.tableSessionProvider.OpenTableWriteSession(ctx, t.connInfo, t.path, engineplugin.TableWriteSessionOptions{
			Method: t.writeOptions.Method,
			Fields: fields,
		})
		if err != nil {
			return nil, fmt.Errorf("open native table write session: %w", err)
		}
		return &nativeTableSessionBatchWriter{session: session, fields: fields}, nil
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
	if t.preparer == nil {
		return fmt.Errorf("target engine does not implement table write prepare")
	}
	opts := engineplugin.TableWriteOptions{
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
	batch = batchWithTargetFields(batch, w.fields)
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

func batchWithTargetFields(batch *engineplugin.BatchData, fields []engineplugin.FieldInfo) *engineplugin.BatchData {
	if batch == nil || len(fields) == 0 {
		return batch
	}
	copyBatch := *batch
	copyBatch.Fields = append([]engineplugin.FieldInfo(nil), fields...)
	return &copyBatch
}

type nativeTableSessionBatchWriter struct {
	session engineplugin.TableWriteSession
	fields  []engineplugin.FieldInfo
	closed  bool
}

func (w *nativeTableSessionBatchWriter) WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	batch = batchWithTargetFields(batch, w.fields)
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
	writer         engineplugin.ContentWritableProvider
	deleter        *engineTargetResourceDeleter
	formatProvider format.TableWriterProvider
	multiProvider  format.MultiTableWriterProvider
	connInfo       engineplugin.ConnectionInfo
	path           engineplugin.CatalogPath
	writeOptions   engineplugin.WriteOptions
	formatOptions  *format.WriteOptions
}

func (t *encodedContentTableTarget) Open(ctx context.Context, schema *format.TableInfo) (TableBatchWriter, error) {
	applySpatialInfoFromOptions(schema, t.formatOptions)
	if t.deleter != nil {
		if err := t.deleteExistingTarget(ctx); err != nil {
			return nil, err
		}
	}
	if tableSchemaEmpty(schema) && t.multiProvider == nil {
		output, err := t.contentWriter().Create(ctx, contentRefFromCatalogPath(t.path))
		if err != nil {
			return nil, fmt.Errorf("create empty encoded target content: %w", err)
		}
		return &emptyContentBatchWriter{output: output}, nil
	}
	if t.multiProvider != nil {
		writer, refs := t.refWriter(t.multiProvider.RelatedRefSpecs())
		tableWriter, err := t.multiProvider.OpenMultiTableWriter(ctx, writer, refs, contentRefFromCatalogPath(t.path), schema, t.formatOptions)
		if err != nil {
			return nil, fmt.Errorf("open encoded multi table writer: %w", err)
		}
		return &multiTableBatchWriter{tableWriter: tableWriter}, nil
	}

	output, err := t.contentWriter().Create(ctx, contentRefFromCatalogPath(t.path))
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

func (t *encodedContentTableTarget) deleteExistingTarget(ctx context.Context) error {
	if t.multiProvider == nil {
		if err := t.deleter.DeleteTarget(ctx); err != nil {
			return fmt.Errorf("delete encoded target before write: %w", err)
		}
		return nil
	}
	multiDeleter := &multiTargetResourceDeleter{
		provider: t.deleter.provider,
		connInfo: t.deleter.connInfo,
		basePath: t.path,
		refs:     contentio.SameBasenameRefs(t.path.StringPath(), t.multiProvider.RelatedRefSpecs()),
	}
	if err := multiDeleter.DeleteTarget(ctx); err != nil {
		return fmt.Errorf("delete encoded multi target before write: %w", err)
	}
	return nil
}

func (t *encodedContentTableTarget) contentWriter() contentio.Writer {
	return contentadapter.NewMappedWriter(t.writer, t.connInfo, contentadapter.FixedPathMapper(t.path), t.writeOptions)
}

func (t *encodedContentTableTarget) refWriter(specs []contentio.RelatedRefSpec) (contentio.Writer, []contentio.Ref) {
	return contentadapter.NewWriter(t.writer, t.connInfo, t.path, t.writeOptions), contentio.SameBasenameRefs(t.path.StringPath(), specs)
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

type multiTableBatchWriter struct {
	tableWriter format.TableWriter
	closed      bool
}

func (w *multiTableBatchWriter) WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if err := w.tableWriter.WriteRows(ctx, batch.Rows); err != nil {
		return fmt.Errorf("write encoded multi target rows at offset %d: %w", batch.Offset, err)
	}
	return nil
}

func (w *multiTableBatchWriter) Close(ctx context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.tableWriter.Close(ctx)
}

func (w *multiTableBatchWriter) Abort(ctx context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.tableWriter != nil {
		return w.tableWriter.Close(ctx)
	}
	return nil
}
