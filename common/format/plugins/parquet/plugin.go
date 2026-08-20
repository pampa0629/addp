// Package parquet 提供 Parquet 文件格式的解析能力
// 支持 Schema 推断和样本数据读取
// 使用纯 Go 实现（github.com/parquet-go/parquet-go），无 CGO 依赖
package parquet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/resume"
	commonSpatial "github.com/addp/common/spatial"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress"
	parquetbrotli "github.com/parquet-go/parquet-go/compress/brotli"
	parquetgzip "github.com/parquet-go/parquet-go/compress/gzip"
	parquetlz4 "github.com/parquet-go/parquet-go/compress/lz4"
	parquetsnappy "github.com/parquet-go/parquet-go/compress/snappy"
	parquetuncompressed "github.com/parquet-go/parquet-go/compress/uncompressed"
	parquetzstd "github.com/parquet-go/parquet-go/compress/zstd"
	parquetfmt "github.com/parquet-go/parquet-go/format"
	"github.com/twpayne/go-geom"
)

const FileRowCountsOption = "parquet_file_row_counts"
const ParquetWriterMaxRowsPerRowGroupOption = "max_rows_per_row_group"
const ParquetWriterCompressionOption = "compression"
const parquetScopeReaderMarkerProvider = "parquet.scope_table_reader"
const parquetScopeReaderMarkerPositionUnit = "ref_row"

// Plugin 实现 Parquet 格式 plugin。
type Plugin struct{}

type Info struct {
	Files            []PartResourceInfo
	PartitionColumns []string
}

type PartResourceInfo struct {
	Path     string `json:"path"`
	RowCount int64  `json:"row_count"`
}

func (i *Info) FormatAttributes() map[string]interface{} {
	if i == nil || len(i.Files) == 0 {
		return nil
	}
	return map[string]interface{}{
		"files": i.Files,
	}
}

func InfoFromDescribeResult(result *format.TableDescribeResult) *Info {
	if result == nil {
		return nil
	}
	var native map[string]interface{}
	if result.Table != nil {
		native = result.Table.Native
	}
	return infoFromFacts(result.FormatInfo, native)
}

func infoFromFacts(formatInfo, native map[string]interface{}) *Info {
	if len(formatInfo) == 0 && len(native) == 0 {
		return nil
	}
	files := parquetResourceInfos(formatInfo["files"])
	partitionColumns := parquetPartitionColumns(native["partition_columns"])
	if len(files) == 0 && len(partitionColumns) == 0 {
		return nil
	}
	return &Info{
		Files:            files,
		PartitionColumns: partitionColumns,
	}
}

var tableNativeKeys = datatype.NewNativeAllowedKeys("partition_columns")

func parquetTableNative(partitionColumns []string) map[string]interface{} {
	if len(partitionColumns) == 0 {
		return nil
	}
	return datatype.FilterTableNative(map[string]interface{}{
		"partition_columns": append([]string(nil), partitionColumns...),
	}, tableNativeKeys)
}

func parquetResourceInfos(value interface{}) []PartResourceInfo {
	switch files := value.(type) {
	case []PartResourceInfo:
		return append([]PartResourceInfo(nil), files...)
	case []interface{}:
		result := make([]PartResourceInfo, 0, len(files))
		for _, item := range files {
			attrs, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			path := commonJSON.InterfaceString(attrs["path"])
			if path == "" {
				continue
			}
			result = append(result, PartResourceInfo{
				Path:     path,
				RowCount: commonJSON.InterfaceInt64(attrs["row_count"]),
			})
		}
		return result
	default:
		return nil
	}
}

func parquetPartitionColumns(value interface{}) []string {
	switch columns := value.(type) {
	case []string:
		return append([]string(nil), columns...)
	case []interface{}:
		result := make([]string, 0, len(columns))
		for _, item := range columns {
			column := strings.TrimSpace(commonJSON.InterfaceString(item))
			if column != "" {
				result = append(result, column)
			}
		}
		return result
	default:
		return nil
	}
}

func SampleOptionsFromAttributes(attrs map[string]interface{}) *format.ParseOptions {
	counts := FileRowCountsFromAttributes(attrs)
	if len(counts) == 0 {
		return nil
	}
	opts := format.DefaultParseOptions()
	opts.ExtraParams = map[string]interface{}{
		FileRowCountsOption: counts,
	}
	return opts
}

func FileRowCountsFromAttributes(attrs map[string]interface{}) map[string]int64 {
	parquetAttrs := commonJSON.Section(attrs, "format_info.parquet")
	if len(parquetAttrs) == 0 {
		return nil
	}
	files, ok := parquetAttrs["files"].([]PartResourceInfo)
	if ok {
		counts := make(map[string]int64, len(files))
		for _, file := range files {
			path := normalizeParquetPath(file.Path)
			if path != "" && file.RowCount >= 0 {
				counts[path] = file.RowCount
			}
		}
		return counts
	}
	return fileRowCountsFromFileList(parquetAttrs["files"])
}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) SampleOptionsFromAttributes(attrs map[string]interface{}) *format.ParseOptions {
	return SampleOptionsFromAttributes(attrs)
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register parquet format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatParquet
}

func (p *Plugin) SpatialEncodingCapability() format.SpatialEncodingCapability {
	return format.SpatialEncodingCapability{
		GeometryReadEncodings:  []format.GeometryEncoding{format.GeometryEncodingWKB, format.GeometryEncodingEWKB, format.GeometryEncodingWKT},
		GeometryWriteEncodings: []format.GeometryEncoding{format.GeometryEncodingWKB, format.GeometryEncodingEWKB},
		DefaultReadEncoding:    format.GeometryEncodingWKB,
		DefaultWriteEncoding:   format.GeometryEncodingWKB,
		NativeReadEncoding:     format.GeometryEncodingWKB,
		NativeWriteEncoding:    format.GeometryEncodingWKB,
	}
}

