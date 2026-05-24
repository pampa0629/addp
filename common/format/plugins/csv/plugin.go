package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/addp/common/datatype"
	"io"
	"strconv"
	"strings"

	"github.com/addp/common/format"
)

// Plugin 实现分隔文本表格格式。
type Plugin struct {
	formatType format.FormatType
	options    *format.ParseOptions
}

// NewPlugin 创建 CSV 格式 plugin
func NewPlugin(opts *format.ParseOptions) *Plugin {
	return newPlugin(format.FormatCSV, opts)
}

// NewTSVPlugin 创建 TSV 格式 plugin。
func NewTSVPlugin(opts *format.ParseOptions) *Plugin {
	return newPlugin(format.FormatTSV, opts)
}

func newPlugin(formatType format.FormatType, opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	copied := *opts
	switch formatType {
	case format.FormatTSV:
		copied.Delimiter = '\t'
	default:
		copied.Delimiter = ','
		formatType = format.FormatCSV
	}
	return &Plugin{formatType: formatType, options: &copied}
}

func (p *Plugin) Format() format.FormatType {
	return p.formatType
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	extensions := []string{".csv"}
	mimeTypes := []string{"text/csv"}
	i18nKey := "format.csv"
	if p.formatType == format.FormatTSV {
		extensions = []string{".tsv"}
		mimeTypes = []string{"text/tab-separated-values"}
		i18nKey = "format.tsv"
	}
	return format.FormatDescriptor{
		ID:             "builtin-" + string(p.formatType),
		Format:         p.formatType,
		I18nKey:        i18nKey,
		DataType:       format.FormatDataTypeTable,
		Layouts:        []string{format.FormatLayoutSingle},
		ProviderHints:  []string{format.FormatProviderTable},
		Identification: format.FormatIdentification{Extensions: extensions, MimeTypes: mimeTypes},
		Providers:      format.FormatProviderDescriptor{FormatInfo: true, TableInfo: true, TableSample: true, Table: true, ContentIndex: true},
		ContentReaders: []string{string(format.ContentReaderTableSample), string(format.ContentReaderRawContent)},
		TransferRead:   true,
		TransferWrite:  true,
		Parse:          true,
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        p.formatType,
		DataType:      format.FormatDataTypeTable,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderTable},
		TransferRead:  true,
		TransferWrite: true,
		Parse:         true,
	}
}

func (p *Plugin) effectiveOptions(options *format.ParseOptions) *format.ParseOptions {
	if p == nil || p.options == nil {
		if options != nil {
			return options
		}
		return format.DefaultParseOptions()
	}
	copied := *p.options
	if options == nil {
		return &copied
	}
	copied.Encoding = options.Encoding
	copied.SkipRows = options.SkipRows
	copied.MaxRows = options.MaxRows
	copied.SampleSize = options.SampleSize
	copied.ExtraParams = options.ExtraParams
	copied.ContentIndexStep = options.ContentIndexStep
	copied.HasHeader = options.HasHeader
	copied.SpatialRefSys = options.SpatialRefSys
	copied.GeometryEncoding = options.GeometryEncoding
	copied.SheetName = options.SheetName
	copied.SheetIndex = options.SheetIndex
	copied.TableSample = options.TableSample
	copied.FieldSelection = options.FieldSelection
	if options.Delimiter != 0 {
		copied.Delimiter = options.Delimiter
	}
	return &copied
}

// DescribeFormat 返回 CSV 格式私有元数据，写入 attributes.format_info.csv。
func (p *Plugin) DescribeFormat(ctx context.Context, input io.Reader, options *format.ParseOptions) (map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := p.effectiveOptions(options)
	reader := csv.NewReader(input)
	p.configureReaderWithOptions(reader, opts)
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}
	attrs := (&Info{
		Encoding:   opts.Encoding,
		LineEnding: "\n",
	}).FormatAttributes()
	attrs["column_count"] = len(headers)
	return attrs, nil
}

