package jsonformat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
	commonSpatial "github.com/addp/common/spatial"
)

const defaultGeometryField = "geometry"
const defaultDocumentTextLimit int64 = 512 * 1024

const (
	StructureDocument          = "document"
	StructureGeoJSONFeatureSet = "geojson_feature_collection"
	StructureJSONLines         = "json_lines"
	StructureObjectArray       = "object_array"
)

const (
	jsonTableWriteModeArray = "array"
	jsonTableWriteModeLines = "lines"
)

const jsonTargetEncodingGeoJSON = "geojson"

// Plugin 提供 JSON 结构解析能力。
//
// 当前实现支持 GeoJSON FeatureCollection 这种 JSON 记录集合结构。
type Plugin struct {
	options       *format.ParseOptions
	geometryField string
}

// NewPlugin 创建 JSON 格式 plugin。
// geometry_field 可以通过 ParseOptions.ExtraParams["geometry_field"] 重写
func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{
		options:       opts,
		geometryField: geometryFieldFromOptions(opts, defaultGeometryField),
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatJSON
}

func (p *Plugin) SupportsAccessIndex() bool {
	return true
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-json",
		Format:         format.FormatJSON,
		I18nKey:        "format.json",
		DataType:       datatype.DataTypeDocument,
		Layouts:        []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".json", ".geojson"}, MimeTypes: []string{"application/json", "application/geo+json", "application/vnd.geo+json"}},
	}
}

// OpenTableWriter 打开 JSON table 写出会话。
//
// 默认写出 JSON 对象数组；通过 WriteOptions.ExtraParams["json_mode"] 设置
// lines/jsonl/ndjson 时写出 JSON Lines。
func (p *Plugin) OpenTableWriter(ctx context.Context, output io.Writer, tableInfo *datatype.TableInfo, options *format.WriteOptions) (format.TableWriter, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "json.table_writer"); err != nil {
			return nil, err
		}
	}
	if output == nil {
		return nil, fmt.Errorf("json table writer requires output")
	}

	opts := jsonTableWriteOptions(options)
	writer := &tableWriter{
		output:         output,
		fields:         jsonWriterFields(tableInfo),
		mode:           opts.mode,
		pretty:         opts.pretty,
		targetEncoding: opts.targetEncoding,
		geometryField:  opts.geometryField,
		idField:        opts.idField,
	}
	if writer.geometryField == "" && options != nil && options.SpatialInfo != nil {
		writer.geometryField = strings.TrimSpace(options.SpatialInfo.PrimaryGeometryName())
	}
	if writer.geometryField == "" {
		writer.geometryField = p.geometryField
	}
	if writer.targetEncoding == jsonTargetEncodingGeoJSON {
		if _, err := writer.output.Write([]byte(`{"type":"FeatureCollection","features":[`)); err != nil {
			return nil, fmt.Errorf("failed to start GeoJSON feature collection: %w", err)
		}
		return writer, nil
	}
	if writer.mode == jsonTableWriteModeArray {
		if _, err := writer.output.Write([]byte("[")); err != nil {
			return nil, fmt.Errorf("failed to start JSON array: %w", err)
		}
	}
	return writer, nil
}

func (p *Plugin) OpenTableReader(ctx context.Context, input io.Reader, options *format.ParseOptions) (format.TableReader, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "json.table_reader"); err != nil {
			return nil, err
		}
	}
	if input == nil {
		return nil, fmt.Errorf("json table reader requires input")
	}
	geometryField := p.geometryField
	if options != nil {
		geometryField = geometryFieldFromOptions(options, geometryField)
	}
	iter, err := newRecordIterator(input)
	if err != nil {
		return nil, err
	}
	return &tableReader{
		iter:          iter,
		geometryField: geometryField,
		selection:     optionsFieldSelection(options),
	}, nil
}

// DescribeFormat 返回 JSON 的格式私有结构信息，写入 attributes.format_info.json。
func (p *Plugin) DescribeFormat(ctx context.Context, input io.Reader, options *format.ParseOptions) (map[string]interface{}, error) {
	iter, err := newRecordIterator(input)
	if err != nil {
		return map[string]interface{}{
			"structure": StructureDocument,
		}, nil
	}

	builder := newMetadataBuilder()
	for {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		feature, err := iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		builder.AddFeature(feature)
	}

	info := builder.Build()
	info["structure"] = iter.structure
	if len(iter.meta.BoundingBox) == 4 {
		info["bbox"] = iter.meta.BoundingBox
	}
	if iter.meta.CoordinateSystem != "" {
		info["crs"] = iter.meta.CoordinateSystem
	}
	return info, nil
}