func (p *Plugin) CRSDefinitionWriteRequirements(spatial *datatype.SpatialInfo) ([]format.CRSDefinitionWriteRequirement, error) {
	return geoParquetCRSDefinitionWriteRequirements(spatial)
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-parquet",
		Format:   format.FormatParquet,
		I18nKey:  "format.parquet",
		DataType: datatype.Table,
		Layouts:  []string{format.LayoutSingle, format.LayoutWhole},
		Identification: format.FormatIdentification{
			Extensions:        []string{".parquet"},
			MimeTypes:         []string{"application/parquet", "application/x-parquet", "application/vnd.apache.parquet"},
			ContentSignatures: []string{"hex:50415231"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	return len(peek) >= 4 && string(peek[:4]) == "PAR1"
}

func (p *Plugin) OpenTableWriter(ctx context.Context, output io.Writer, tableInfo *datatype.TableInfo, options *format.WriteOptions) (format.TableWriter, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "parquet.table_writer"); err != nil {
			return nil, err
		}
	}
	if output == nil {
		return nil, fmt.Errorf("parquet table writer requires output")
	}
	fields := parquetWriterFields(tableInfo)
	if len(fields) == 0 {
		return nil, fmt.Errorf("parquet table writer requires table fields")
	}
	geoMetadata, err := geoParquetWriteMetadata(fields, options)
	if err != nil {
		return nil, err
	}

	group := parquetgo.Group{}
	for _, field := range fields {
		group[field.Name] = parquetNodeForField(field)
	}
	schema := parquetgo.NewSchema("", group)
	writerOptionSet, err := parquetWriterOptions(options)
	if err != nil {
		return nil, err
	}
	writerOptions := append([]parquetgo.WriterOption{schema}, writerOptionSet.options...)
	writer := parquetgo.NewGenericWriter[any](output, writerOptions...)
	if geoMetadata != "" {
		writer.SetKeyValueMetadata(geoParquetMetadataKey, geoMetadata)
	}
	return &tableWriter{
		writer:          writer,
		schema:          schema,
		fields:          fields,
		geometryColumns: geoParquetWriteColumns(options),
		maxRowsPerWrite: writerOptionSet.maxRowsPerRowGroup,
	}, nil
}

func (p *Plugin) OpenTableReader(ctx context.Context, input io.Reader, options *format.ParseOptions) (format.TableReader, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "parquet.table_reader"); err != nil {
			return nil, err
		}
	}
	if input == nil {
		return nil, fmt.Errorf("parquet table reader requires input")
	}
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet data: %w", err)
	}
	file, err := parquetgo.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file: %w", err)
	}
	return p.openTableReaderFromFile(ctx, file, options)
}

func (p *Plugin) openTableReaderFromContent(ctx context.Context, reader contentio.Reader, ref contentio.Ref, options *format.ParseOptions) (format.TableReader, io.Closer, error) {
	if err := contextErr(ctx); err != nil {
		return nil, nil, err
	}
	if rangeReader, ok := reader.(contentio.RangeReader); ok {
		stat, err := reader.Stat(ctx, ref)
		if err == nil && stat != nil && stat.Exists && stat.Size > 0 {
			file, openErr := parquetgo.OpenFile(rangeContentReaderAt{
				ctx:    ctx,
				reader: rangeReader,
				ref:    ref,
			}, stat.Size)
			if openErr != nil {
				return nil, nil, fmt.Errorf("failed to open parquet file with range reader: %w", openErr)
			}
			tableReader, tableErr := p.openTableReaderFromFile(ctx, file, options)
			return tableReader, nil, tableErr
		}
	}
	input, err := reader.Open(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	tableReader, err := p.OpenTableReader(ctx, input, options)
	if err != nil {
		_ = input.Close()
		return nil, nil, err
	}
	return tableReader, input, nil
}

func (p *Plugin) openTableReaderFromFile(ctx context.Context, file *parquetgo.File, options *format.ParseOptions) (format.TableReader, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	rowCount := file.NumRows()
	sourceFields := extractFields(file.Schema())
	geoInfo, err := parseGeoParquetFile(file, sourceFields)
	if err != nil {
		return nil, err
	}
	geometryEncoding, err := geoParquetReadEncoding(geoInfo, options)
	if err != nil {
		return nil, err
	}
	sourceFields = applyGeoParquetFieldTypes(sourceFields, geoInfo)
	describeResult := &format.TableDescribeResult{Table: &datatype.TableInfo{
		Fields:   sourceFields,
		RowCount: &rowCount,
	}, Spatial: geoInfoSpatial(geoInfo)}
	describeResult, err = format.ApplyFieldSelectionToTableDescribeResult(describeResult, fieldSelectionFromOptions(options))
	if err != nil {
		return nil, err
	}
	tableInfo := describeResult.Table
	projectionSchema, projectionFieldNames, err := parquetProjectionForFieldSelection(file.Schema(), tableInfo.Fields, sourceFields, fieldSelectionFromOptions(options))
	if err != nil {
		return nil, err
	}
	var projectionConversion parquetgo.Conversion
	if projectionSchema != nil {
		projectionConversion, err = parquetgo.Convert(projectionSchema, file.Schema())
		if err != nil {
			return nil, fmt.Errorf("failed to create parquet field projection: %w", err)
		}
	}
	fieldNames := extractLeafColumnNames(file.Schema())
	if projectionSchema != nil {
		fieldNames = projectionFieldNames
	}
	return &tableReader{
		file:                 file,
		fieldNames:           fieldNames,
		tableInfo:            tableInfo,
		spatialInfo:          describeResult.Spatial,
		geoFormatInfo:        geoParquetFormatInfo(geoInfo),
		geometryFields:       geoParquetGeometryFieldSet(geoInfo, tableInfo.Fields),
		geometrySRIDs:        geoParquetGeometrySRIDs(describeResult.Spatial),
		geometryEncoding:     geometryEncoding,
		fieldSelection:       fieldSelectionFromOptions(options),
		projectionSchema:     projectionSchema,
		projectionConversion: projectionConversion,
	}, nil
}

type rangeContentReaderAt struct {
	ctx    context.Context
	reader contentio.RangeReader
	ref    contentio.Ref
}

func (r rangeContentReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, fmt.Errorf("parquet range read offset cannot be negative")
	}
	rc, err := r.reader.OpenRange(r.ctx, r.ref, off, int64(len(p)))
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	n, readErr := io.ReadFull(rc, p)
	if readErr == io.ErrUnexpectedEOF {
		return n, io.EOF
	}
	return n, readErr
}

// DescribeTable 从 Parquet 文件中提取 TableInfo 和行数。
func (p *Plugin) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableDescribeResult, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet data: %w", err)
	}

	file, err := parquetgo.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file: %w", err)
	}

	fields := extractFields(file.Schema())
	geoInfo, err := parseGeoParquetFile(file, fields)
	if err != nil {
		return nil, err
	}
	fields = applyGeoParquetFieldTypes(fields, geoInfo)
	rowCount := file.NumRows()

	result := &format.TableDescribeResult{
		Table: &datatype.TableInfo{
			Fields:   fields,
			RowCount: &rowCount,
		},
		Spatial:    geoInfoSpatial(geoInfo),
		FormatInfo: geoParquetFormatAttributes(geoInfo),
	}
	selected, err := format.ApplyFieldSelectionToTableDescribeResult(result, fieldSelectionFromOptions(options))
	if err != nil {
		return nil, err
	}
	return selected, nil
}

