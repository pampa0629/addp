package executor

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
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
	TableInfo() *datatype.TableInfo
	SpatialInfo() *datatype.SpatialInfo
	ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error)
	Close(ctx context.Context) error
	ResumeMarker() *resume.Marker
}

type TableBatchTarget interface {
	Open(ctx context.Context, tableInfo *datatype.TableInfo, spatialInfo *datatype.SpatialInfo) (TableBatchWriter, error)
}

type TableBatchWriter interface {
	WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error
	Close(ctx context.Context) error
	Abort(ctx context.Context) error
	CommitMarker() *resume.Marker
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
	refs     []format.RelatedRef
}

func (d *multiTargetResourceDeleter) DeleteTarget(ctx context.Context) error {
	if d == nil || d.provider == nil {
		return nil
	}
	for _, ref := range d.refs {
		path, err := contentadapter.CatalogPath(d.basePath, ref.Ref)
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

	tableInfo := reader.TableInfo()
	spatialInfo := reader.SpatialInfo()
	var firstBatch *engineplugin.BatchData
	if tableInfoEmpty(tableInfo) {
		firstBatch, err = reader.ReadBatch(ctx, batchSize)
		if err != nil {
			return nil, err
		}
		if firstBatch != nil && len(firstBatch.Rows) > 0 {
			tableInfo = tableInfoFromBatch(firstBatch)
			if spatialInfo == nil {
				spatialInfo = spatialInfoFromBatch(firstBatch)
			}
		}
	}
	tableInfo, spatialInfo, err = applyTableInfoTransforms(tableInfo, spatialInfo, transforms)
	if err != nil {
		return nil, fmt.Errorf("transform table info: %w", err)
	}
	if tableInfo == nil {
		tableInfo = &datatype.TableInfo{}
	}
	writer, err := p.Target.Open(ctx, tableInfo, spatialInfo)
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
	var lastSourceOffset int64
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
		lastSourceOffset = sourceOffset
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
				ResumeMarker:   cloneResumeMarker(reader.ResumeMarker()),
				CommitMarker:   cloneResumeMarker(writer.CommitMarker()),
			}); err != nil {
				return metrics, fmt.Errorf("update table transfer progress at batch %d: %w", metrics.Batches, err)
			}
		}
	}
	if err := writer.Close(ctx); err != nil {
		return metrics, err
	}
	writerClosed = true
	if p.ProgressCallback != nil {
		if err := p.ProgressCallback(ctx, TableProgressEvent{
			BatchIndex:     metrics.Batches,
			SourceOffset:   lastSourceOffset,
			RecordsRead:    metrics.RecordsRead,
			RecordsWritten: metrics.RecordsWritten,
			ResumeMarker:   cloneResumeMarker(reader.ResumeMarker()),
			CommitMarker:   cloneResumeMarker(writer.CommitMarker()),
			Final:          true,
		}); err != nil {
			return metrics, fmt.Errorf("update final table transfer checkpoint: %w", err)
		}
	}
	return metrics, nil
}

func tableInfoEmpty(tableInfo *datatype.TableInfo) bool {
	return tableInfo == nil || len(tableInfo.Fields) == 0
}

type nativeTableBatchSource struct {
	reader               engineplugin.BatchReadableProvider
	tableSessionProvider engineplugin.TableReadSessionProvider
	connInfo             engineplugin.ConnectionInfo
	path                 engineplugin.CatalogPath
	query                string
	readOptions          map[string]interface{}
	resumeMarker         *resume.Marker
	tableInfo            *datatype.TableInfo
}

func (s *nativeTableBatchSource) Open(ctx context.Context) (TableBatchReader, error) {
	if s.tableSessionProvider != nil {
		session, err := s.tableSessionProvider.OpenTableReadSession(ctx, s.connInfo, s.path, engineplugin.TableReadSessionOptions{
			Query:        s.query,
			Metadata:     s.readOptions,
			ResumeMarker: cloneResumeMarker(s.resumeMarker),
		})
		if err != nil {
			return nil, fmt.Errorf("open native table read session: %w", err)
		}
		return &nativeTableSessionBatchReader{session: session, tableInfo: s.tableInfo}, nil
	}
	if s.reader == nil {
		return nil, fmt.Errorf("native table source requires batch reader")
	}
	return &nativeOffsetBatchReader{
		reader:    s.reader,
		connInfo:  s.connInfo,
		path:      s.path,
		query:     s.query,
		tableInfo: s.tableInfo,
	}, nil
}

