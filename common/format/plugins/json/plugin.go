package jsonformat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
)

const defaultGeometryField = "geometry"
const defaultDocumentTextLimit int64 = 512 * 1024

const (
	StructureDocument          = "document"
	StructureGeoJSONFeatureSet = "geojson_feature_collection"
	StructureObjectArray       = "object_array"
)

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

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-json",
		Format:         format.FormatJSON,
		I18nKey:        "format.json",
		DataType:       format.FormatDataTypeDocument,
		Layouts:        []string{format.FormatLayoutSingle},
		ProviderHints:  []string{format.FormatProviderDocument, format.FormatProviderTable, format.FormatProviderSpatial},
		Identification: format.FormatIdentification{Extensions: []string{".json", ".geojson"}, MimeTypes: []string{"application/json", "application/geo+json", "application/vnd.geo+json"}},
		Providers:      format.FormatProviderDescriptor{DocumentInfo: true, FormatInfo: true, TableInfo: true, TableSample: true, Table: true, ContentIndex: true},
		ContentReaders: []string{string(format.ContentReaderDocumentText), string(format.ContentReaderTableSample), string(format.ContentReaderRawContent)},
		TransferRead:   true,
		TransferWrite:  true,
		Parse:          true,
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile, format.EngineFamilyDocument},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(format.FormatJSON)
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        format.FormatJSON,
		DataType:      format.FormatDataTypeDocument,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument, format.FormatProviderTable, format.FormatProviderSpatial},
		TransferRead:  true,
		TransferWrite: true,
		Parse:         true,
	}
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
	info["has_geometry"] = builder.HasGeometry()
	if len(iter.meta.BoundingBox) == 4 {
		info["bbox"] = iter.meta.BoundingBox
	} else if bbox, ok := builder.BoundingBox(); ok {
		info["bbox"] = bbox
	}
	if iter.meta.CoordinateSystem != "" {
		info["crs"] = iter.meta.CoordinateSystem
	}
	return info, nil
}

// DescribeDocument 返回普通 JSON 文档的轻量信息。
func (p *Plugin) DescribeDocument(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.DocumentInfo, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	return &format.DocumentInfo{
		Format:   format.FormatJSON,
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
func (p *Plugin) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
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

	tableInfo := builder.Build()
	tableInfo.RowCount = &featureCount
	tableInfo.PrimaryKey = nil

	// 仅在实际记录里发现 geometry 结构时构建 SpatialInfo。
	geometryType := builder.GeometryType()
	if geometryType != "" {
		spatialGeometryField := geometryField
		if tableInfo.SpatialInfo != nil && tableInfo.SpatialInfo.GeometryColumn != "" {
			spatialGeometryField = tableInfo.SpatialInfo.GeometryColumn
		}
		srid := builder.SRID()
		if crsSRID := commonSpatial.ParseSRID(iter.meta.CoordinateSystem); crsSRID > 0 {
			srid = crsSRID
		} else if srid == 0 && iter.structure == StructureGeoJSONFeatureSet {
			srid = commonSpatial.SRIDWGS84
		}

		tableInfo.SpatialInfo = &format.SpatialInfo{
			GeometryColumn: spatialGeometryField,
			GeometryType:   geometryType,
			SRID:           srid,
			Dimension:      2, // GeoJSON 主要是 2D
		}
		if len(iter.meta.BoundingBox) == 4 {
			tableInfo.SpatialInfo.BoundingBox = &[4]float64{
				iter.meta.BoundingBox[0],
				iter.meta.BoundingBox[1],
				iter.meta.BoundingBox[2],
				iter.meta.BoundingBox[3],
			}
		} else if bbox, ok := builder.BoundingBox(); ok {
			tableInfo.SpatialInfo.BoundingBox = &bbox
		}
	}

	if len(index.Anchors) > 0 {
		tableInfo.ContentIndex = &format.ContentIndexInfo{Table: index}
	}

	return tableInfo, nil
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

	return records, nil
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
	return records, nil
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

func (p *Plugin) newSparseRowIndex(opts *format.ParseOptions, headerBytes int64) *format.ContentIndex {
	step := int64(5000)
	if opts != nil && opts.ContentIndexStep > 0 {
		step = opts.ContentIndexStep
	}
	return &format.ContentIndex{
		Kind:        format.ContentIndexKindSparseRow,
		DataType:    format.ContentIndexDataTypeTable,
		Format:      string(format.FormatJSON),
		Unit:        format.ContentIndexUnitRow,
		OffsetUnit:  format.ContentIndexOffsetByte,
		Step:        step,
		HeaderBytes: headerBytes,
		Anchors: []format.ContentIndexAnchor{{
			Row:        0,
			ByteOffset: headerBytes,
		}},
	}
}

func (p *Plugin) recordSparseRowAnchor(index *format.ContentIndex, nextRow int64, byteOffset int64) {
	if index == nil || index.Step <= 0 || nextRow <= 0 || nextRow%index.Step != 0 {
		return
	}
	anchors := index.Anchors
	if len(anchors) > 0 && anchors[len(anchors)-1].Row == nextRow {
		index.Anchors[len(anchors)-1].ByteOffset = byteOffset
		return
	}
	index.Anchors = append(index.Anchors, format.ContentIndexAnchor{
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

func init() {
	_ = format.RegisterFormatPlugin(NewPlugin(nil))
}