// SampleTable 读取 Parquet 表格样本。
func (p *Plugin) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet data: %w", err)
	}

	file, err := parquetgo.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file: %w", err)
	}

	fields := extractFields(file.Schema())
	geoInfo, err := parseGeoParquetFile(file, fields)
	if err != nil {
		return nil, err
	}
	fields = applyGeoParquetFieldTypes(fields, geoInfo)
	geometryEncoding, err := geoParquetReadEncoding(geoInfo, options)
	if err != nil {
		return nil, err
	}

	// 提取列名（叶子列顺序）
	fieldNames := extractLeafColumnNames(file.Schema())
	fieldSelection := fieldSelectionFromOptions(options)
	if _, err := format.ApplyFieldSelectionToTableInfo(&datatype.TableInfo{
		Fields: fields,
	}, fieldSelection); err != nil {
		return nil, err
	}
	geometryFields := geoParquetGeometryFieldSet(geoInfo, fields)
	geometrySRIDs := geoParquetGeometrySRIDs(geoInfoSpatial(geoInfo))

	if limit <= 0 {
		limit = 100
	}

	result := make([]map[string]interface{}, 0, limit)
	remainingOffset := offset
	if remainingOffset < 0 {
		remainingOffset = 0
	}

	for _, rg := range file.RowGroups() {
		if int64(len(result)) >= limit {
			break
		}
		if remainingOffset >= rg.NumRows() {
			remainingOffset -= rg.NumRows()
			continue
		}

		rows := rg.Rows()
		// 使用 parquet row seeker 直接定位到 row group 内部行号，避免深分页逐行跳过。
		if remainingOffset > 0 {
			if err := rows.SeekToRow(remainingOffset); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to seek parquet row %d: %w", remainingOffset, err)
			}
			remainingOffset = 0
		}

		readErr := appendRows(ctx, rows, fieldNames, geometryFields, geometrySRIDs, geometryEncoding, limit, &result)
		closeErr := rows.Close()
		if readErr != nil {
			return result, readErr
		}
		if closeErr != nil {
			return result, fmt.Errorf("failed to close parquet rows: %w", closeErr)
		}
	}

	return format.ApplyFieldSelectionToRows(result, fieldSelection), nil
}

func appendRows(ctx context.Context, rows parquetgo.Rows, fieldNames []string, geometryFields map[string]bool, geometrySRIDs map[string]int, geometryEncoding format.GeometryEncoding, limit int64, result *[]map[string]interface{}) error {
	const maxBatchSize = 128
	for int64(len(*result)) < limit {
		if err := contextErr(ctx); err != nil {
			return err
		}
		remaining := limit - int64(len(*result))
		batchSize := int64(maxBatchSize)
		if remaining < batchSize {
			batchSize = remaining
		}
		if batchSize <= 0 || batchSize > math.MaxInt {
			break
		}
		buf := make([]parquetgo.Row, int(batchSize))
		n, readErr := rows.ReadRows(buf)
		if n > 0 {
			for _, parquetRow := range buf[:n] {
				row := make(map[string]interface{}, len(parquetRow))
				for j, val := range parquetRow {
					if j < len(fieldNames) {
						name := fieldNames[j]
						value, err := valueToInterface(val, geometryFields[name], geometrySRIDs[name], geometryEncoding)
						if err != nil {
							return fmt.Errorf("decode parquet field %q: %w", name, err)
						}
						row[name] = value
					}
				}
				*result = append(*result, row)
				if int64(len(*result)) >= limit {
					break
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return fmt.Errorf("failed to read row: %w", readErr)
			}
			if n == 0 {
				break
			}
			break
		}
		if n == 0 {
			break
		}
	}
	return nil
}

type tableReader struct {
	file                 *parquetgo.File
	fieldNames           []string
	tableInfo            *datatype.TableInfo
	spatialInfo          *datatype.SpatialInfo
	geoFormatInfo        map[string]interface{}
	geometryFields       map[string]bool
	geometrySRIDs        map[string]int
	geometryEncoding     format.GeometryEncoding
	fieldSelection       *format.FieldSelectionOptions
	projectionSchema     *parquetgo.Schema
	projectionConversion parquetgo.Conversion
	rowGroupIndex        int
	rows                 parquetgo.Rows
	closed               bool
}

func (r *tableReader) Fields() []datatype.FieldInfo {
	if r == nil || r.tableInfo == nil {
		return nil
	}
	return append([]datatype.FieldInfo(nil), r.tableInfo.Fields...)
}

func (r *tableReader) SpatialInfo() *datatype.SpatialInfo {
	if r == nil {
		return nil
	}
	return r.spatialInfo.Clone()
}

func (r *tableReader) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if r.closed {
		return nil, fmt.Errorf("parquet table reader is closed")
	}
	if limit < 0 {
		return nil, fmt.Errorf("parquet table reader limit cannot be negative")
	}
	if limit == 0 {
		limit = 1
	}
	result := make([]map[string]interface{}, 0, limit)
	for len(result) < limit {
		if err := contextErr(ctx); err != nil {
			return result, err
		}
		if r.rows == nil {
			if !r.openNextRowGroup() {
				break
			}
		}
		before := len(result)
		if err := appendRows(ctx, r.rows, r.fieldNames, r.geometryFields, r.geometrySRIDs, r.geometryEncoding, int64(limit), &result); err != nil {
			return result, err
		}
		if len(result) >= limit {
			break
		}
		if len(result) == before {
			if err := r.closeCurrentRows(); err != nil {
				return result, err
			}
		}
	}
	return format.ApplyFieldSelectionToRows(result, r.fieldSelection), nil
}

