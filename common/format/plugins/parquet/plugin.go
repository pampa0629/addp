// Package parquet 提供 Parquet 文件格式的解析能力
// 支持 Schema 推断和样本数据读取
// 使用纯 Go 实现（github.com/parquet-go/parquet-go），无 CGO 依赖
package parquet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/addp/common/datatype"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	parquetgo "github.com/parquet-go/parquet-go"
	parquetfmt "github.com/parquet-go/parquet-go/format"
)

const FileRowCountsOption = "parquet_file_row_counts"

// Plugin 实现 Parquet 格式 plugin。
type Plugin struct{}

type Info struct {
	Files            []FileInfo
	PartitionColumns []string
}

type FileInfo struct {
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
	files := parquetFileInfos(formatInfo["files"])
	partitionColumns := parquetPartitionColumns(native["partition_columns"])
	if len(partitionColumns) == 0 {
		partitionColumns = parquetPartitionColumns(formatInfo["partition_columns"])
	}
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

func parquetFileInfos(value interface{}) []FileInfo {
	switch files := value.(type) {
	case []FileInfo:
		return append([]FileInfo(nil), files...)
	case []interface{}:
		result := make([]FileInfo, 0, len(files))
		for _, item := range files {
			attrs, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			path := commonJSON.InterfaceString(attrs["path"])
			if path == "" {
				continue
			}
			result = append(result, FileInfo{
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
	files, ok := parquetAttrs["files"].([]FileInfo)
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

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:            "builtin-parquet",
		Format:        format.FormatParquet,
		I18nKey:       "format.parquet",
		DataType:      format.FormatDataTypeTable,
		Layouts:       []string{format.FormatLayoutSingle, format.FormatLayoutWhole},
		ProviderHints: []string{format.FormatProviderTable},
		Identification: format.FormatIdentification{
			Extensions:        []string{".parquet"},
			MimeTypes:         []string{"application/parquet", "application/x-parquet", "application/vnd.apache.parquet"},
			ContentSignatures: []string{"hex:50415231"},
		},
		Providers:      format.FormatProviderDescriptor{TableInfo: true, TableSample: true, Table: true, ScopeTable: true},
		ContentReaders: []string{string(format.ContentReaderTableSample), string(format.ContentReaderScopeTableSample), string(format.ContentReaderRawContent)},
		TransferRead:   true,
		TransferWrite:  true,
		Parse:          true,
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	return len(peek) >= 4 && string(peek[:4]) == "PAR1"
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(format.FormatParquet)
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        format.FormatParquet,
		DataType:      format.FormatDataTypeTable,
		Layouts:       []string{format.FormatLayoutSingle, format.FormatLayoutWhole},
		ProviderHints: []string{format.FormatProviderTable},
		TransferRead:  true,
		TransferWrite: true,
		Parse:         true,
	}
}

func (p *Plugin) OpenTableWriter(ctx context.Context, output io.Writer, schema *format.TableInfo, options *format.WriteOptions) (format.TableWriter, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if output == nil {
		return nil, fmt.Errorf("parquet table writer requires output")
	}
	fields := parquetWriterFields(schema)
	if len(fields) == 0 {
		return nil, fmt.Errorf("parquet table writer requires schema fields")
	}

	group := parquetgo.Group{}
	for _, field := range fields {
		group[field.Name] = parquetNodeForField(field)
	}
	writer := parquetgo.NewGenericWriter[any](output, parquetgo.NewSchema("", group))
	return &tableWriter{
		writer: writer,
		fields: fields,
	}, nil
}

func (p *Plugin) OpenTableReader(ctx context.Context, input io.Reader, options *format.ParseOptions) (format.TableReader, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
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
	rowCount := file.NumRows()
	schema := &format.TableInfo{
		TableInfo: datatype.TableInfo{
			Fields:   extractFields(file.Schema()),
			RowCount: &rowCount,
		},
	}
	schema, err = format.ApplyFieldSelectionToTableInfo(schema, fieldSelectionFromOptions(options))
	if err != nil {
		return nil, err
	}
	return &tableReader{
		file:           file,
		fieldNames:     extractLeafColumnNames(file.Schema()),
		schema:         schema,
		fieldSelection: fieldSelectionFromOptions(options),
	}, nil
}

// DescribeTable 从 Parquet 文件中提取 TableInfo（Schema + 行数）
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
	rowCount := file.NumRows()

	result := &format.TableDescribeResult{
		Table: &datatype.TableInfo{
			Fields:   fields,
			RowCount: &rowCount,
		},
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

	// 提取列名（叶子列顺序）
	fieldNames := extractLeafColumnNames(file.Schema())
	fieldSelection := fieldSelectionFromOptions(options)
	if _, err := format.ApplyFieldSelectionToTableInfo(&format.TableInfo{
		TableInfo: datatype.TableInfo{Fields: extractFields(file.Schema())},
	}, fieldSelection); err != nil {
		return nil, err
	}

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

		readErr := appendRows(ctx, rows, fieldNames, limit, &result)
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

func appendRows(ctx context.Context, rows parquetgo.Rows, fieldNames []string, limit int64, result *[]map[string]interface{}) error {
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
						row[fieldNames[j]] = valueToInterface(val)
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
	file           *parquetgo.File
	fieldNames     []string
	schema         *format.TableInfo
	fieldSelection *format.FieldSelectionOptions
	rowGroupIndex  int
	rows           parquetgo.Rows
	closed         bool
}

func (r *tableReader) Fields() []datatype.FieldInfo {
	if r == nil || r.schema == nil {
		return nil
	}
	return append([]datatype.FieldInfo(nil), r.schema.Fields...)
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
		if err := appendRows(ctx, r.rows, r.fieldNames, int64(limit), &result); err != nil {
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
	r.rows = r.file.RowGroups()[r.rowGroupIndex].Rows()
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
	writer *parquetgo.GenericWriter[any]
	fields []datatype.FieldInfo
	closed bool
}

func (w *tableWriter) WriteRows(ctx context.Context, rows []map[string]interface{}) error {
	if w.closed {
		return fmt.Errorf("parquet table writer is closed")
	}
	if len(rows) == 0 {
		return nil
	}
	values := make([]any, 0, len(rows))
	for _, row := range rows {
		if err := contextErr(ctx); err != nil {
			return err
		}
		values = append(values, parquetWriterRow(row, w.fields))
	}
	if _, err := w.writer.Write(values); err != nil {
		return fmt.Errorf("failed to write parquet rows: %w", err)
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

func parquetWriterFields(schema *format.TableInfo) []datatype.FieldInfo {
	if schema == nil {
		return nil
	}
	fields := make([]datatype.FieldInfo, 0, len(schema.Fields))
	seen := map[string]struct{}{}
	for _, field := range schema.Fields {
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
	case datatype.FieldTypeDate:
		node = parquetgo.Date()
	case datatype.FieldTypeTimestamp, datatype.FieldTypeTime:
		node = parquetgo.String()
	case datatype.FieldTypeJSON, datatype.FieldTypeArray,
		datatype.FieldTypeGeometry, datatype.FieldTypePoint, datatype.FieldTypeLineString,
		datatype.FieldTypePolygon, datatype.FieldTypeMultiPoint:
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

func parquetWriterRow(row map[string]interface{}, fields []datatype.FieldInfo) map[string]any {
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		out[field.Name] = parquetWriterValue(row[field.Name], field.Type)
	}
	return out
}

func parquetWriterValue(value interface{}, fieldType datatype.FieldType) any {
	if value == nil {
		return nil
	}
	switch fieldType {
	case datatype.FieldTypeBool:
		return boolValue(value)
	case datatype.FieldTypeInt:
		return int32(int64Value(value))
	case datatype.FieldTypeBigInt:
		return int64Value(value)
	case datatype.FieldTypeFloat:
		return float32(float64Value(value))
	case datatype.FieldTypeDouble, datatype.FieldTypeDecimal:
		return float64Value(value)
	case datatype.FieldTypeBytes:
		if bytes, ok := value.([]byte); ok {
			return bytes
		}
		return []byte(fmt.Sprint(value))
	case datatype.FieldTypeDate:
		return dateValue(value)
	case datatype.FieldTypeTimestamp, datatype.FieldTypeTime:
		return temporalString(value)
	case datatype.FieldTypeJSON, datatype.FieldTypeArray,
		datatype.FieldTypeGeometry, datatype.FieldTypePoint, datatype.FieldTypeLineString,
		datatype.FieldTypePolygon, datatype.FieldTypeMultiPoint:
		return jsonString(value)
	default:
		return fmt.Sprint(value)
	}
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
	files := make([]FileInfo, 0, len(scopedRefs))
	partitionFields := partitionFieldsFromScopedRefs(scopedRefs)
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
			baseInfo := &format.TableDescribeResult{
				Table: &datatype.TableInfo{
					Name:       contentio.BaseName(scope),
					Fields:     appendPartitionFields(info.Fields, partitionFields),
					PrimaryKey: append([]string(nil), info.PrimaryKey...),
					Native:     parquetTableNative(fieldNames(partitionFields)),
				},
			}
			merged, err = format.ApplyFieldSelectionToTableDescribeResult(baseInfo, fieldSelectionFromOptions(options))
			if err != nil {
				return nil, err
			}
		} else if !sameFieldSchema(dataFields, info.Fields) {
			return nil, fmt.Errorf("parquet scope %s has incompatible schema in %s", scope.Path, ref.Path)
		}
		if info.RowCount != nil {
			totalRows += *info.RowCount
			files = append(files, FileInfo{
				Path:     normalizeParquetPath(ref.Path),
				RowCount: *info.RowCount,
			})
		}
	}
	if merged == nil {
		return nil, fmt.Errorf("parquet scope %s has no parquet files", scope.Path)
	}
	merged.Table.RowCount = &totalRows
	merged.FormatInfo = (&Info{Files: files}).FormatAttributes()
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
	scopedRefs, err := listParquetScopeResources(ctx, reader, scope)
	if err != nil {
		return nil, err
	}
	if len(scopedRefs) == 0 {
		return nil, fmt.Errorf("parquet scope %s has no parquet files", scope.Path)
	}
	return &scopeTableReader{
		plugin:          p,
		reader:          reader,
		refs:            scopedRefs,
		partitionFields: partitionFieldsFromScopedRefs(scopedRefs),
		parseOptions:    options,
		fieldSelection:  fieldSelectionFromOptions(options),
	}, nil
}

type scopeTableReader struct {
	plugin            *Plugin
	reader            contentio.Reader
	refs              []scopedParquetRef
	partitionFields   []datatype.FieldInfo
	parseOptions      *format.ParseOptions
	fieldSelection    *format.FieldSelectionOptions
	schema            *format.TableInfo
	dataFields        []datatype.FieldInfo
	index             int
	currentInput      io.Closer
	current           format.TableReader
	currentPartitions []partitionValue
	closed            bool
}

func (r *scopeTableReader) Fields() []datatype.FieldInfo {
	if r == nil || r.schema == nil {
		return nil
	}
	return append([]datatype.FieldInfo(nil), r.schema.Fields...)
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
		if len(rows) == 0 {
			if err := r.closeCurrent(ctx); err != nil {
				return result, err
			}
		}
	}
	return result, nil
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
		scopedRef := r.refs[r.index]
		r.index++
		ref := scopedRef.Ref
		input, err := r.reader.Open(ctx, ref)
		if err != nil {
			return fmt.Errorf("failed to open parquet file %s: %w", ref.Path, err)
		}
		tableReader, err := r.plugin.OpenTableReader(ctx, input, parseOptionsWithoutFieldSelection(r.parseOptions))
		if err != nil {
			_ = input.Close()
			return fmt.Errorf("failed to open parquet table reader for %s: %w", ref.Path, err)
		}
		schema := &format.TableInfo{
			TableInfo: datatype.TableInfo{Fields: tableReader.Fields()},
		}
		if r.schema == nil {
			r.dataFields = append([]datatype.FieldInfo(nil), schema.Fields...)
			r.schema, err = format.ApplyFieldSelectionToTableInfo(copyTableInfoWithPartitionFields(schema, r.partitionFields), r.fieldSelection)
			if err != nil {
				_ = tableReader.Close(ctx)
				_ = input.Close()
				return err
			}
		} else if schema != nil && !sameFieldSchema(r.dataFields, schema.Fields) {
			_ = tableReader.Close(ctx)
			_ = input.Close()
			return fmt.Errorf("parquet scope has incompatible schema in %s", ref.Path)
		}
		r.currentInput = input
		r.current = tableReader
		r.currentPartitions = scopedRef.Partitions
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
	return firstErr
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

func parseOptionsWithoutFieldSelection(options *format.ParseOptions) *format.ParseOptions {
	if options == nil || options.FieldSelection == nil {
		return options
	}
	copied := *options
	copied.FieldSelection = nil
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

func copyTableInfoWithPartitionFields(info *format.TableInfo, partitions []datatype.FieldInfo) *format.TableInfo {
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

func sameFieldSchema(left, right []datatype.FieldInfo) bool {
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
func valueToInterface(v parquetgo.Value) interface{} {
	if v.IsNull() {
		return nil
	}
	switch v.Kind() {
	case parquetgo.Boolean:
		return v.Boolean()
	case parquetgo.Int32:
		return v.Int32()
	case parquetgo.Int64:
		return v.Int64()
	case parquetgo.Int96:
		return v.Int96().String()
	case parquetgo.Float:
		return v.Float()
	case parquetgo.Double:
		return v.Double()
	case parquetgo.ByteArray:
		return string(v.ByteArray())
	case parquetgo.FixedLenByteArray:
		return string(v.ByteArray())
	default:
		return v.String()
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
