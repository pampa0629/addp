package executor

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

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

type componentTargetResourceDeleter struct {
	provider   engineplugin.ResourceDeleteProvider
	connInfo   engineplugin.ConnectionInfo
	basePath   engineplugin.CatalogPath
	components []resource.ComponentRef
}

func (d *componentTargetResourceDeleter) DeleteTarget(ctx context.Context) error {
	if d == nil || d.provider == nil {
		return nil
	}
	for _, component := range d.components {
		path, err := contentCatalogPathForComponent(d.basePath, component.ResourceRef)
		if err != nil {
			return err
		}
		if err := d.provider.DeleteResource(ctx, d.connInfo, path); err != nil {
			return err
		}
	}
	return nil
}

func contentCatalogPathForComponent(base engineplugin.CatalogPath, ref resource.ResourceRef) (engineplugin.CatalogPath, error) {
	if len(base.Segments) == 0 {
		return engineplugin.CatalogPath{}, fmt.Errorf("resource base path requires at least one segment")
	}
	name := filepath.Base(ref.Path)
	if name == "." || name == "/" || name == "" {
		return engineplugin.CatalogPath{}, fmt.Errorf("resource path %q has no file name", ref.Path)
	}
	next := engineplugin.CatalogPath{
		Version:  base.Version,
		EngineID: base.EngineID,
		Segments: append([]engineplugin.CatalogSegment(nil), base.Segments...),
	}
	if next.Version == "" {
		next.Version = engineplugin.CatalogPathVersion
	}
	last := &next.Segments[len(next.Segments)-1]
	last.Name = name
	if strings.TrimSpace(last.Term) == "" {
		last.Term = engineplugin.CatalogTermFile
	}
	if strings.TrimSpace(last.Kind) == "" {
		last.Kind = engineplugin.CatalogKindFile
	}
	return next, nil
}

type TablePipeline struct {
	Source     TableBatchSource
	Target     TableBatchTarget
	Transforms []TableTransformPlan
	BatchSize  int
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
		if len(transforms) > 0 {
			offset := batch.Offset
			batch, err = applyBatchTransforms(ctx, batch, transforms)
			if err != nil {
				return metrics, fmt.Errorf("transform table batch at offset %d: %w", offset, err)
			}
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
		resourceReader := resource.NewEngineContentReader(s.reader, s.connInfo, s.path, s.readOptions)
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
	deleter           *engineTargetResourceDeleter
	formatProvider    format.TableWriterProvider
	componentProvider format.ComponentTableWriterProvider
	connInfo          engineplugin.ConnectionInfo
	path              engineplugin.CatalogPath
	writeOptions      engineplugin.WriteOptions
	formatOptions     *format.WriteOptions
}

func (t *encodedContentTableTarget) Open(ctx context.Context, schema *format.TableInfo) (TableBatchWriter, error) {
	applySpatialInfoFromOptions(schema, t.formatOptions)
	if t.deleter != nil {
		if err := t.deleteExistingTarget(ctx); err != nil {
			return nil, err
		}
	}
	if tableSchemaEmpty(schema) && t.componentProvider == nil {
		output, err := t.writer.CreateContent(ctx, t.connInfo, t.path, t.writeOptions)
		if err != nil {
			return nil, fmt.Errorf("create empty encoded target content: %w", err)
		}
		return &emptyContentBatchWriter{output: output}, nil
	}
	if t.componentProvider != nil {
		resourceWriter := resource.NewEngineContentWriter(t.writer, t.connInfo, t.path, t.writeOptions)
		componentWriter := resource.NewStaticComponentWriter(resourceWriter, resource.SameBasenameComponents(t.path.StringPath(), t.componentProvider.ComponentSpecs()))
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

func (t *encodedContentTableTarget) deleteExistingTarget(ctx context.Context) error {
	if t.componentProvider == nil {
		if err := t.deleter.DeleteTarget(ctx); err != nil {
			return fmt.Errorf("delete encoded target before write: %w", err)
		}
		return nil
	}
	componentDeleter := &componentTargetResourceDeleter{
		provider:   t.deleter.provider,
		connInfo:   t.deleter.connInfo,
		basePath:   t.path,
		components: resource.SameBasenameComponents(t.path.StringPath(), t.componentProvider.ComponentSpecs()),
	}
	if err := componentDeleter.DeleteTarget(ctx); err != nil {
		return fmt.Errorf("delete encoded component target before write: %w", err)
	}
	return nil
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