func (r *tableReader) Close(ctx context.Context) error {
	if r.closed {
		return nil
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.closed = true
	return r.closeCurrentRows()
}

func (r *tableReader) openNextRowGroup() bool {
	if r.file == nil || r.rowGroupIndex >= len(r.file.RowGroups()) {
		return false
	}
	rowGroup := r.file.RowGroups()[r.rowGroupIndex]
	if r.projectionConversion != nil {
		rowGroup = parquetgo.ConvertRowGroup(rowGroup, r.projectionConversion)
	}
	r.rows = rowGroup.Rows()
	r.rowGroupIndex++
	return true
}

func (r *tableReader) closeCurrentRows() error {
	if r.rows == nil {
		return nil
	}
	err := r.rows.Close()
	r.rows = nil
	if err != nil {
		return fmt.Errorf("failed to close parquet rows: %w", err)
	}
	return nil
}

type tableWriter struct {
	writer          *parquetgo.GenericWriter[any]
	schema          *parquetgo.Schema
	fields          []datatype.FieldInfo
	geometryColumns map[string]datatype.GeometryColumnInfo
	maxRowsPerWrite int64
	closed          bool
}

func (w *tableWriter) WriteRows(ctx context.Context, rows []map[string]interface{}) error {
	if w.closed {
		return fmt.Errorf("parquet table writer is closed")
	}
	if len(rows) == 0 {
		return nil
	}
	parquetRows := make([]parquetgo.Row, 0, len(rows))
	for rowIndex, row := range rows {
		if err := contextErr(ctx); err != nil {
			return err
		}
		writerRow, err := parquetWriterRow(row, w.fields, w.geometryColumns)
		if err != nil {
			return fmt.Errorf("convert parquet row %d: %w", rowIndex, err)
		}
		parquetRows = append(parquetRows, w.schema.Deconstruct(nil, writerRow))
	}
	for start := 0; start < len(parquetRows); {
		end := len(parquetRows)
		if w.maxRowsPerWrite > 0 {
			chunkEnd := int64(start) + w.maxRowsPerWrite
			if chunkEnd < int64(end) {
				end = int(chunkEnd)
			}
		}
		if _, err := w.writer.WriteRows(parquetRows[start:end]); err != nil {
			return fmt.Errorf("failed to write parquet rows: %w", err)
		}
		start = end
	}
	return nil
}

func (w *tableWriter) Close(ctx context.Context) error {
	if w.closed {
		return nil
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	w.closed = true
	if err := w.writer.Close(); err != nil {
		return fmt.Errorf("failed to close parquet writer: %w", err)
	}
	return nil
}

func parquetWriterFields(tableInfo *datatype.TableInfo) []datatype.FieldInfo {
	if tableInfo == nil {
		return nil
	}
	fields := make([]datatype.FieldInfo, 0, len(tableInfo.Fields))
	seen := map[string]struct{}{}
	for _, field := range tableInfo.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		field.Name = name
		fields = append(fields, field)
	}
	return fields
}

type parquetWriterOptionSet struct {
	options            []parquetgo.WriterOption
	maxRowsPerRowGroup int64
}

func parquetWriterOptions(options *format.WriteOptions) (parquetWriterOptionSet, error) {
	if options == nil || len(options.ExtraParams) == 0 {
		return parquetWriterOptionSet{}, nil
	}
	optionSet := parquetWriterOptionSet{}
	writerOptions := make([]parquetgo.WriterOption, 0, 2)
	if value, ok := options.ExtraParams[ParquetWriterMaxRowsPerRowGroupOption]; ok {
		rows := commonJSON.InterfaceInt64(value)
		if rows <= 0 {
			return parquetWriterOptionSet{}, fmt.Errorf("parquet %s must be positive", ParquetWriterMaxRowsPerRowGroupOption)
		}
		writerOptions = append(writerOptions, parquetgo.MaxRowsPerRowGroup(rows))
		optionSet.maxRowsPerRowGroup = rows
	}
	if value, ok := options.ExtraParams[ParquetWriterCompressionOption]; ok {
		codecName := strings.ToLower(strings.TrimSpace(commonJSON.InterfaceString(value)))
		if codecName == "" {
			return parquetWriterOptionSet{}, fmt.Errorf("parquet %s must not be empty", ParquetWriterCompressionOption)
		}
		codec, err := parquetCompressionCodec(codecName)
		if err != nil {
			return parquetWriterOptionSet{}, err
		}
		writerOptions = append(writerOptions, parquetgo.Compression(codec))
	}
	optionSet.options = writerOptions
	return optionSet, nil
}

func parquetCompressionCodec(name string) (compress.Codec, error) {
	switch strings.ReplaceAll(name, "-", "_") {
	case "none", "uncompressed":
		return &parquetuncompressed.Codec{}, nil
	case "snappy":
		return &parquetsnappy.Codec{}, nil
	case "gzip":
		return &parquetgzip.Codec{}, nil
	case "zstd":
		return &parquetzstd.Codec{}, nil
	case "lz4", "lz4_raw":
		return &parquetlz4.Codec{}, nil
	case "brotli":
		return &parquetbrotli.Codec{}, nil
	default:
		return nil, fmt.Errorf("unsupported parquet compression %q", name)
	}
}

func parquetNodeForField(field datatype.FieldInfo) parquetgo.Node {
	var node parquetgo.Node
	switch field.Type {
	case datatype.FieldTypeBool:
		node = parquetgo.Leaf(parquetgo.BooleanType)
	case datatype.FieldTypeInt:
		node = parquetgo.Int(32)
	case datatype.FieldTypeBigInt:
		node = parquetgo.Int(64)
	case datatype.FieldTypeFloat:
		node = parquetgo.Leaf(parquetgo.FloatType)
	case datatype.FieldTypeDouble, datatype.FieldTypeDecimal:
		node = parquetgo.Leaf(parquetgo.DoubleType)
	case datatype.FieldTypeBytes:
		node = parquetgo.Leaf(parquetgo.ByteArrayType)
	case datatype.FieldTypeGeometry:
		node = parquetgo.Leaf(parquetgo.ByteArrayType)
	case datatype.FieldTypeDate:
		node = parquetgo.Date()
	case datatype.FieldTypeTimestamp, datatype.FieldTypeTime:
		node = parquetgo.String()
	case datatype.FieldTypeJSON, datatype.FieldTypeArray:
		node = parquetgo.String()
	case datatype.FieldTypeUUID:
		node = parquetgo.UUID()
	default:
		node = parquetgo.String()
	}
	if field.Nullable {
		return parquetgo.Optional(node)
	}
	return node
}

func parquetWriterRow(row map[string]interface{}, fields []datatype.FieldInfo, geometryColumns map[string]datatype.GeometryColumnInfo) (map[string]any, error) {
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		geometryColumn, _ := geometryColumns[field.Name]
		value, err := parquetWriterValue(row[field.Name], field.Type, geometryColumn)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", field.Name, err)
		}
		out[field.Name] = value
	}
	return out, nil
}