// DescribeDocument 返回普通 JSON 文档的轻量信息。
func (p *Plugin) DescribeDocument(ctx context.Context, input io.Reader, options *format.ParseOptions) (*datatype.DocumentInfo, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	return &datatype.DocumentInfo{
		Encoding: "utf-8",
	}, nil
}

// ReadDocumentText 返回 JSON 文档原文片段。
func (p *Plugin) ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *format.ParseOptions) (string, bool, error) {
	if limit <= 0 {
		limit = defaultDocumentTextLimit
	}
	if err := contextErr(ctx); err != nil {
		return "", false, err
	}

	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return "", false, err
	}
	if err := contextErr(ctx); err != nil {
		return "", false, err
	}

	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	data = removeUTF8BOM(data)
	if !utf8.Valid(data) {
		data = []byte(string(data))
	}
	return string(data), truncated, nil
}

func removeUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

// DescribeTable 从 JSON 记录集合结构中提取 TableInfo。
func (p *Plugin) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableDescribeResult, error) {
	geometryField := p.geometryField
	opts := p.options
	if options != nil {
		opts = options
	}
	geometryField = geometryFieldFromOptions(opts, geometryField)

	iter, err := newRecordIterator(input)
	if err != nil {
		return nil, err
	}

	builder := newTableInfoBuilder(geometryField)
	featureCount := int64(0)
	index := p.newSparseRowIndex(opts, iter.dataStartOffset)

	for {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}

		feature, err := iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		builder.AddFeature(feature)
		featureCount++
		p.recordSparseRowAnchor(index, featureCount, iter.decoder.InputOffset())
	}
	index.RowCount = featureCount

	buildResult := builder.Build()
	tableInfo := buildResult.Table
	tableInfo.RowCount = &featureCount
	tableInfo.PrimaryKey = nil
	spatialInfo := buildResult.Spatial

	// 仅在实际记录里发现 geometry 结构时构建 SpatialInfo。
	geometryType := builder.GeometryType()
	if geometryType != "" {
		spatialGeometryField := geometryField
		if spatialInfo != nil && spatialInfo.PrimaryGeometryName() != "" {
			spatialGeometryField = spatialInfo.PrimaryGeometryName()
		}
		srid := builder.SRID()
		if crsSRID := commonSpatial.ParseSRID(iter.meta.CoordinateSystem); crsSRID > 0 {
			srid = crsSRID
		} else if srid == 0 && iter.structure == StructureGeoJSONFeatureSet {
			srid = commonSpatial.SRIDWGS84
		}

		spatialInfo = datatype.NewSingleGeometrySpatialInfo(spatialGeometryField, geometryType, srid, 2)
		if len(iter.meta.BoundingBox) == 4 {
			spatialInfo.Extent = &datatype.BoundingBox{
				iter.meta.BoundingBox[0],
				iter.meta.BoundingBox[1],
				iter.meta.BoundingBox[2],
				iter.meta.BoundingBox[3],
			}
		} else if bbox, ok := builder.BoundingBox(); ok {
			extent := datatype.BoundingBox(bbox)
			spatialInfo.Extent = &extent
		}
	}

	result := &format.TableDescribeResult{
		Table:   tableInfo,
		Spatial: spatialInfo,
	}
	if len(index.Anchors) > 0 {
		result.AccessIndex = index
	}

	selected, err := format.ApplyFieldSelectionToTableDescribeResult(result, opts.FieldSelection)
	if err != nil {
		return nil, err
	}
	return selected, nil
}

// SampleTable 读取 JSON 记录集合样本数据。
func (p *Plugin) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	geometryField := p.geometryField
	if options != nil {
		geometryField = geometryFieldFromOptions(options, geometryField)
	}
	if options != nil && options.TableSample != nil && options.TableSample.InputIsPositioned {
		return p.samplePositionedTable(ctx, input, offset, limit, options, geometryField)
	}

	iter, err := newRecordIterator(input)
	if err != nil {
		return nil, err
	}

	if offset < 0 {
		offset = 0
	}
	skipped := int64(0)
	for skipped < offset {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}

		_, err := iter.Next()
		if errors.Is(err, io.EOF) {
			return []map[string]interface{}{}, nil
		}
		if err != nil {
			return nil, err
		}
		skipped++
	}

	maxRows := limit
	if limit < 0 {
		maxRows = math.MaxInt64
	}

	records := make([]map[string]interface{}, 0)
	read := int64(0)
	for read < maxRows {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}

		feature, err := iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		record := feature.ToRecord(geometryField)
		records = append(records, record)
		read++
	}

	return format.ApplyFieldSelectionToRows(records, optionsFieldSelection(options)), nil
}