// DescribeTable 从 CSV 文件中提取 table 类型信息。
func (p *Plugin) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableDescribeResult, error) {
	opts := p.effectiveOptions(options)

	reader := csv.NewReader(input)
	p.configureReaderWithOptions(reader, opts)

	// 读取表头
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}
	headerBytes := reader.InputOffset()
	index := p.newSparseRowIndex(opts, headerBytes)

	// 读取样本数据用于类型推断
	sampleRows := make([][]string, 0, opts.SampleSize)
	rowCount := int64(0)
	for i := 0; i < opts.SampleSize; i++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 容错：跳过损坏的行
			continue
		}
		sampleRows = append(sampleRows, record)
		rowCount++
		p.recordSparseRowAnchor(index, rowCount, reader.InputOffset())
	}

	// 继续统计剩余行数
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		rowCount++
		p.recordSparseRowAnchor(index, rowCount, reader.InputOffset())
	}
	index.RowCount = rowCount

	// 构建 FieldInfo 列表
	fields := make([]datatype.FieldInfo, len(headers))
	for i, header := range headers {
		fieldType := p.inferColumnType(sampleRows, i)
		fields[i] = datatype.FieldInfo{
			Name:     strings.TrimSpace(header),
			Type:     fieldType,
			Nullable: true, // CSV 默认允许 NULL
		}
	}

	// 构建 CSV 格式私有信息
	csvInfo := &Info{
		Delimiter:  opts.Delimiter,
		Encoding:   opts.Encoding,
		HasHeader:  opts.HasHeader,
		QuoteChar:  '"',
		EscapeChar: '"',
		LineEnding: "\n",
	}

	result := &format.TableDescribeResult{
		Table: &datatype.TableInfo{
			Name:       "csv_data", // CSV 文件没有表名，使用默认值
			RowCount:   &rowCount,
			Fields:     fields,
			PrimaryKey: []string{}, // CSV 没有主键
			Native:     csvInfo.TableNative(),
		},
		FormatInfo: csvInfo.FormatAttributes(),
	}
	if len(index.Anchors) > 0 {
		result.ContentIndex = index
	}

	selected, err := format.ApplyFieldSelectionToTableDescribeResult(result, opts.FieldSelection)
	if err != nil {
		return nil, err
	}
	return selected, nil
}

// SampleTable 读取 CSV 表格样本。
func (p *Plugin) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	opts := p.effectiveOptions(options)

	reader := csv.NewReader(input)
	p.configureReaderWithOptions(reader, opts)

	headers, localSkip, err := p.sampleHeadersAndLocalSkip(reader, offset, opts)
	if err != nil {
		return nil, err
	}

	// 跳过到 offset
	for i := int64(0); i < localSkip; i++ {
		if _, err := reader.Read(); err != nil {
			if err == io.EOF {
				return []map[string]interface{}{}, nil
			}
			return nil, fmt.Errorf("failed to skip to offset: %w", err)
		}
	}

	// 读取数据
	maxRows := limit
	if limit < 0 {
		maxRows = 1<<63 - 1 // 最大值
	}

	records := make([]map[string]interface{}, 0)
	for i := int64(0); i < maxRows; i++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 容错：跳过损坏的行
			continue
		}

		row := make(map[string]interface{})
		for j, header := range headers {
			if j >= len(record) {
				row[header] = nil
				continue
			}

			value := record[j]
			if opts.Encoding == "utf-8" {
				value = strings.TrimSpace(value)
			}

			// 自动类型转换
			row[header] = p.convertValue(value)
		}

		records = append(records, row)
	}

	return format.ApplyFieldSelectionToRows(records, opts.FieldSelection), nil
}