func parquetWriterValue(value interface{}, fieldType datatype.FieldType, geometryColumn datatype.GeometryColumnInfo) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch fieldType {
	case datatype.FieldTypeBool:
		return boolValue(value), nil
	case datatype.FieldTypeInt:
		return int32(int64Value(value)), nil
	case datatype.FieldTypeBigInt:
		return int64Value(value), nil
	case datatype.FieldTypeFloat:
		return float32(float64Value(value)), nil
	case datatype.FieldTypeDouble, datatype.FieldTypeDecimal:
		return float64Value(value), nil
	case datatype.FieldTypeBytes:
		if bytes, ok := value.([]byte); ok {
			return bytes, nil
		}
		return []byte(fmt.Sprint(value)), nil
	case datatype.FieldTypeGeometry:
		data, ok := value.([]byte)
		if !ok {
			return nil, fmt.Errorf("geometry value must be WKB or EWKB []byte, got %T", value)
		}
		geometry, err := commonSpatial.ParseGeometryBytes(data)
		if err != nil {
			return nil, fmt.Errorf("decode WKB/EWKB geometry: %w", err)
		}
		if err := validateGeoParquetWriteGeometry(geometry, geometryColumn); err != nil {
			return nil, err
		}
		wkb, err := commonSpatial.GeomToWKB(geometry)
		if err != nil {
			return nil, fmt.Errorf("encode standard WKB geometry: %w", err)
		}
		return wkb, nil
	case datatype.FieldTypeDate:
		return dateValue(value), nil
	case datatype.FieldTypeTimestamp, datatype.FieldTypeTime:
		return temporalString(value), nil
	case datatype.FieldTypeJSON, datatype.FieldTypeArray:
		return jsonString(value), nil
	default:
		return fmt.Sprint(value), nil
	}
}

func validateGeoParquetWriteGeometry(geometry geom.T, column datatype.GeometryColumnInfo) error {
	switch geometry.Layout() {
	case geom.XY, geom.XYZ:
	case geom.XYM, geom.XYZM:
		return fmt.Errorf("GeoParquet 1.1 WKB does not support measured coordinates with layout %s", geometry.Layout())
	default:
		return fmt.Errorf("GeoParquet 1.1 WKB requires XY or XYZ geometry layout, got %s", geometry.Layout())
	}
	if column.Dimension == nil || *column.Dimension == 0 {
		return nil
	}
	wantLayout := geom.XY
	if *column.Dimension == 3 {
		wantLayout = geom.XYZ
	}
	if geometry.Layout() != wantLayout {
		return fmt.Errorf("geometry layout %s does not match declared dimension %d", geometry.Layout(), *column.Dimension)
	}
	expectedType := datatype.ParseGeometryType(column.GeometryType)
	if expectedType == datatype.GeometryTypeUnknown || expectedType == datatype.GeometryTypeGeometry {
		return nil
	}
	actualType := datatype.ParseGeometryType(commonSpatial.GeometryTypeName(geometry))
	if actualType != expectedType {
		return fmt.Errorf("geometry type %s does not match declared type %s", actualType, expectedType)
	}
	return nil
}

func boolValue(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return fmt.Sprint(value) == "true"
	}
}

func int64Value(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int8:
		return int64(typed)
	case int16:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case uint:
		return int64(typed)
	case uint8:
		return int64(typed)
	case uint16:
		return int64(typed)
	case uint32:
		return int64(typed)
	case uint64:
		if typed > math.MaxInt64 {
			return math.MaxInt64
		}
		return int64(typed)
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		return parsed
	}
}

func float64Value(value interface{}) float64 {
	switch typed := value.(type) {
	case float32:
		return float64(typed)
	case float64:
		return typed
	case int:
		return float64(typed)
	case int8:
		return float64(typed)
	case int16:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint:
		return float64(typed)
	case uint8:
		return float64(typed)
	case uint16:
		return float64(typed)
	case uint32:
		return float64(typed)
	case uint64:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	default:
		parsed, _ := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed
	}
}

func dateValue(value interface{}) any {
	switch typed := value.(type) {
	case time.Time:
		return typed
	case string:
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
		return typed
	default:
		return value
	}
}

func temporalString(value interface{}) string {
	if typed, ok := value.(time.Time); ok {
		return typed.Format(time.RFC3339Nano)
	}
	return fmt.Sprint(value)
}

func jsonString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(data)
	}
}

func (p *Plugin) DescribeTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *format.ParseOptions) (*format.TableDescribeResult, error) {
	scopedRefs, err := listParquetScopeResources(ctx, reader, scope)
	if err != nil {
		return nil, err
	}
	var merged *format.TableDescribeResult
	var dataFields []datatype.FieldInfo
	totalRows := int64(0)
	files := make([]PartResourceInfo, 0, len(scopedRefs))
	partitionFields := partitionFieldsFromScopedRefs(scopedRefs)
	var scopeGeoInfo *geoParquetInfo
	var scopeSpatial *datatype.SpatialInfo
	for _, scopedRef := range scopedRefs {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		ref := scopedRef.Ref
		input, err := reader.Open(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("failed to open parquet file %s: %w", ref.Path, err)
		}
		result, describeErr := p.DescribeTable(ctx, input, parseOptionsWithoutFieldSelection(options))
		closeErr := input.Close()
		if describeErr != nil {
			return nil, fmt.Errorf("failed to describe parquet file %s: %w", ref.Path, describeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("failed to close parquet file %s: %w", ref.Path, closeErr)
		}
		info := result.Table
		if merged == nil {
			dataFields = append([]datatype.FieldInfo(nil), info.Fields...)
			scopeSpatial = result.Spatial.Clone()
			baseInfo := &format.TableDescribeResult{
				Table: &datatype.TableInfo{
					Name:       contentio.BaseName(scope),
					Fields:     appendPartitionFields(info.Fields, partitionFields),
					PrimaryKey: append([]string(nil), info.PrimaryKey...),
					Native:     parquetTableNative(fieldNames(partitionFields)),
				},
				Spatial: scopeSpatial.Clone(),
			}
			if geoAttrs := commonJSON.Section(result.FormatInfo, "geo"); len(geoAttrs) > 0 {
				scopeGeoInfo = &geoParquetInfo{formatInfo: geoAttrs}
			}
			merged, err = format.ApplyFieldSelectionToTableDescribeResult(baseInfo, fieldSelectionFromOptions(options))
			if err != nil {
				return nil, err
			}
		} else {
			if !sameFieldInfoList(dataFields, info.Fields) {
				return nil, fmt.Errorf("parquet scope %s has incompatible table fields in %s", scope.Path, ref.Path)
			}
			currentGeoAttrs := commonJSON.Section(result.FormatInfo, "geo")
			if !sameGeoParquetSpatialSchema(scopeSpatial, result.Spatial) || !sameGeoParquetFormatSchema(geoParquetFormatInfo(scopeGeoInfo), currentGeoAttrs) {
				return nil, fmt.Errorf("parquet scope %s has incompatible geoparquet spatial metadata in %s", scope.Path, ref.Path)
			}
			mergeGeoParquetExtent(scopeSpatial, result.Spatial)
		}
		if info.RowCount != nil {
			totalRows += *info.RowCount
			files = append(files, PartResourceInfo{
				Path:     normalizeParquetPath(ref.Path),
				RowCount: *info.RowCount,
			})
		}
	}
	if merged == nil {
		return nil, fmt.Errorf("parquet scope %s has no parquet files", scope.Path)
	}
	merged.Table.RowCount = &totalRows
	if merged.Spatial != nil {
		merged.Spatial = scopeSpatial.Clone()
	}
	merged.FormatInfo = (&Info{Files: files}).FormatAttributes()
	if geoAttrs := geoParquetScopeFormatAttributes(scopeGeoInfo, scopeSpatial); len(geoAttrs) > 0 {
		merged.FormatInfo["geo"] = geoAttrs
	}
	return merged, nil
}

func (p *Plugin) SampleTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	scopedRefs, err := listParquetScopeResources(ctx, reader, scope)
	if err != nil {
		return nil, err
	}
	fileRowCounts := parquetFileRowCountsFromOptions(options)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}
	rows := make([]map[string]interface{}, 0, limit)
	remainingOffset := offset
	for _, scopedRef := range scopedRefs {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		if int64(len(rows)) >= limit {
			break
		}
		ref := scopedRef.Ref
		fileRows := int64(0)
		if rowCount, ok := fileRowCounts[normalizeParquetPath(ref.Path)]; ok {
			fileRows = rowCount
		} else {
			input, err := reader.Open(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("failed to open parquet file %s: %w", ref.Path, err)
			}
			result, describeErr := p.DescribeTable(ctx, input, parseOptionsWithoutFieldSelection(options))
			closeErr := input.Close()
			if describeErr != nil {
				return nil, fmt.Errorf("failed to describe parquet file %s: %w", ref.Path, describeErr)
			}
			if closeErr != nil {
				return nil, fmt.Errorf("failed to close parquet file %s: %w", ref.Path, closeErr)
			}
			info := result.Table
			if info.RowCount != nil {
				fileRows = *info.RowCount
			}
		}
		if remainingOffset >= fileRows {
			remainingOffset -= fileRows
			continue
		}
		input, err := reader.Open(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("failed to open parquet file %s: %w", ref.Path, err)
		}
		limitForFile := limit - int64(len(rows))
		partRows, sampleErr := p.SampleTable(ctx, input, remainingOffset, limitForFile, parseOptionsWithoutFieldSelection(options))
		closeErr := input.Close()
		if sampleErr != nil {
			return nil, fmt.Errorf("failed to sample parquet file %s: %w", ref.Path, sampleErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("failed to close parquet file %s: %w", ref.Path, closeErr)
		}
		rows = append(rows, format.ApplyFieldSelectionToRows(withPartitionValues(partRows, scopedRef.Partitions), fieldSelectionFromOptions(options))...)
		remainingOffset = 0
	}
	return rows, nil
}