func (p *Plugin) samplePositionedTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions, geometryField string) ([]map[string]interface{}, error) {
	if options.TableSample.InputStartsAtRow > offset {
		return nil, fmt.Errorf("positioned JSON reader starts at row %d after requested offset %d", options.TableSample.InputStartsAtRow, offset)
	}

	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read positioned JSON sample: %w", err)
	}
	iter, err := newRecordIterator(bytes.NewReader(jsonArrayFragment(data)))
	if err != nil {
		return nil, err
	}

	localSkip := offset - options.TableSample.InputStartsAtRow
	if localSkip < 0 {
		localSkip = 0
	}
	for skipped := int64(0); skipped < localSkip; skipped++ {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		if _, err := iter.Next(); err != nil {
			if errors.Is(err, io.EOF) {
				return []map[string]interface{}{}, nil
			}
			return nil, err
		}
	}

	maxRows := limit
	if limit < 0 {
		maxRows = math.MaxInt64
	}
	records := make([]map[string]interface{}, 0)
	for read := int64(0); read < maxRows; read++ {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		feature, err := iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		records = append(records, feature.ToRecord(geometryField))
	}
	return format.ApplyFieldSelectionToRows(records, optionsFieldSelection(options)), nil
}

type tableReader struct {
	iter          *iterator
	geometryField string
	selection     *format.FieldSelectionOptions
	tableInfo     *datatype.TableInfo
	spatialInfo   *datatype.SpatialInfo
	closed        bool
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
		return nil, fmt.Errorf("json table reader is closed")
	}
	if limit < 0 {
		return nil, fmt.Errorf("json table reader limit cannot be negative")
	}
	if limit == 0 {
		limit = 1
	}

	rows := make([]map[string]interface{}, 0, limit)
	builder := newTableInfoBuilder(r.geometryField)
	for len(rows) < limit {
		if err := contextErr(ctx); err != nil {
			return rows, err
		}
		feature, err := r.iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return rows, err
		}
		builder.AddFeature(feature)
		rows = append(rows, feature.ToRecord(r.geometryField))
	}
	if len(rows) > 0 {
		result := builder.Build()
		if selected, err := format.ApplyFieldSelectionToTableDescribeResult(&format.TableDescribeResult{
			Table:   result.Table,
			Spatial: result.Spatial,
		}, r.selection); err == nil {
			result.Table = selected.Table
			result.Spatial = selected.Spatial
		}
		info := result.Table.Clone()
		r.tableInfo = mergeTableInfo(r.tableInfo, info)
		if r.spatialInfo == nil && result.Spatial != nil {
			r.spatialInfo = result.Spatial.Clone()
		}
	}
	return format.ApplyFieldSelectionToRows(rows, r.selection), nil
}