func (p *Plugin) OpenTableWriter(ctx context.Context, output io.Writer, schema *datatype.TableInfo, options *format.WriteOptions) (format.TableWriter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if output == nil {
		return nil, fmt.Errorf("csv table writer requires output")
	}
	fields := schemaFields(schema)
	if len(fields) == 0 {
		return nil, fmt.Errorf("csv table writer requires schema fields")
	}
	opts := p.effectiveWriteOptions(options)
	writer := csv.NewWriter(output)
	writer.Comma = opts.Delimiter
	if writer.Comma == 0 {
		writer.Comma = ','
	}
	tableWriter := &tableWriter{
		writer:     writer,
		fields:     fields,
		omitHeader: opts.OmitHeader,
	}
	if !tableWriter.omitHeader {
		if err := tableWriter.writer.Write(fields); err != nil {
			return nil, fmt.Errorf("failed to write CSV header: %w", err)
		}
	}
	return tableWriter, nil
}

func (p *Plugin) OpenTableReader(ctx context.Context, input io.Reader, options *format.ParseOptions) (format.TableReader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, fmt.Errorf("csv table reader requires input")
	}
	opts := p.effectiveOptions(options)
	reader := csv.NewReader(input)
	p.configureReaderWithOptions(reader, opts)

	headers, _, err := p.sampleHeadersAndLocalSkip(reader, 0, opts)
	if err != nil {
		return nil, err
	}
	fields := make([]datatype.FieldInfo, 0, len(headers))
	for _, header := range headers {
		name := strings.TrimSpace(header)
		if name == "" {
			continue
		}
		fields = append(fields, datatype.FieldInfo{
			Name:     name,
			Type:     datatype.FieldTypeString,
			Nullable: true,
		})
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("csv table reader requires at least one field")
	}

	schema, err := format.ApplyFieldSelectionToTableInfo(&datatype.TableInfo{
		Name:   "csv_data",
		Fields: fields,
	}, opts.FieldSelection)
	if err != nil {
		return nil, err
	}
	return &tableReader{
		reader:  reader,
		plugin:  p,
		headers: fieldNames(fields),
		schema:  schema,
	}, nil
}

// ============ 辅助方法 ============

type tableReader struct {
	reader  *csv.Reader
	plugin  *Plugin
	headers []string
	schema  *datatype.TableInfo
	closed  bool
}

func (r *tableReader) Fields() []datatype.FieldInfo {
	if r == nil || r.schema == nil {
		return nil
	}
	return append([]datatype.FieldInfo(nil), r.schema.Fields...)
}

func (r *tableReader) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if r.closed {
		return nil, fmt.Errorf("csv table reader is closed")
	}
	if limit < 0 {
		return nil, fmt.Errorf("csv table reader limit cannot be negative")
	}
	if limit == 0 {
		limit = 1
	}
	rows := make([]map[string]interface{}, 0, limit)
	for len(rows) < limit {
		if err := ctx.Err(); err != nil {
			return rows, err
		}
		record, err := r.reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		row := make(map[string]interface{}, len(r.headers))
		for i, header := range r.headers {
			if i >= len(record) {
				row[header] = nil
				continue
			}
			row[header] = r.plugin.convertValue(record[i])
		}
		rows = append(rows, row)
	}
	return format.ApplyFieldSelectionToRows(rows, r.schemaFieldSelection()), nil
}

func (r *tableReader) schemaFieldSelection() *format.FieldSelectionOptions {
	if r == nil || r.schema == nil || len(r.schema.Fields) == 0 {
		return nil
	}
	include := make([]string, 0, len(r.schema.Fields))
	for _, field := range r.schema.Fields {
		include = append(include, field.Name)
	}
	return &format.FieldSelectionOptions{Include: include, MissingFieldPolicy: format.MissingFieldIgnore}
}