func (p *Plugin) OpenTableScopeReader(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *format.ParseOptions) (format.TableReader, error) {
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "parquet.scope_table_reader"); err != nil {
			return nil, err
		}
	}
	scopedRefs, err := listParquetScopeResources(ctx, reader, scope)
	if err != nil {
		return nil, err
	}
	if len(scopedRefs) == 0 {
		return nil, fmt.Errorf("parquet scope %s has no parquet files", scope.Path)
	}
	result := &scopeTableReader{
		plugin:          p,
		reader:          reader,
		refs:            scopedRefs,
		partitionFields: partitionFieldsFromScopedRefs(scopedRefs),
		parseOptions:    options,
		fieldSelection:  fieldSelectionFromOptions(options),
	}
	if err := result.openNext(ctx); err != nil {
		_ = result.closeCurrent(context.Background())
		return nil, err
	}
	return result, nil
}

type scopeTableReader struct {
	plugin             *Plugin
	reader             contentio.Reader
	refs               []scopedParquetRef
	partitionFields    []datatype.FieldInfo
	parseOptions       *format.ParseOptions
	fieldSelection     *format.FieldSelectionOptions
	tableInfo          *datatype.TableInfo
	spatialInfo        *datatype.SpatialInfo
	spatialComplete    bool
	geoFormatInfo      map[string]interface{}
	dataFields         []datatype.FieldInfo
	index              int
	currentInput       io.Closer
	current            format.TableReader
	currentPartitions  []partitionValue
	currentRefPath     string
	currentRefIndex    int
	currentRefRowsRead int64
	totalRowsRead      int64
	lastMarker         *resume.Marker
	closed             bool
}

func (r *scopeTableReader) Fields() []datatype.FieldInfo {
	if r == nil || r.tableInfo == nil {
		return nil
	}
	return append([]datatype.FieldInfo(nil), r.tableInfo.Fields...)
}

func (r *scopeTableReader) SpatialInfo() *datatype.SpatialInfo {
	if r == nil || !r.spatialComplete {
		return nil
	}
	return r.spatialInfo.Clone()
}