func (r *tableReader) Close(ctx context.Context) error {
	if r.closed {
		return nil
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.closed = true
	return nil
}

type tableWriter struct {
	output         io.Writer
	fields         []string
	mode           string
	pretty         bool
	targetEncoding string
	geometryField  string
	idField        string

	wroteRows bool
	closed    bool
}

func (w *tableWriter) WriteRows(ctx context.Context, rows []map[string]interface{}) error {
	if w.closed {
		return fmt.Errorf("json table writer is closed")
	}
	for _, row := range rows {
		if err := contextErr(ctx); err != nil {
			return err
		}
		writeRow := jsonTableRow(row, w.fields)
		if w.targetEncoding == jsonTargetEncodingGeoJSON {
			writeRow = geoJSONFeatureRow(writeRow, w.geometryField, w.idField)
		}
		data, err := marshalJSONTableRow(writeRow, w.pretty)
		if err != nil {
			return err
		}
		switch w.mode {
		case jsonTableWriteModeLines:
			if _, err := w.output.Write(data); err != nil {
				return fmt.Errorf("failed to write JSON line: %w", err)
			}
			if _, err := w.output.Write([]byte("\n")); err != nil {
				return fmt.Errorf("failed to write JSON line ending: %w", err)
			}
		default:
			if w.wroteRows {
				if _, err := w.output.Write([]byte(",")); err != nil {
					return fmt.Errorf("failed to write JSON row separator: %w", err)
				}
			}
			if w.pretty {
				prefix := []byte("\n  ")
				if w.targetEncoding == jsonTargetEncodingGeoJSON {
					prefix = []byte("\n    ")
				}
				if _, err := w.output.Write(prefix); err != nil {
					return fmt.Errorf("failed to write JSON row prefix: %w", err)
				}
				if w.targetEncoding == jsonTargetEncodingGeoJSON {
					data = bytes.ReplaceAll(data, []byte("\n"), []byte("\n    "))
				} else {
					data = bytes.ReplaceAll(data, []byte("\n"), []byte("\n  "))
				}
			}
			if _, err := w.output.Write(data); err != nil {
				return fmt.Errorf("failed to write JSON row: %w", err)
			}
		}
		w.wroteRows = true
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
	if w.targetEncoding == jsonTargetEncodingGeoJSON {
		suffix := []byte(`]}`)
		if w.pretty && w.wroteRows {
			suffix = []byte("\n  ]\n}")
		}
		if _, err := w.output.Write(suffix); err != nil {
			return fmt.Errorf("failed to close GeoJSON feature collection: %w", err)
		}
	} else if w.mode == jsonTableWriteModeArray {
		suffix := []byte("]")
		if w.pretty && w.wroteRows {
			suffix = []byte("\n]")
		}
		if _, err := w.output.Write(suffix); err != nil {
			return fmt.Errorf("failed to close JSON array: %w", err)
		}
	}
	w.closed = true
	return nil
}

type jsonTableWriterOptions struct {
	mode           string
	pretty         bool
	targetEncoding string
	geometryField  string
	idField        string
}

func jsonTableWriteOptions(options *format.WriteOptions) jsonTableWriterOptions {
	opts := jsonTableWriterOptions{mode: jsonTableWriteModeArray}
	if options == nil || options.ExtraParams == nil {
		return opts
	}
	if v, ok := options.ExtraParams["pretty"].(bool); ok {
		opts.pretty = v
	}
	opts.geometryField = strings.TrimSpace(formatOptionString(options.ExtraParams["geometry_field"]))
	opts.idField = strings.TrimSpace(formatOptionString(options.ExtraParams["id_field"]))
	opts.targetEncoding = jsonTargetEncoding(options.ExtraParams)
	mode := strings.ToLower(strings.TrimSpace(formatOptionString(options.ExtraParams["json_mode"])))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(formatOptionString(options.ExtraParams["layout"])))
	}
	switch mode {
	case "lines", "jsonl", "ndjson":
		opts.mode = jsonTableWriteModeLines
	case "", "array":
		opts.mode = jsonTableWriteModeArray
	default:
		opts.mode = mode
	}
	if opts.targetEncoding == jsonTargetEncodingGeoJSON {
		opts.mode = jsonTableWriteModeArray
	}
	return opts
}

func jsonTargetEncoding(params map[string]interface{}) string {
	value := strings.ToLower(strings.TrimSpace(formatOptionString(params["spatial.target_encoding"])))
	if value != "" {
		return value
	}
	if spatial, ok := params["spatial"].(map[string]interface{}); ok {
		return strings.ToLower(strings.TrimSpace(formatOptionString(spatial["target_encoding"])))
	}
	if spatial, ok := params["spatial"].(map[string]string); ok {
		return strings.ToLower(strings.TrimSpace(spatial["target_encoding"]))
	}
	return strings.ToLower(strings.TrimSpace(formatOptionString(params["target_encoding"])))
}

func marshalJSONTableRow(row map[string]interface{}, pretty bool) ([]byte, error) {
	if pretty {
		data, err := json.MarshalIndent(row, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to encode JSON row: %w", err)
		}
		return data, nil
	}
	data, err := json.Marshal(row)
	if err != nil {
		return nil, fmt.Errorf("failed to encode JSON row: %w", err)
	}
	return data, nil
}

func jsonTableRow(row map[string]interface{}, fields []string) map[string]interface{} {
	if len(fields) == 0 {
		copied := make(map[string]interface{}, len(row))
		for key, value := range row {
			copied[key] = value
		}
		return copied
	}
	out := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		out[field] = row[field]
	}
	return out
}