func (r *tableReader) Close(ctx context.Context) error {
	if r.closed {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.closed = true
	return nil
}

type tableWriter struct {
	writer     *csv.Writer
	fields     []string
	omitHeader bool
	closed     bool
}

func (w *tableWriter) WriteRows(ctx context.Context, rows []map[string]interface{}) error {
	if w.closed {
		return fmt.Errorf("csv table writer is closed")
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		record := make([]string, len(w.fields))
		for i, field := range w.fields {
			record[i] = csvValue(row[field])
		}
		if err := w.writer.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}
	return w.writer.Error()
}

func (w *tableWriter) Close(ctx context.Context) error {
	if w.closed {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.writer.Flush()
	w.closed = true
	return w.writer.Error()
}

func (p *Plugin) effectiveWriteOptions(options *format.WriteOptions) *format.WriteOptions {
	opts := format.DefaultWriteOptions()
	if p != nil && p.formatType == format.FormatTSV {
		opts.Delimiter = '\t'
	}
	if options == nil {
		return opts
	}
	opts.Encoding = options.Encoding
	opts.ExtraParams = options.ExtraParams
	opts.OmitHeader = options.OmitHeader
	if options.Delimiter != 0 {
		opts.Delimiter = options.Delimiter
	}
	return opts
}

func schemaFields(schema *datatype.TableInfo) []string {
	if schema == nil {
		return nil
	}
	fields := make([]string, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		fields = append(fields, name)
	}
	return fields
}

func csvValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// configureReader 配置 CSV reader（使用默认 options）
func (p *Plugin) configureReader(reader *csv.Reader) {
	p.configureReaderWithOptions(reader, p.options)
}

// configureReaderWithOptions 配置 CSV reader（使用指定 options）
func (p *Plugin) configureReaderWithOptions(reader *csv.Reader, opts *format.ParseOptions) {
	reader.Comma = opts.Delimiter
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // 允许可变字段数

	// 跳过指定行数
	for i := 0; i < opts.SkipRows; i++ {
		reader.Read()
	}
}

func (p *Plugin) sampleHeadersAndLocalSkip(reader *csv.Reader, offset int64, opts *format.ParseOptions) ([]string, int64, error) {
	if opts != nil && opts.TableSample != nil && opts.TableSample.InputIsPositioned {
		if opts.TableSample.InputStartsAtRow > offset {
			return nil, 0, fmt.Errorf("positioned CSV reader starts at row %d after requested offset %d", opts.TableSample.InputStartsAtRow, offset)
		}
		headers := fieldNames(opts.TableSample.Fields)
		if len(headers) == 0 && opts.TableSample.InputHasHeader {
			record, err := reader.Read()
			if err != nil {
				return nil, 0, fmt.Errorf("failed to read positioned CSV headers: %w", err)
			}
			headers = normalizedHeaders(record)
		}
		if len(headers) == 0 {
			return nil, 0, fmt.Errorf("positioned CSV sample requires field metadata")
		}
		return headers, offset - opts.TableSample.InputStartsAtRow, nil
	}

	headers, err := reader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read CSV headers: %w", err)
	}
	return normalizedHeaders(headers), offset, nil
}

func (p *Plugin) newSparseRowIndex(opts *format.ParseOptions, headerBytes int64) *datatype.ContentIndex {
	step := int64(5000)
	if opts != nil && opts.ContentIndexStep > 0 {
		step = opts.ContentIndexStep
	}
	return &datatype.ContentIndex{
		Kind:        datatype.ContentIndexKindSparseRow,
		DataType:    datatype.DataTypeTable,
		Format:      string(p.formatType),
		Unit:        datatype.ContentIndexUnitRow,
		OffsetUnit:  datatype.ContentIndexOffsetByte,
		Step:        step,
		HeaderBytes: headerBytes,
		Anchors: []datatype.ContentIndexAnchor{{
			Row:        0,
			ByteOffset: headerBytes,
		}},
	}
}

func (p *Plugin) recordSparseRowAnchor(index *datatype.ContentIndex, nextRow int64, byteOffset int64) {
	if index == nil || index.Step <= 0 || nextRow <= 0 || nextRow%index.Step != 0 {
		return
	}
	anchors := index.Anchors
	if len(anchors) > 0 && anchors[len(anchors)-1].Row == nextRow {
		index.Anchors[len(anchors)-1].ByteOffset = byteOffset
		return
	}
	index.Anchors = append(index.Anchors, datatype.ContentIndexAnchor{
		Row:        nextRow,
		ByteOffset: byteOffset,
	})
}

func fieldNames(fields []datatype.FieldInfo) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func normalizedHeaders(headers []string) []string {
	normalized := make([]string, len(headers))
	for i, header := range headers {
		normalized[i] = strings.TrimSpace(header)
	}
	return normalized
}

// inferColumnType 推断列的数据类型
func (p *Plugin) inferColumnType(rows [][]string, colIndex int) datatype.FieldType {
	if len(rows) == 0 {
		return datatype.FieldTypeString
	}

	// 统计各类型的出现次数
	intCount := 0
	floatCount := 0
	boolCount := 0
	dateCount := 0
	nullCount := 0
	totalCount := 0

	for _, row := range rows {
		if colIndex >= len(row) {
			continue
		}

		value := strings.TrimSpace(row[colIndex])
		totalCount++

		if value == "" || strings.EqualFold(value, "null") || strings.EqualFold(value, "na") {
			nullCount++
			continue
		}

		// 检查整数
		if _, err := strconv.ParseInt(value, 10, 64); err == nil {
			intCount++
			continue
		}

		// 检查浮点数
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			floatCount++
			continue
		}

		// 检查布尔值。数值型 1/0 已在前面归为整数。
		if p.isBool(value) {
			boolCount++
			continue
		}

		// 检查日期
		if p.isDate(value) {
			dateCount++
			continue
		}
	}

	// 根据统计结果判断类型（需要80%以上的样本符合）
	threshold := float64(totalCount) * 0.8

	if float64(boolCount) >= threshold {
		return datatype.FieldTypeBool
	}
	if float64(intCount) >= threshold {
		return datatype.FieldTypeInt
	}
	if float64(floatCount+intCount) >= threshold {
		return datatype.FieldTypeDouble // CSV 中的浮点数默认为双精度
	}
	if float64(dateCount) >= threshold {
		return datatype.FieldTypeTimestamp
	}

	return datatype.FieldTypeString
}