type nativeTableSessionBatchReader struct {
	session   engineplugin.TableReadSession
	tableInfo *datatype.TableInfo
	closed    bool
}

func (r *nativeTableSessionBatchReader) TableInfo() *datatype.TableInfo {
	return r.tableInfo
}

func (r *nativeTableSessionBatchReader) SpatialInfo() *datatype.SpatialInfo {
	return spatialInfoFromTableInfoOrFields(r.tableInfo)
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
	if tableInfoEmpty(r.tableInfo) && !tableInfoEmpty(tableInfoFromBatch(batch)) {
		r.tableInfo = tableInfoFromBatch(batch)
	}
	if !tableInfoEmpty(r.tableInfo) && batch != nil && batch.Spatial == nil {
		batch.Spatial = r.SpatialInfo()
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

func (r *nativeTableSessionBatchReader) ResumeMarker() *resume.Marker {
	if markerProvider, ok := r.session.(engineplugin.ResumeMarkerProvider); ok {
		return markerProvider.ResumeMarker()
	}
	return nil
}

type nativeOffsetBatchReader struct {
	reader    engineplugin.BatchReadableProvider
	connInfo  engineplugin.ConnectionInfo
	path      engineplugin.CatalogPath
	query     string
	offset    int64
	tableInfo *datatype.TableInfo
	done      bool
}

func (r *nativeOffsetBatchReader) TableInfo() *datatype.TableInfo {
	return r.tableInfo
}

func (r *nativeOffsetBatchReader) SpatialInfo() *datatype.SpatialInfo {
	return spatialInfoFromTableInfoOrFields(r.tableInfo)
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
		if tableInfoEmpty(r.tableInfo) && !tableInfoEmpty(tableInfoFromBatch(batch)) {
			r.tableInfo = tableInfoFromBatch(batch)
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

func (r *nativeOffsetBatchReader) ResumeMarker() *resume.Marker {
	return nil
}

type encodedContentTableSource struct {
	reader              engineplugin.ContentReadableProvider
	tableProvider       format.TableReaderProvider
	multiReaderProvider format.MultiTableReaderProvider
	multiInfoProvider   format.MultiTableInfoProvider
	multiSampleReader   format.MultiTableSampleReader
	scopeReaderProvider format.ScopeTableReaderProvider
	sampleProvider      format.TableSampleReader
	infoProvider        format.TableInfoProvider
	connInfo            engineplugin.ConnectionInfo
	path                engineplugin.CatalogPath
	readOptions         engineplugin.ReadOptions
	parseOptions        *format.ParseOptions
	resumeMarker        *resume.Marker
	tableInfo           *datatype.TableInfo
	spatialInfo         *datatype.SpatialInfo
	relatedRefs         []format.RelatedRef
}

func (s *encodedContentTableSource) Open(ctx context.Context) (TableBatchReader, error) {
	parseOptions := parseOptionsWithResumeMarker(s.parseOptions, s.resumeMarker)
	if s.scopeReaderProvider != nil {
		scope := contentRefFromCatalogPath(s.path)
		scope.Role = contentio.RoleScope
		tableReader, err := s.scopeReaderProvider.OpenTableScopeReader(ctx, s.scopeReader(), scope, parseOptions)
		if err != nil {
			return nil, fmt.Errorf("open encoded source scope table reader: %w", err)
		}
		tableInfo := s.tableInfo
		spatialInfo := s.spatialInfo.Clone()
		if tableInfoEmpty(tableInfo) {
			tableInfo = tableInfoFromFormatReader(tableReader)
		}
		if spatialInfo == nil {
			spatialInfo = spatialInfoFromFormatReader(tableReader)
		}
		return &multiTableBatchReader{
			tableReader: tableReader,
			tableInfo:   tableInfo,
			spatialInfo: spatialInfo,
		}, nil
	}

	if s.multiReaderProvider != nil {
		reader, refs := s.refReader(s.multiReaderProvider.RelatedRefSpecs())
		tableReader, err := s.multiReaderProvider.OpenMultiTableReader(ctx, reader, refs, parseOptions)
		if err != nil {
			return nil, fmt.Errorf("open encoded source multi table reader: %w", err)
		}
		tableInfo := s.tableInfo
		spatialInfo := s.spatialInfo.Clone()
		if tableInfoEmpty(tableInfo) {
			tableInfo = tableInfoFromFormatReader(tableReader)
		}
		if spatialInfo == nil {
			spatialInfo = spatialInfoFromFormatReader(tableReader)
		}
		return &multiTableBatchReader{
			tableReader: tableReader,
			tableInfo:   tableInfo,
			spatialInfo: spatialInfo,
		}, nil
	}

	if s.multiInfoProvider != nil && s.multiSampleReader != nil {
		reader, refs := s.refReader(s.multiInfoProvider.RelatedRefSpecs())
		tableInfo := s.tableInfo
		if tableInfoEmpty(tableInfo) {
			result, err := s.multiInfoProvider.DescribeMultiTable(ctx, reader, refs, parseOptions)
			if err != nil {
				return nil, fmt.Errorf("describe encoded source table refs: %w", err)
			}
			tableInfo = format.TableInfoFromDescribeResult(result)
			s.spatialInfo = result.Spatial.Clone()
		}
		return &multiEncodedTableBatchReader{
			reader:         reader,
			refs:           refs,
			readerProvider: s.multiSampleReader,
			tableInfo:      tableInfo,
			spatialInfo:    s.spatialInfo.Clone(),
			parseOptions:   parseOptions,
		}, nil
	}

	tableInfo := s.tableInfo
	if tableInfoEmpty(tableInfo) {
		var err error
		tableInfo, err = s.describeTableInfo(ctx)
		if err != nil {
			return nil, err
		}
	}
	if s.tableProvider == nil {
		if s.sampleProvider == nil {
			return nil, fmt.Errorf("encoded source requires table reader or sample provider")
		}
		return &sampleEncodedTableBatchReader{
			source:      s,
			tableInfo:   tableInfo,
			spatialInfo: s.spatialInfo.Clone(),
		}, nil
	}

	input, err := s.contentReader().Open(ctx, contentRefFromCatalogPath(s.path))
	if err != nil {
		return nil, fmt.Errorf("open encoded source content: %w", err)
	}
	tableReader, err := s.tableProvider.OpenTableReader(ctx, input, parseOptions)
	if err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("open encoded source table reader: %w", err)
	}
	if tableInfoEmpty(tableInfo) {
		tableInfo = tableInfoFromFormatReader(tableReader)
	}
	spatialInfo := s.spatialInfo.Clone()
	if spatialInfo == nil {
		spatialInfo = spatialInfoFromFormatReader(tableReader)
	}
	return &encodedTableBatchReader{input: input, tableReader: tableReader, tableInfo: tableInfo, spatialInfo: spatialInfo}, nil
}

func parseOptionsWithResumeMarker(options *format.ParseOptions, marker *resume.Marker) *format.ParseOptions {
	if marker == nil {
		return options
	}
	if options == nil {
		options = format.DefaultParseOptions()
	}
	copied := *options
	copied.ResumeMarker = marker.Clone()
	return &copied
}

func cloneResumeMarker(marker *resume.Marker) *resume.Marker {
	if marker == nil {
		return nil
	}
	return marker.Clone()
}

type multiTableBatchReader struct {
	tableReader format.TableReader
	tableInfo   *datatype.TableInfo
	spatialInfo *datatype.SpatialInfo
	offset      int64
}

func (r *multiTableBatchReader) TableInfo() *datatype.TableInfo {
	if !tableInfoEmpty(r.tableInfo) {
		return r.tableInfo
	}
	return tableInfoFromFormatReader(r.tableReader)
}

func (r *multiTableBatchReader) SpatialInfo() *datatype.SpatialInfo {
	if r.spatialInfo != nil {
		return r.spatialInfo.Clone()
	}
	return spatialInfoFromFormatReader(r.tableReader)
}

func (r *multiTableBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	rows, err := r.tableReader.ReadRows(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("read encoded source multi table rows at offset %d: %w", r.offset, err)
	}
	batch := &engineplugin.BatchData{
		Rows:    rows,
		Fields:  tableInfoFields(r.TableInfo()),
		Spatial: r.SpatialInfo(),
		Offset:  r.offset,
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

func (r *multiTableBatchReader) ResumeMarker() *resume.Marker {
	if markerProvider, ok := r.tableReader.(format.ResumeMarkerProvider); ok {
		return markerProvider.ResumeMarker()
	}
	return nil
}

func (s *encodedContentTableSource) describeTableInfo(ctx context.Context) (*datatype.TableInfo, error) {
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
	s.spatialInfo = info.Spatial.Clone()
	return format.TableInfoFromDescribeResult(info), nil
}

func tableInfoFromFormatReader(reader format.TableReader) *datatype.TableInfo {
	if reader == nil {
		return nil
	}
	info := &datatype.TableInfo{
		Fields: reader.Fields(),
	}
	return info
}

func spatialInfoFromFormatReader(reader format.TableReader) *datatype.SpatialInfo {
	if spatialProvider, ok := reader.(format.TableSpatialInfoProvider); ok {
		return spatialProvider.SpatialInfo().Clone()
	}
	return nil
}

func spatialInfoFromTableInfoOrFields(info *datatype.TableInfo) *datatype.SpatialInfo {
	if info == nil {
		return nil
	}
	for _, field := range info.Fields {
		if datatype.IsSpatialFieldType(field.Type) {
			return datatype.NewSingleGeometrySpatialInfo(field.Name, "", 0, 0)
		}
	}
	return nil
}

type encodedTableBatchReader struct {
	input       io.Closer
	tableReader format.TableReader
	tableInfo   *datatype.TableInfo
	spatialInfo *datatype.SpatialInfo
	offset      int64
}

func (r *encodedTableBatchReader) TableInfo() *datatype.TableInfo {
	if !tableInfoEmpty(r.tableInfo) {
		return r.tableInfo
	}
	return tableInfoFromFormatReader(r.tableReader)
}

func (r *encodedTableBatchReader) SpatialInfo() *datatype.SpatialInfo {
	if r.spatialInfo != nil {
		return r.spatialInfo.Clone()
	}
	return spatialInfoFromFormatReader(r.tableReader)
}

func (r *encodedTableBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	rows, err := r.tableReader.ReadRows(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("read encoded source rows at offset %d: %w", r.offset, err)
	}
	batch := &engineplugin.BatchData{
		Rows:    rows,
		Fields:  tableInfoFields(r.TableInfo()),
		Spatial: r.SpatialInfo(),
		Offset:  r.offset,
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

func (r *encodedTableBatchReader) ResumeMarker() *resume.Marker {
	if markerProvider, ok := r.tableReader.(format.ResumeMarkerProvider); ok {
		return markerProvider.ResumeMarker()
	}
	return nil
}

type multiEncodedTableBatchReader struct {
	reader         contentio.Reader
	refs           []format.RelatedRef
	readerProvider format.MultiTableSampleReader
	tableInfo      *datatype.TableInfo
	spatialInfo    *datatype.SpatialInfo
	parseOptions   *format.ParseOptions
	offset         int64
	done           bool
}

func (r *multiEncodedTableBatchReader) TableInfo() *datatype.TableInfo {
	return r.tableInfo
}

func (r *multiEncodedTableBatchReader) SpatialInfo() *datatype.SpatialInfo {
	if r.spatialInfo != nil {
		return r.spatialInfo.Clone()
	}
	return spatialInfoFromTableInfoOrFields(r.tableInfo)
}

func (r *multiEncodedTableBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	if r.done {
		return &engineplugin.BatchData{}, nil
	}
	rows, err := r.readerProvider.SampleMultiTable(ctx, r.reader, r.refs, r.offset, int64(limit), r.parseOptions)
	if err != nil {
		return nil, fmt.Errorf("sample encoded source table refs at offset %d: %w", r.offset, err)
	}
	batch := &engineplugin.BatchData{
		Rows:    rows,
		Fields:  tableInfoFields(r.tableInfo),
		Spatial: r.SpatialInfo(),
		Offset:  r.offset,
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

func (r *multiEncodedTableBatchReader) ResumeMarker() *resume.Marker {
	return nil
}

type sampleEncodedTableBatchReader struct {
	source      *encodedContentTableSource
	tableInfo   *datatype.TableInfo
	spatialInfo *datatype.SpatialInfo
	offset      int64
	done        bool
}

func (r *sampleEncodedTableBatchReader) TableInfo() *datatype.TableInfo {
	return r.tableInfo
}

func (r *sampleEncodedTableBatchReader) SpatialInfo() *datatype.SpatialInfo {
	if r.spatialInfo != nil {
		return r.spatialInfo.Clone()
	}
	return spatialInfoFromTableInfoOrFields(r.tableInfo)
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
		Rows:    rows,
		Fields:  tableInfoFields(r.tableInfo),
		Spatial: r.SpatialInfo(),
		Offset:  r.offset,
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

func (r *sampleEncodedTableBatchReader) ResumeMarker() *resume.Marker {
	return nil
}

func (s *encodedContentTableSource) contentReader() contentio.Reader {
	return contentadapter.NewMappedReader(s.reader, s.connInfo, contentadapter.FixedPathMapper(s.path), s.readOptions)
}

func (s *encodedContentTableSource) scopeReader() contentio.Reader {
	return contentadapter.NewMappedReader(s.reader, s.connInfo, contentadapter.ScopePathMapper(s.path), s.readOptions)
}

func (s *encodedContentTableSource) refReader(specs []format.RelatedRefSpec) (contentio.Reader, []format.RelatedRef) {
	if len(s.relatedRefs) > 0 {
		return contentadapter.NewReader(s.reader, s.connInfo, s.path, s.readOptions), append([]format.RelatedRef(nil), s.relatedRefs...)
	}
	return contentadapter.NewReader(s.reader, s.connInfo, s.path, s.readOptions), format.SameBasenameRelatedRefs(s.path.StringPath(), specs)
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
	resumeMarker         *resume.Marker
}

func (t *nativeTableBatchTarget) Open(ctx context.Context, tableInfo *datatype.TableInfo, spatialInfo *datatype.SpatialInfo) (TableBatchWriter, error) {
	if t.deleter != nil {
		if err := t.deleter.DeleteTarget(ctx); err != nil {
			return nil, fmt.Errorf("delete native table target before write: %w", err)
		}
	}
	if err := t.prepare(ctx, tableInfo, spatialInfo); err != nil {
		return nil, err
	}
	fields := tableInfoFields(tableInfo)
	if t.tableSessionProvider != nil && isCopyWriteMethod(t.writeOptions.Method) {
		session, err := t.tableSessionProvider.OpenTableWriteSession(ctx, t.connInfo, t.path, engineplugin.TableWriteSessionOptions{
			Method:       t.writeOptions.Method,
			Fields:       fields,
			SpatialInfo:  spatialInfo,
			ResumeMarker: cloneResumeMarker(t.resumeMarker),
		})
		if err != nil {
			return nil, fmt.Errorf("open native table write session: %w", err)
		}
		return &nativeTableSessionBatchWriter{session: session, fields: fields, spatialInfo: spatialInfo}, nil
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
		spatialInfo:  spatialInfo,
	}, nil
}

func (t *nativeTableBatchTarget) prepare(ctx context.Context, tableInfo *datatype.TableInfo, spatialInfo *datatype.SpatialInfo) error {
	if t.preparer == nil {
		return fmt.Errorf("target engine does not implement table write prepare")
	}
	opts := engineplugin.TableWriteOptions{
		Fields:      tableInfoFields(tableInfo),
		SpatialInfo: spatialInfo.Clone(),
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
	fields       []datatype.FieldInfo
	spatialInfo  *datatype.SpatialInfo
}

func (w *nativeDirectBatchWriter) WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	batch = batchWithTargetFields(batch, w.fields, w.spatialInfo)
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

func (w *nativeDirectBatchWriter) CommitMarker() *resume.Marker {
	return nil
}

func batchWithTargetFields(batch *engineplugin.BatchData, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) *engineplugin.BatchData {
	if batch == nil || (len(fields) == 0 && spatialInfo == nil) {
		return batch
	}
	copyBatch := *batch
	if len(fields) > 0 {
		copyBatch.Fields = append([]datatype.FieldInfo(nil), fields...)
	}
	copyBatch.Spatial = spatialInfo.Clone()
	return &copyBatch
}

type nativeTableSessionBatchWriter struct {
	session     engineplugin.TableWriteSession
	fields      []datatype.FieldInfo
	spatialInfo *datatype.SpatialInfo
	closed      bool
}

func (w *nativeTableSessionBatchWriter) WriteBatch(ctx context.Context, batch *engineplugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	batch = batchWithTargetFields(batch, w.fields, w.spatialInfo)
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

func (w *nativeTableSessionBatchWriter) CommitMarker() *resume.Marker {
	if markerProvider, ok := w.session.(engineplugin.CommitMarkerProvider); ok {
		return markerProvider.CommitMarker()
	}
	return nil
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
	resumeMarker   *resume.Marker
}

func (t *encodedContentTableTarget) Open(ctx context.Context, tableInfo *datatype.TableInfo, spatialInfo *datatype.SpatialInfo) (TableBatchWriter, error) {
	formatOptions := writeOptionsWithResumeMarker(writeOptionsWithSpatialInfo(t.formatOptions, tableInfo, spatialInfo), t.resumeMarker)
	if t.deleter != nil {
		if err := t.deleteExistingTarget(ctx); err != nil {
			return nil, err
		}
	}
	if tableInfoEmpty(tableInfo) && t.multiProvider == nil {
		output, err := t.contentWriter().Create(ctx, contentRefFromCatalogPath(t.path))
		if err != nil {
			return nil, fmt.Errorf("create empty encoded target content: %w", err)
		}
		return &emptyContentBatchWriter{output: output}, nil
	}
	if t.multiProvider != nil {
		writer, refs := t.refWriter(t.multiProvider.RelatedRefSpecs())
		tableWriter, err := t.multiProvider.OpenMultiTableWriter(ctx, writer, refs, tableInfo, formatOptions)
		if err != nil {
			return nil, fmt.Errorf("open encoded multi table writer: %w", err)
		}
		return &multiTableBatchWriter{tableWriter: tableWriter}, nil
	}

	output, err := t.contentWriter().Create(ctx, contentRefFromCatalogPath(t.path))
	if err != nil {
		return nil, fmt.Errorf("create encoded target content: %w", err)
	}
	tableWriter, err := t.formatProvider.OpenTableWriter(ctx, output, tableInfo, formatOptions)
	if err != nil {
		_ = output.Close()
		return nil, fmt.Errorf("open encoded target table writer: %w", err)
	}
	return &contentTableBatchWriter{output: output, tableWriter: tableWriter}, nil
}

func writeOptionsWithResumeMarker(options *format.WriteOptions, marker *resume.Marker) *format.WriteOptions {
	if marker == nil {
		return options
	}
	if options == nil {
		options = format.DefaultWriteOptions()
	}
	copied := *options
	copied.ResumeMarker = marker.Clone()
	return &copied
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
		refs:     format.SameBasenameRelatedRefs(t.path.StringPath(), t.multiProvider.RelatedRefSpecs()),
	}
	if err := multiDeleter.DeleteTarget(ctx); err != nil {
		return fmt.Errorf("delete encoded multi target before write: %w", err)
	}
	return nil
}

func (t *encodedContentTableTarget) contentWriter() contentio.Writer {
	return contentadapter.NewMappedWriter(t.writer, t.connInfo, contentadapter.FixedPathMapper(t.path), t.writeOptions)
}

func (t *encodedContentTableTarget) refWriter(specs []format.RelatedRefSpec) (contentio.Writer, []format.RelatedRef) {
	return contentadapter.NewWriter(t.writer, t.connInfo, t.path, t.writeOptions), format.SameBasenameRelatedRefs(t.path.StringPath(), specs)
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

func (w *emptyContentBatchWriter) CommitMarker() *resume.Marker {
	return nil
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

func (w *contentTableBatchWriter) CommitMarker() *resume.Marker {
	if markerProvider, ok := w.tableWriter.(format.CommitMarkerProvider); ok {
		return markerProvider.CommitMarker()
	}
	return nil
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

func (w *multiTableBatchWriter) CommitMarker() *resume.Marker {
	if markerProvider, ok := w.tableWriter.(format.CommitMarkerProvider); ok {
		return markerProvider.CommitMarker()
	}
	return nil
}