func geoJSONFeatureRow(row map[string]interface{}, geometryField, idField string) map[string]interface{} {
	if geometryField == "" {
		geometryField = defaultGeometryField
	}
	if idField == "" {
		idField = "id"
	}
	properties := make(map[string]interface{}, len(row))
	var geometry interface{}
	var id interface{}
	for key, value := range row {
		switch key {
		case geometryField:
			geometry = geoJSONGeometry(value)
		case idField:
			id = value
		default:
			properties[key] = value
		}
	}
	feature := map[string]interface{}{
		"type":       "Feature",
		"geometry":   geometry,
		"properties": properties,
	}
	if id != nil {
		feature["id"] = id
	}
	return feature
}

func geoJSONGeometry(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	if geom := geometryValue(value); geom != nil {
		return geom
	}
	if raw, ok := value.(json.RawMessage); ok {
		var decoded interface{}
		if err := json.Unmarshal(raw, &decoded); err == nil {
			return decoded
		}
	}
	if text, ok := value.(string); ok {
		var decoded interface{}
		if err := json.Unmarshal([]byte(text), &decoded); err == nil {
			return decoded
		}
	}
	return value
}

func jsonWriterFields(tableInfo *datatype.TableInfo) []string {
	if tableInfo == nil {
		return nil
	}
	fields := make([]string, 0, len(tableInfo.Fields))
	for _, field := range tableInfo.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		fields = append(fields, name)
	}
	return fields
}

func mergeTableInfo(current, next *datatype.TableInfo) *datatype.TableInfo {
	if current == nil {
		return next
	}
	if next == nil {
		return current
	}
	existing := make(map[string]struct{}, len(current.Fields))
	for _, field := range current.Fields {
		existing[field.Name] = struct{}{}
	}
	copied := current.Clone()
	for _, field := range next.Fields {
		if _, ok := existing[field.Name]; ok {
			continue
		}
		copied.Fields = append(copied.Fields, field)
	}
	return copied
}

func formatOptionString(value interface{}) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func jsonArrayFragment(data []byte) []byte {
	objects := jsonObjectFragments(data)
	if len(objects) == 0 {
		return []byte("[]")
	}
	total := 2
	for _, object := range objects {
		total += len(object) + 1
	}
	out := make([]byte, 0, total)
	out = append(out, '[')
	for i, object := range objects {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, object...)
	}
	out = append(out, ']')
	return out
}

func jsonObjectFragments(data []byte) [][]byte {
	fragments := make([][]byte, 0)
	depth := 0
	start := -1
	inString := false
	escaped := false
	for i, b := range data {
		if inString {
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			if depth > 0 {
				inString = true
			}
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				fragments = append(fragments, bytes.TrimSpace(data[start:i+1]))
				start = -1
			}
		}
	}
	return fragments
}

func (p *Plugin) newSparseRowIndex(opts *format.ParseOptions, headerBytes int64) *datatype.AccessIndex {
	step := int64(5000)
	if opts != nil && opts.AccessIndexStep > 0 {
		step = opts.AccessIndexStep
	}
	return &datatype.AccessIndex{
		Kind:        datatype.AccessIndexKindSparseRow,
		DataType:    datatype.DataTypeTable,
		Format:      string(format.FormatJSON),
		Unit:        datatype.AccessIndexUnitRow,
		OffsetUnit:  datatype.AccessIndexOffsetByte,
		Step:        step,
		HeaderBytes: headerBytes,
		Anchors: []datatype.AccessIndexAnchor{{
			Row:        0,
			ByteOffset: headerBytes,
		}},
	}
}

func (p *Plugin) recordSparseRowAnchor(index *datatype.AccessIndex, nextRow int64, byteOffset int64) {
	if index == nil || index.Step <= 0 || nextRow <= 0 || nextRow%index.Step != 0 {
		return
	}
	anchors := index.Anchors
	if len(anchors) > 0 && anchors[len(anchors)-1].Row == nextRow {
		index.Anchors[len(anchors)-1].ByteOffset = byteOffset
		return
	}
	index.Anchors = append(index.Anchors, datatype.AccessIndexAnchor{
		Row:        nextRow,
		ByteOffset: byteOffset,
	})
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

func geometryFieldFromOptions(opts *format.ParseOptions, fallback string) string {
	if fallback == "" {
		fallback = defaultGeometryField
	}
	if opts == nil || opts.ExtraParams == nil {
		return fallback
	}
	if v, ok := opts.ExtraParams["geometry_field"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func optionsFieldSelection(opts *format.ParseOptions) *format.FieldSelectionOptions {
	if opts == nil {
		return nil
	}
	return opts.FieldSelection
}

func init() {
	_ = format.RegisterFormatPlugin(NewPlugin(nil))
}