func (r *scopeTableReader) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if r.closed {
		return nil, fmt.Errorf("parquet scope table reader is closed")
	}
	if limit < 0 {
		return nil, fmt.Errorf("parquet scope table reader limit cannot be negative")
	}
	if limit == 0 {
		limit = 1
	}
	result := make([]map[string]interface{}, 0, limit)
	for len(result) < limit {
		if err := contextErr(ctx); err != nil {
			return result, err
		}
		if r.current == nil {
			if err := r.openNext(ctx); err != nil {
				return result, err
			}
			if r.current == nil {
				break
			}
		}
		rows, err := r.current.ReadRows(ctx, limit-len(result))
		if err != nil {
			return result, err
		}
		result = append(result, format.ApplyFieldSelectionToRows(withPartitionValues(rows, r.currentPartitions), r.fieldSelection)...)
		if len(rows) > 0 {
			r.updateResumeMarker(len(rows))
		}
		if len(rows) == 0 {
			if err := r.closeCurrent(ctx); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func (r *scopeTableReader) ResumeMarker() *resume.Marker {
	if r == nil {
		return nil
	}
	return r.lastMarker.Clone()
}

func (r *scopeTableReader) Close(ctx context.Context) error {
	if r.closed {
		return nil
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.closed = true
	return r.closeCurrent(ctx)
}

func (r *scopeTableReader) openNext(ctx context.Context) error {
	for r.index < len(r.refs) {
		refIndex := r.index
		scopedRef := r.refs[refIndex]
		r.index++
		ref := scopedRef.Ref
		partReader, closer, err := r.plugin.openTableReaderFromContent(ctx, r.reader, ref, parquetDataFieldParseOptions(r.parseOptions, scopedRef.Partitions))
		if err != nil {
			return fmt.Errorf("failed to open parquet table reader for %s: %w", ref.Path, err)
		}
		tableInfo := &datatype.TableInfo{Fields: partReader.Fields()}
		var spatialInfo *datatype.SpatialInfo
		if provider, ok := partReader.(format.TableSpatialInfoProvider); ok {
			spatialInfo = provider.SpatialInfo()
		}
		var geoFormatInfo map[string]interface{}
		if parquetReader, ok := partReader.(*tableReader); ok {
			geoFormatInfo = parquetReader.geoFormatInfo
		}
		if r.tableInfo == nil {
			r.dataFields = append([]datatype.FieldInfo(nil), tableInfo.Fields...)
			r.spatialInfo = spatialInfo.Clone()
			r.geoFormatInfo = geoFormatInfo
			r.tableInfo, err = format.ApplyFieldSelectionToTableInfo(copyTableInfoWithPartitionFields(tableInfo, r.partitionFields), r.fieldSelection)
			if err != nil {
				_ = partReader.Close(ctx)
				if closer != nil {
					_ = closer.Close()
				}
				return err
			}
		} else {
			if tableInfo != nil && !sameFieldInfoList(r.dataFields, tableInfo.Fields) {
				_ = partReader.Close(ctx)
				if closer != nil {
					_ = closer.Close()
				}
				return fmt.Errorf("parquet scope has incompatible table fields in %s", ref.Path)
			}
			if !sameGeoParquetSpatialSchema(r.spatialInfo, spatialInfo) || !sameGeoParquetFormatSchema(r.geoFormatInfo, geoFormatInfo) {
				_ = partReader.Close(ctx)
				if closer != nil {
					_ = closer.Close()
				}
				return fmt.Errorf("parquet scope has incompatible geoparquet spatial metadata in %s", ref.Path)
			}
			mergeGeoParquetExtent(r.spatialInfo, spatialInfo)
		}
		r.currentInput = closer
		r.current = partReader
		r.currentPartitions = scopedRef.Partitions
		r.currentRefPath = ref.Path
		r.currentRefIndex = refIndex
		r.currentRefRowsRead = 0
		r.spatialComplete = r.index == len(r.refs)
		return nil
	}
	return nil
}

func (r *scopeTableReader) closeCurrent(ctx context.Context) error {
	var firstErr error
	if r.current != nil {
		firstErr = r.current.Close(ctx)
		r.current = nil
	}
	if r.currentInput != nil {
		if err := r.currentInput.Close(); firstErr == nil && err != nil {
			firstErr = err
		}
		r.currentInput = nil
	}
	r.currentPartitions = nil
	r.currentRefPath = ""
	r.currentRefIndex = 0
	r.currentRefRowsRead = 0
	return firstErr
}

func (r *scopeTableReader) updateResumeMarker(rowsRead int) {
	if rowsRead <= 0 {
		return
	}
	r.currentRefRowsRead += int64(rowsRead)
	r.totalRowsRead += int64(rowsRead)
	r.lastMarker = &resume.Marker{
		Version:      resume.MarkerVersionV1,
		Provider:     parquetScopeReaderMarkerProvider,
		PositionUnit: parquetScopeReaderMarkerPositionUnit,
		ReadPosition: map[string]interface{}{
			"ref":        r.currentRefPath,
			"ref_index":  r.currentRefIndex,
			"row_offset": r.currentRefRowsRead,
			"rows_read":  r.totalRowsRead,
		},
		Fingerprint: map[string]interface{}{
			"ref_count": len(r.refs),
			"refs":      parquetScopeReaderRefPaths(r.refs),
		},
	}
}

func parquetScopeReaderRefPaths(refs []scopedParquetRef) []string {
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		paths = append(paths, ref.Ref.Path)
	}
	return paths
}

func parquetFileRowCountsFromOptions(options *format.ParseOptions) map[string]int64 {
	if options == nil || options.ExtraParams == nil {
		return nil
	}
	value := options.ExtraParams[FileRowCountsOption]
	return fileRowCountsFromPathMap(value)
}

func fileRowCountsFromPathMap(value interface{}) map[string]int64 {
	switch typed := value.(type) {
	case map[string]int64:
		counts := make(map[string]int64, len(typed))
		for path, count := range typed {
			counts[normalizeParquetPath(path)] = count
		}
		return counts
	case map[string]interface{}:
		counts := make(map[string]int64, len(typed))
		for path, raw := range typed {
			if count := interfaceInt64(raw); count >= 0 {
				counts[normalizeParquetPath(path)] = count
			}
		}
		return counts
	default:
		return nil
	}
}

func fileRowCountsFromFileList(value interface{}) map[string]int64 {
	values, ok := value.([]interface{})
	if !ok {
		return nil
	}
	counts := make(map[string]int64, len(values))
	for _, item := range values {
		fileAttrs, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		path := normalizeParquetPath(commonJSON.InterfaceString(fileAttrs["path"]))
		rowCount := commonJSON.InterfaceInt64(fileAttrs["row_count"])
		if path == "" || rowCount < 0 {
			continue
		}
		counts[path] = rowCount
	}
	return counts
}

func interfaceInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case uint:
		return int64(typed)
	case uint64:
		if typed > math.MaxInt64 {
			return -1
		}
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return -1
	}
}

func normalizeParquetPath(path string) string {
	return strings.Trim(path, "/")
}

func fieldSelectionFromOptions(options *format.ParseOptions) *format.FieldSelectionOptions {
	if options == nil {
		return nil
	}
	return options.FieldSelection
}

func parquetProjectionForFieldSelection(schema *parquetgo.Schema, selectedFields, sourceFields []datatype.FieldInfo, selection *format.FieldSelectionOptions) (*parquetgo.Schema, []string, error) {
	if schema == nil || selection == nil || len(selection.Include) == 0 {
		return nil, nil, nil
	}
	if len(selectedFields) == 0 || len(selectedFields) >= len(sourceFields) {
		return nil, nil, nil
	}
	group := parquetgo.Group{}
	for _, field := range selectedFields {
		leaf, ok := schema.Lookup(field.Name)
		if !ok {
			if selection.EffectiveMissingFieldPolicy() == format.MissingFieldIgnore {
				continue
			}
			return nil, nil, fmt.Errorf("field selection references missing parquet field %q", field.Name)
		}
		group[field.Name] = leaf.Node
	}
	if len(group) == 0 {
		return nil, nil, nil
	}
	projected := parquetgo.NewSchema(schema.Name(), group)
	return projected, extractLeafColumnNames(projected), nil
}

func parseOptionsWithoutFieldSelection(options *format.ParseOptions) *format.ParseOptions {
	if options == nil || options.FieldSelection == nil {
		return options
	}
	copied := *options
	copied.FieldSelection = nil
	return &copied
}