// convertValue 将字符串转换为适当的类型
func (p *Plugin) convertValue(s string) interface{} {
	s = strings.TrimSpace(s)

	if s == "" || strings.EqualFold(s, "null") || strings.EqualFold(s, "na") {
		return nil
	}

	// 尝试整数
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}

	// 尝试浮点数
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}

	// 尝试布尔值。数值型 1/0 已在前面归为数值。
	if p.isBool(s) {
		lower := strings.ToLower(s)
		return lower == "true" || lower == "yes" || lower == "t" || lower == "y"
	}

	return s
}

// isBool 检查是否为布尔值
func (p *Plugin) isBool(s string) bool {
	s = strings.ToLower(s)
	return s == "true" || s == "false" || s == "yes" || s == "no" ||
		s == "1" || s == "0" || s == "t" || s == "f" || s == "y" || s == "n"
}

// isDate 简单的日期检测
func (p *Plugin) isDate(s string) bool {
	datePatterns := []string{
		"2006-01-02",
		"2006/01/02",
		"01-02-2006",
		"01/02/2006",
		"2006-01-02 15:04:05",
		"02-Jan-2006",
	}

	for _, pattern := range datePatterns {
		if len(s) == len(pattern) {
			match := true
			for i, ch := range pattern {
				if ch >= '0' && ch <= '9' {
					if i >= len(s) || (s[i] < '0' || s[i] > '9') {
						match = false
						break
					}
				} else if ch != rune(s[i]) {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

func init() {
	_ = format.RegisterFormatPlugin(NewPlugin(nil))
	_ = format.RegisterFormatPlugin(NewTSVPlugin(nil))
}