func parquetDataFieldParseOptions(options *format.ParseOptions, partitions []partitionValue) *format.ParseOptions {
	selection := fieldSelectionFromOptions(options)
	if selection == nil || len(selection.Include) == 0 {
		return parseOptionsWithoutFieldSelection(options)
	}
	partitionNames := make(map[string]bool, len(partitions))
	for _, partition := range partitions {
		partitionNames[partition.Name] = true
	}
	include := make([]string, 0, len(selection.Include))
	for _, name := range selection.Include {
		if name == "" || partitionNames[name] {
			continue
		}
		include = append(include, name)
	}
	copied := *options
	if len(include) == 0 {
		copied.FieldSelection = nil
		return &copied
	}
	fieldSelection := *selection
	fieldSelection.Include = include
	copied.FieldSelection = &fieldSelection
	return &copied
}

func partitionFieldsFromScopedRefs(refs []scopedParquetRef) []datatype.FieldInfo {
	seen := map[string]bool{}
	fields := make([]datatype.FieldInfo, 0)
	for _, ref := range refs {
		for _, partition := range ref.Partitions {
			if seen[partition.Name] {
				continue
			}
			seen[partition.Name] = true
			fields = append(fields, datatype.FieldInfo{
				Name:     partition.Name,
				Type:     datatype.FieldTypeString,
				Nullable: false,
			})
		}
	}
	return fields
}

func appendPartitionFields(fields []datatype.FieldInfo, partitions []datatype.FieldInfo) []datatype.FieldInfo {
	result := append([]datatype.FieldInfo(nil), fields...)
	if len(partitions) == 0 {
		return result
	}
	existing := map[string]bool{}
	for _, field := range result {
		existing[field.Name] = true
	}
	for _, partition := range partitions {
		if existing[partition.Name] {
			continue
		}
		result = append(result, partition)
	}
	return result
}

func fieldNames(fields []datatype.FieldInfo) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	return names
}

func withPartitionValues(rows []map[string]interface{}, partitions []partitionValue) []map[string]interface{} {
	if len(rows) == 0 || len(partitions) == 0 {
		return rows
	}
	result := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		for _, partition := range partitions {
			if _, exists := row[partition.Name]; exists {
				continue
			}
			row[partition.Name] = partition.Value
		}
		result = append(result, row)
	}
	return result
}

func copyTableInfoWithPartitionFields(info *datatype.TableInfo, partitions []datatype.FieldInfo) *datatype.TableInfo {
	if info == nil {
		return nil
	}
	copied := info.Clone()
	copied.Fields = appendPartitionFields(info.Fields, partitions)
	return copied
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func sameFieldInfoList(left, right []datatype.FieldInfo) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Name != right[i].Name ||
			left[i].Type != right[i].Type ||
			left[i].Nullable != right[i].Nullable {
			return false
		}
	}
	return true
}

// valueToInterface 将 parquet.Value 转换为 Go 原生类型
func valueToInterface(v parquetgo.Value, geometry bool, srid int, geometryEncoding format.GeometryEncoding) (interface{}, error) {
	if v.IsNull() {
		return nil, nil
	}
	if geometry {
		if v.Kind() != parquetgo.ByteArray {
			return nil, fmt.Errorf("geoparquet WKB value must use parquet BYTE_ARRAY")
		}
		data := append([]byte(nil), v.ByteArray()...)
		if geometryEncoding == "" || geometryEncoding == format.GeometryEncodingWKB {
			return data, nil
		}
		geometry, err := commonSpatial.DecodeGeometryValue(data, string(format.GeometryEncodingWKB), srid)
		if err != nil {
			return nil, err
		}
		return commonSpatial.EncodeGeometryValue(geometry, string(geometryEncoding), srid)
	}
	switch v.Kind() {
	case parquetgo.Boolean:
		return v.Boolean(), nil
	case parquetgo.Int32:
		return v.Int32(), nil
	case parquetgo.Int64:
		return v.Int64(), nil
	case parquetgo.Int96:
		return v.Int96().String(), nil
	case parquetgo.Float:
		return v.Float(), nil
	case parquetgo.Double:
		return v.Double(), nil
	case parquetgo.ByteArray:
		return string(v.ByteArray()), nil
	case parquetgo.FixedLenByteArray:
		return string(v.ByteArray()), nil
	default:
		return v.String(), nil
	}
}

// extractFields 从 Parquet Schema 提取 FieldInfo 列表
func extractFields(schema *parquetgo.Schema) []datatype.FieldInfo {
	if schema == nil {
		return nil
	}

	fields := schema.Fields()
	result := make([]datatype.FieldInfo, 0, len(fields))

	for _, f := range fields {
		fieldInfo := datatype.FieldInfo{
			Name:     f.Name(),
			Nullable: f.Optional(),
			Type:     mapParquetType(f),
		}
		result = append(result, fieldInfo)
	}

	return result
}

// extractLeafColumnNames 提取叶子列名（与 parquet.Row 中的 Value 顺序对应）
func extractLeafColumnNames(schema *parquetgo.Schema) []string {
	if schema == nil {
		return nil
	}
	fields := schema.Fields()
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.Name())
	}
	return names
}

// parquetTypeString 返回 Parquet 类型的字符串表示
func parquetTypeString(f parquetgo.Field) string {
	if f.Type() == nil {
		return "unknown"
	}
	return strings.ToLower(f.Type().String())
}

// mapParquetType 将 Parquet 类型映射到 ADDP 统一类型
func mapParquetType(f parquetgo.Field) datatype.FieldType {
	if f.Type() == nil {
		return datatype.FieldTypeUnknown
	}

	// 先检查逻辑类型
	lt := f.Type().LogicalType()
	if lt != nil {
		switch {
		case lt.Date != nil:
			return datatype.FieldTypeDate
		case lt.Time != nil:
			return datatype.FieldTypeTime
		case lt.Timestamp != nil:
			return datatype.FieldTypeTimestamp
		case lt.Decimal != nil:
			return datatype.FieldTypeDecimal
		case lt.UTF8 != nil:
			return datatype.FieldTypeString
		case lt.UUID != nil:
			return datatype.FieldTypeUUID
		case lt.List != nil:
			return datatype.FieldTypeArray
		case lt.Map != nil:
			return datatype.FieldTypeJSON
		}
	}

	// 按物理类型映射
	switch f.Type() {
	case parquetgo.BooleanType:
		return datatype.FieldTypeBool
	case parquetgo.Int32Type:
		return datatype.FieldTypeInt
	case parquetgo.Int64Type:
		return datatype.FieldTypeBigInt
	case parquetgo.FloatType:
		return datatype.FieldTypeFloat
	case parquetgo.DoubleType:
		return datatype.FieldTypeDouble
	case parquetgo.ByteArrayType:
		return datatype.FieldTypeString
	case parquetgo.Int96Type:
		return datatype.FieldTypeTimestamp
	default:
		return datatype.FieldTypeString
	}
}

// 确保 parquetfmt 包被使用（避免 unused import）
var _ *parquetfmt.LogicalType
