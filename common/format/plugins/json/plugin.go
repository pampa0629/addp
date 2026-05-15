package jsonformat

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
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

	builder := newSchemaBuilder(geometryField)
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

	schema := builder.Build()

	// 转换为 TableInfo
	fields := make([]format.FieldInfo, len(schema.Fields))
	for i, field := range schema.Fields {
		fields[i] = format.FieldInfo{
			Name:         field.Name,
			Type:         field.Type,
			OriginalType: string(field.Type), // GeoJSON没有原始类型
			Nullable:     field.Nullable,
			IsPrimaryKey: false,
			Comment:      field.Comment,
		}
	}

	// 仅在实际记录里发现 geometry 结构时构建 SpatialInfo 扩展。
	var extensions []format.ExtensionInfo
	geometryType := builder.GeometryType()
	if geometryType != "" {
		spatialGeometryField := geometryField
		if schema.GeometryField != nil && *schema.GeometryField != "" {
			spatialGeometryField = *schema.GeometryField
		}
		srid := builder.SRID()
		if crsSRID := commonSpatial.ParseSRID(iter.meta.CoordinateSystem); crsSRID > 0 {
			srid = crsSRID
		} else if srid == 0 && iter.structure == StructureGeoJSONFeatureSet {
			srid = commonSpatial.SRIDWGS84
		}

		spatialInfo := &format.SpatialInfo{
			GeometryColumn: spatialGeometryField,
			GeometryType:   geometryType,
			SRID:           srid,
			Dimension:      2, // GeoJSON 主要是 2D
		}
		if len(iter.meta.BoundingBox) == 4 {
			spatialInfo.BoundingBox = &[4]float64{
				iter.meta.BoundingBox[0],
				iter.meta.BoundingBox[1],
				iter.meta.BoundingBox[2],
				iter.meta.BoundingBox[3],
			}
		} else if bbox, ok := builder.BoundingBox(); ok {
			spatialInfo.BoundingBox = &bbox
		}
		extensions = append(extensions, spatialInfo)
	}
	if len(index.Anchors) > 0 {
		extensions = append(extensions, &format.ContentIndexInfo{Table: index})
	}

	// 构建 TableInfo
	tableInfo := &format.TableInfo{
		Name:       "json_records", // JSON 记录集合没有稳定表名，使用默认值
		RowCount:   &featureCount,
		Fields:     fields,
		PrimaryKey: []string{}, // GeoJSON 没有主键
		Extensions: extensions,
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

// Feature 表示单条 GeoJSON Feature
type Feature struct {
	ID            interface{}
	Geometry      map[string]interface{}
	GeometryField string
	Properties    map[string]interface{}
}

// GeometryType 返回几何类型（Point/LineString/Polygon/等）
func (f *Feature) GeometryType() string {
	if f.Geometry == nil {
		return ""
	}
	if v, ok := f.Geometry["type"].(string); ok {
		return v
	}
	return ""
}

// ToRecord 转换为通用记录（属性 + 几何字段）
func (f *Feature) ToRecord(geometryField string) map[string]interface{} {
	record := make(map[string]interface{}, len(f.Properties)+1)
	for k, v := range f.Properties {
		record[k] = v
	}
	if f.GeometryType() != "" {
		field := geometryField
		if f.GeometryField != "" {
			field = f.GeometryField
		}
		record[field] = f.Geometry
	}
	return record
}

// Metadata 解析的附加信息
type Metadata struct {
	BoundingBox      []float64
	CoordinateSystem string
}

type iterator struct {
	decoder         *json.Decoder
	meta            Metadata
	structure       string
	dataStartOffset int64
}

func newRecordIterator(r io.Reader) (*iterator, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()

	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("json table: failed to read root token: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil, fmt.Errorf("json table: expected object or array start")
	}
	if delim == '[' {
		return &iterator{
			decoder:         dec,
			meta:            Metadata{},
			structure:       StructureObjectArray,
			dataStartOffset: dec.InputOffset(),
		}, nil
	}
	if delim != '{' {
		return nil, fmt.Errorf("json table: expected object or array start")
	}

	it := &iterator{
		decoder:   dec,
		meta:      Metadata{},
		structure: StructureGeoJSONFeatureSet,
	}

	var collectionType string
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("geojson: failed to read key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("geojson: invalid key token %v", keyToken)
		}

		switch key {
		case "type":
			valTok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("geojson: failed to read type: %w", err)
			}
			if typeStr, ok := valTok.(string); ok {
				collectionType = typeStr
			}
		case "bbox":
			var bbox []float64
			if err := dec.Decode(&bbox); err != nil {
				return nil, fmt.Errorf("geojson: failed to decode bbox: %w", err)
			}
			it.meta.BoundingBox = bbox
		case "crs":
			var crs rawCRS
			if err := dec.Decode(&crs); err != nil {
				return nil, fmt.Errorf("geojson: failed to decode crs: %w", err)
			}
			if name := crs.Name(); name != "" {
				it.meta.CoordinateSystem = name
			}
		case "features":
			arrayTok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("geojson: failed to read features token: %w", err)
			}
			arrayDelim, ok := arrayTok.(json.Delim)
			if !ok || arrayDelim != '[' {
				return nil, fmt.Errorf("geojson: expected features array")
			}

			if collectionType != "" && !strings.EqualFold(collectionType, "FeatureCollection") {
				return nil, fmt.Errorf("geojson: unsupported type %q", collectionType)
			}

			it.dataStartOffset = dec.InputOffset()
			return it, nil
		default:
			var skip interface{}
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("geojson: failed to skip key %q: %w", key, err)
			}
		}
	}

	return nil, fmt.Errorf("json table: features array not found")
}

// Next 读取下一条 Feature
func (it *iterator) Next() (*Feature, error) {
	if !it.decoder.More() {
		if err := it.finishArray(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}

	var raw map[string]interface{}
	if err := it.decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("json table: failed to decode record: %w", err)
	}

	if it.structure == StructureObjectArray {
		return featureFromObjectRecord(raw), nil
	}

	feature := featureFromGeoJSONFeature(raw)
	if feature == nil {
		return nil, fmt.Errorf("geojson: invalid feature record")
	}

	return feature, nil
}

func (it *iterator) finishArray() error {
	// consume closing ']'
	token, err := it.decoder.Token()
	if err != nil {
		return fmt.Errorf("geojson: failed to close features array: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != ']' {
		return fmt.Errorf("geojson: expected closing features array, got %v", token)
	}

	// consume remaining tokens until '}' (end of object)
	for {
		token, err := it.decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("geojson: failed to finish object: %w", err)
		}
		if delim, ok := token.(json.Delim); ok && delim == '}' {
			return nil
		}

		// skip value for any trailing key
		if key, ok := token.(string); ok {
			var skip interface{}
			if err := it.decoder.Decode(&skip); err != nil {
				return fmt.Errorf("geojson: failed to skip trailing key %q: %w", key, err)
			}
		}
	}
}

type rawFeature struct {
	Type       string                 `json:"type"`
	ID         interface{}            `json:"id"`
	Geometry   map[string]interface{} `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

func featureFromObjectRecord(raw map[string]interface{}) *Feature {
	if isGeoJSONFeatureObject(raw) {
		return featureFromGeoJSONFeature(raw)
	}
	props := normalizeProperties(raw)
	geometryKey, geometry := detectGeometryProperty(props)
	if geometryKey != "" {
		delete(props, geometryKey)
	}
	return &Feature{
		Geometry:      geometry,
		GeometryField: geometryKey,
		Properties:    props,
	}
}

func isGeoJSONFeatureObject(raw map[string]interface{}) bool {
	typeName, _ := raw["type"].(string)
	if !strings.EqualFold(strings.TrimSpace(typeName), "Feature") {
		return false
	}
	_, hasProperties := raw["properties"].(map[string]interface{})
	_, hasGeometry := raw["geometry"].(map[string]interface{})
	return hasProperties || hasGeometry
}

func featureFromGeoJSONFeature(raw map[string]interface{}) *Feature {
	props := map[string]interface{}{}
	if rawProps, ok := raw["properties"].(map[string]interface{}); ok {
		props = normalizeProperties(rawProps)
	}
	return &Feature{
		ID:            normalizeValue(raw["id"]),
		Geometry:      normalizeGeometry(interfaceMap(raw["geometry"])),
		GeometryField: defaultGeometryField,
		Properties:    props,
	}
}

type rawCRS struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

func (c rawCRS) Name() string {
	if c.Properties == nil {
		return ""
	}
	if name, ok := c.Properties["name"].(string); ok {
		return name
	}
	return ""
}

func normalizeProperties(props map[string]interface{}) map[string]interface{} {
	if props == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(props))
	for k, v := range props {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeGeometry(geom map[string]interface{}) map[string]interface{} {
	if geom == nil {
		return nil
	}
	out := make(map[string]interface{}, len(geom))
	for k, v := range geom {
		out[k] = normalizeValue(v)
	}
	return out
}

func interfaceMap(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	default:
		return nil
	}
}

func detectGeometryProperty(props map[string]interface{}) (string, map[string]interface{}) {
	for key, value := range props {
		if geom := geometryValue(value); geom != nil {
			return key, geom
		}
	}
	return "", nil
}

func geometryValue(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return normalizeGeoJSONGeometry(typed)
	case string:
		if geom, err := decodeWKBGeometry(typed); err == nil {
			return geom
		}
	}
	return nil
}

func normalizeGeoJSONGeometry(value map[string]interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	typeName, _ := value["type"].(string)
	if !isGeoJSONGeometryType(typeName) {
		return nil
	}
	if _, ok := value["coordinates"]; !ok && !strings.EqualFold(typeName, "GeometryCollection") {
		return nil
	}
	return normalizeGeometry(value)
}

func geometrySRID(geom map[string]interface{}) int {
	if geom == nil {
		return 0
	}
	return intValue(geom["srid"])
}

func isGeoJSONGeometryType(typeName string) bool {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "point", "linestring", "polygon", "multipoint", "multilinestring", "multipolygon", "geometrycollection":
		return true
	default:
		return false
	}
}

func wkbGeometryType(value string) string {
	geom, err := decodeWKBGeometry(value)
	if err != nil {
		return ""
	}
	typeName, _ := geom["type"].(string)
	return typeName
}

func wkbTypeName(typeCode uint32) string {
	switch typeCode {
	case 1:
		return "Point"
	case 2:
		return "LineString"
	case 3:
		return "Polygon"
	case 4:
		return "MultiPoint"
	case 5:
		return "MultiLineString"
	case 6:
		return "MultiPolygon"
	case 7:
		return "GeometryCollection"
	default:
		return ""
	}
}

type wkbReader struct {
	data []byte
	pos  int
}

type wkbHeader struct {
	order    binary.ByteOrder
	typeCode uint32
	hasSRID  bool
	srid     uint32
	hasZ     bool
	hasM     bool
}

func decodeWKBGeometry(value string) (map[string]interface{}, error) {
	hexValue := strings.TrimSpace(value)
	if len(hexValue) < 10 || len(hexValue)%2 != 0 {
		return nil, fmt.Errorf("invalid WKB hex length")
	}
	data, err := hex.DecodeString(hexValue)
	if err != nil {
		return nil, err
	}
	reader := &wkbReader{data: data}
	geom, err := reader.readGeometry()
	if err != nil {
		return nil, err
	}
	if reader.pos > len(reader.data) {
		return nil, fmt.Errorf("invalid WKB cursor")
	}
	if reader.pos != len(reader.data) {
		return nil, fmt.Errorf("unexpected trailing WKB data")
	}
	geom["wkb"] = hexValue
	return geom, nil
}

func (r *wkbReader) readGeometry() (map[string]interface{}, error) {
	header, err := r.readHeader()
	if err != nil {
		return nil, err
	}
	typeName := wkbTypeName(header.typeCode)
	if typeName == "" {
		return nil, fmt.Errorf("unsupported WKB geometry type %d", header.typeCode)
	}

	geom := map[string]interface{}{"type": typeName}
	if header.hasSRID {
		geom["srid"] = int64(header.srid)
	}

	switch header.typeCode {
	case 1:
		geom["coordinates"] = r.readPosition(header)
	case 2:
		coordinates, err := r.readPositionList(header)
		if err != nil {
			return nil, err
		}
		geom["coordinates"] = coordinates
	case 3:
		coordinates, err := r.readPolygon(header)
		if err != nil {
			return nil, err
		}
		geom["coordinates"] = coordinates
	case 4, 5, 6:
		coordinates, err := r.readMultiGeometryCoordinates(header)
		if err != nil {
			return nil, err
		}
		geom["coordinates"] = coordinates
	case 7:
		geometries, err := r.readGeometryCollection(header)
		if err != nil {
			return nil, err
		}
		geom["geometries"] = geometries
	}
	if r.pos > len(r.data) {
		return nil, fmt.Errorf("short WKB coordinate data")
	}
	return geom, nil
}

func (r *wkbReader) readHeader() (wkbHeader, error) {
	if r.remaining() < 5 {
		return wkbHeader{}, fmt.Errorf("short WKB header")
	}
	byteOrder := r.data[r.pos]
	r.pos++
	var order binary.ByteOrder = binary.BigEndian
	if byteOrder == 1 {
		order = binary.LittleEndian
	} else if byteOrder != 0 {
		return wkbHeader{}, fmt.Errorf("invalid WKB byte order")
	}
	rawType, err := r.readUint32(order)
	if err != nil {
		return wkbHeader{}, err
	}
	header := wkbHeader{order: order}
	if rawType&0x80000000 != 0 {
		header.hasZ = true
		rawType &^= 0x80000000
	}
	if rawType&0x40000000 != 0 {
		header.hasM = true
		rawType &^= 0x40000000
	}
	if rawType&0x20000000 != 0 {
		header.hasSRID = true
		rawType &^= 0x20000000
		srid, err := r.readUint32(order)
		if err != nil {
			return wkbHeader{}, err
		}
		header.srid = srid
	}
	switch {
	case rawType >= 3000 && rawType < 4000:
		header.hasZ = true
		header.hasM = true
		rawType -= 3000
	case rawType >= 2000 && rawType < 3000:
		header.hasM = true
		rawType -= 2000
	case rawType >= 1000 && rawType < 2000:
		header.hasZ = true
		rawType -= 1000
	}
	header.typeCode = rawType
	return header, nil
}

func (r *wkbReader) readMultiGeometryCoordinates(header wkbHeader) (interface{}, error) {
	count, err := r.readUint32(header.order)
	if err != nil {
		return nil, err
	}
	items := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		geom, err := r.readGeometry()
		if err != nil {
			return nil, err
		}
		childType, _ := geom["type"].(string)
		switch header.typeCode {
		case 4:
			if childType != "Point" {
				return nil, fmt.Errorf("invalid MultiPoint child %q", childType)
			}
			items = append(items, geom["coordinates"])
		case 5:
			if childType != "LineString" {
				return nil, fmt.Errorf("invalid MultiLineString child %q", childType)
			}
			items = append(items, geom["coordinates"])
		case 6:
			if childType != "Polygon" {
				return nil, fmt.Errorf("invalid MultiPolygon child %q", childType)
			}
			items = append(items, geom["coordinates"])
		}
	}
	return items, nil
}

func (r *wkbReader) readGeometryCollection(header wkbHeader) ([]interface{}, error) {
	count, err := r.readUint32(header.order)
	if err != nil {
		return nil, err
	}
	items := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		geom, err := r.readGeometry()
		if err != nil {
			return nil, err
		}
		items = append(items, geom)
	}
	return items, nil
}

func (r *wkbReader) readPolygon(header wkbHeader) ([]interface{}, error) {
	count, err := r.readUint32(header.order)
	if err != nil {
		return nil, err
	}
	rings := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		ring, err := r.readPositionList(header)
		if err != nil {
			return nil, err
		}
		rings = append(rings, ring)
	}
	return rings, nil
}

func (r *wkbReader) readPositionList(header wkbHeader) ([]interface{}, error) {
	count, err := r.readUint32(header.order)
	if err != nil {
		return nil, err
	}
	positions := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		positions = append(positions, r.readPosition(header))
		if r.pos > len(r.data) {
			return nil, fmt.Errorf("short WKB coordinate data")
		}
	}
	return positions, nil
}

func (r *wkbReader) readPosition(header wkbHeader) []interface{} {
	position := []interface{}{r.readFloat64(header.order), r.readFloat64(header.order)}
	if header.hasZ {
		position = append(position, r.readFloat64(header.order))
	}
	if header.hasM {
		_ = r.readFloat64(header.order)
	}
	return position
}

func (r *wkbReader) readUint32(order binary.ByteOrder) (uint32, error) {
	if order == nil {
		order = binary.LittleEndian
	}
	if r.remaining() < 4 {
		return 0, fmt.Errorf("short WKB uint32")
	}
	value := order.Uint32(r.data[r.pos : r.pos+4])
	r.pos += 4
	return value, nil
}

func (r *wkbReader) readFloat64(order binary.ByteOrder) float64 {
	if r.remaining() < 8 {
		r.pos = len(r.data) + 1
		return 0
	}
	bits := order.Uint64(r.data[r.pos : r.pos+8])
	r.pos += 8
	return math.Float64frombits(bits)
}

func (r *wkbReader) remaining() int {
	return len(r.data) - r.pos
}

func normalizeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case json.Number:
		if strings.Contains(v.String(), ".") {
			if f, err := v.Float64(); err == nil {
				return f
			}
		}
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	case map[string]interface{}:
		m := make(map[string]interface{}, len(v))
		for key, val := range v {
			m[key] = normalizeValue(val)
		}
		return m
	case []interface{}:
		arr := make([]interface{}, len(v))
		for i, val := range v {
			arr[i] = normalizeValue(val)
		}
		return arr
	default:
		return value
	}
}

// ---------- Schema builder ----------

type schemaBuilder struct {
	geometryField string
	fieldTypes    map[string]format.FieldType
	geometryTypes map[string]struct{}
	propertySet   map[string]struct{}
	bounds        geometryBounds
	srid          int
}

func newSchemaBuilder(geometryField string) *schemaBuilder {
	return &schemaBuilder{
		geometryField: geometryField,
		fieldTypes:    make(map[string]format.FieldType),
		geometryTypes: make(map[string]struct{}),
		propertySet:   make(map[string]struct{}),
	}
}

func (b *schemaBuilder) AddFeature(feature *Feature) {
	if feature == nil {
		return
	}

	if gt := feature.GeometryType(); gt != "" {
		b.geometryTypes[gt] = struct{}{}
		if feature.GeometryField != "" {
			b.geometryField = feature.GeometryField
		}
		if b.srid == 0 {
			b.srid = geometrySRID(feature.Geometry)
		}
		b.bounds.AddGeometry(feature.Geometry)
	}

	for key, val := range feature.Properties {
		b.propertySet[key] = struct{}{}
		fieldType := inferFieldType(val)
		b.fieldTypes[key] = mergeFieldType(b.fieldTypes[key], fieldType)
	}
}

func (b *schemaBuilder) GeometryType() string {
	if len(b.geometryTypes) == 0 {
		return ""
	}
	if len(b.geometryTypes) == 1 {
		for gt := range b.geometryTypes {
			return gt
		}
	}
	return "Geometry"
}

func (b *schemaBuilder) HasGeometry() bool {
	return len(b.geometryTypes) > 0
}

func (b *schemaBuilder) BoundingBox() ([4]float64, bool) {
	return b.bounds.BoundingBox()
}

func (b *schemaBuilder) SRID() int {
	return b.srid
}

func (b *schemaBuilder) Build() *format.Schema {
	fieldNames := make([]string, 0, len(b.propertySet))
	for name := range b.propertySet {
		if name == "" {
			continue
		}
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	fields := make([]format.Field, 0, len(fieldNames)+1)
	geometryField := b.geometryField
	if b.HasGeometry() {
		fields = append(fields, format.Field{
			Name:     geometryField,
			Type:     format.FieldTypeGeometry,
			Nullable: false,
		})
	}

	for _, name := range fieldNames {
		fieldType := b.fieldTypes[name]
		if fieldType == "" {
			fieldType = format.FieldTypeUnknown
		}
		fields = append(fields, format.Field{
			Name:     name,
			Type:     fieldType,
			Nullable: true,
		})
	}

	schema := &format.Schema{
		Fields: fields,
	}
	if b.HasGeometry() {
		schema.GeometryField = &geometryField
	}

	return schema
}

// ---------- Metadata builder ----------

type metadataBuilder struct {
	count         int64
	geometryTypes map[string]struct{}
	propertySet   map[string]struct{}
	bounds        geometryBounds
}

func newMetadataBuilder() *metadataBuilder {
	return &metadataBuilder{
		geometryTypes: make(map[string]struct{}),
		propertySet:   make(map[string]struct{}),
	}
}

func (b *metadataBuilder) AddFeature(feature *Feature) {
	if feature == nil {
		return
	}
	b.count++
	if gt := feature.GeometryType(); gt != "" {
		b.geometryTypes[gt] = struct{}{}
		b.bounds.AddGeometry(feature.Geometry)
	}
	for key := range feature.Properties {
		if key == "" {
			continue
		}
		b.propertySet[key] = struct{}{}
	}
}

func (b *metadataBuilder) Build() map[string]interface{} {
	meta := map[string]interface{}{
		"feature_count": b.count,
	}
	if len(b.geometryTypes) > 0 {
		types := make([]string, 0, len(b.geometryTypes))
		for gt := range b.geometryTypes {
			types = append(types, gt)
		}
		sort.Strings(types)
		meta["geometry_types"] = types
	}
	if len(b.propertySet) > 0 {
		props := make([]string, 0, len(b.propertySet))
		for name := range b.propertySet {
			props = append(props, name)
		}
		sort.Strings(props)
		meta["properties"] = props
	}
	return meta
}

func (b *metadataBuilder) HasGeometry() bool {
	return len(b.geometryTypes) > 0
}

func (b *metadataBuilder) BoundingBox() ([4]float64, bool) {
	return b.bounds.BoundingBox()
}

type geometryBounds struct {
	seen       bool
	minX, minY float64
	maxX, maxY float64
}

func (b *geometryBounds) AddGeometry(geom map[string]interface{}) {
	if geom == nil {
		return
	}
	if coordinates, ok := geom["coordinates"]; ok {
		b.addCoordinates(coordinates)
		return
	}
	if geometries, ok := geom["geometries"].([]interface{}); ok {
		for _, item := range geometries {
			if child, ok := item.(map[string]interface{}); ok {
				b.AddGeometry(child)
			}
		}
	}
}

func (b *geometryBounds) addCoordinates(value interface{}) {
	switch typed := value.(type) {
	case []interface{}:
		if len(typed) >= 2 && isNumberValue(typed[0]) && isNumberValue(typed[1]) {
			x, _ := numberValue(typed[0])
			y, _ := numberValue(typed[1])
			b.addPoint(x, y)
			return
		}
		for _, item := range typed {
			b.addCoordinates(item)
		}
	case []float64:
		if len(typed) >= 2 {
			b.addPoint(typed[0], typed[1])
		}
	}
}

func (b *geometryBounds) addPoint(x, y float64) {
	if !b.seen {
		b.seen = true
		b.minX, b.minY, b.maxX, b.maxY = x, y, x, y
		return
	}
	if x < b.minX {
		b.minX = x
	}
	if y < b.minY {
		b.minY = y
	}
	if x > b.maxX {
		b.maxX = x
	}
	if y > b.maxY {
		b.maxY = y
	}
}

func (b *geometryBounds) BoundingBox() ([4]float64, bool) {
	if !b.seen {
		return [4]float64{}, false
	}
	return [4]float64{b.minX, b.minY, b.maxX, b.maxY}, true
}

func isNumberValue(value interface{}) bool {
	_, ok := numberValue(value)
	return ok
}

func numberValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		v, err := typed.Float64()
		return v, err == nil
	default:
		return 0, false
	}
}

func intValue(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		v, err := typed.Int64()
		if err == nil {
			return int(v)
		}
	}
	return 0
}

// ---------- Helpers ----------

func inferFieldType(value interface{}) format.FieldType {
	switch v := value.(type) {
	case nil:
		return format.FieldTypeUnknown
	case bool:
		return format.FieldTypeBool
	case int, int8, int16, int32, int64:
		return format.FieldTypeInt
	case uint, uint8, uint16, uint32, uint64:
		return format.FieldTypeBigInt
	case float32:
		return format.FieldTypeFloat // 单精度浮点数
	case float64:
		return format.FieldTypeDouble // 双精度浮点数
	case json.Number:
		str := v.String()
		if strings.Contains(str, ".") {
			return format.FieldTypeDouble // JSON 数字默认为双精度
		}
		return format.FieldTypeInt
	case string:
		if looksLikeDate(v) {
			return format.FieldTypeDate
		}
		if looksLikeTimestamp(v) {
			return format.FieldTypeTimestamp
		}
		return format.FieldTypeString
	case map[string]interface{}:
		return format.FieldTypeJSON
	case []interface{}:
		return format.FieldTypeArray
	default:
		return format.FieldTypeString
	}
}

func mergeFieldType(current, next format.FieldType) format.FieldType {
	if current == "" || current == format.FieldTypeUnknown {
		return next
	}
	if next == "" || next == format.FieldTypeUnknown {
		return current
	}
	if current == next {
		return current
	}

	// 数值类型合并
	if isNumericType(current) && isNumericType(next) {
		// 优先级: Decimal > Double > Float > BigInt > Int
		if current == format.FieldTypeDecimal || next == format.FieldTypeDecimal {
			return format.FieldTypeDecimal
		}
		if current == format.FieldTypeDouble || next == format.FieldTypeDouble {
			return format.FieldTypeDouble
		}
		if current == format.FieldTypeFloat || next == format.FieldTypeFloat {
			return format.FieldTypeFloat
		}
		if current == format.FieldTypeBigInt || next == format.FieldTypeBigInt {
			return format.FieldTypeBigInt
		}
		return format.FieldTypeInt
	}

	// 时间/日期混合为字符串
	if isTemporalType(current) && isTemporalType(next) {
		return format.FieldTypeString
	}

	// 不同类型统一降级为字符串
	return format.FieldTypeString
}

func isNumericType(t format.FieldType) bool {
	switch t {
	case format.FieldTypeInt, format.FieldTypeBigInt, format.FieldTypeFloat, format.FieldTypeDouble, format.FieldTypeDecimal:
		return true
	default:
		return false
	}
}

func isTemporalType(t format.FieldType) bool {
	switch t {
	case format.FieldTypeDate, format.FieldTypeTime, format.FieldTypeTimestamp:
		return true
	default:
		return false
	}
}

func looksLikeDate(value string) bool {
	if len(value) != 10 {
		return false
	}
	if value[4] != '-' || value[7] != '-' {
		return false
	}
	return true
}

func looksLikeTimestamp(value string) bool {
	return strings.Contains(value, "T") || strings.Count(value, ":") >= 2
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

// Iterator 为外部提供流式读取能力
type Iterator struct {
	inner *iterator
}

// NewFeatureIterator 创建新的 Feature 迭代器
func NewFeatureIterator(r io.Reader) (*Iterator, error) {
	it, err := newRecordIterator(r)
	if err != nil {
		return nil, err
	}
	return &Iterator{inner: it}, nil
}

// Next 返回下一条 Feature（到末尾返回 io.EOF）
func (i *Iterator) Next() (*Feature, error) {
	if i == nil || i.inner == nil {
		return nil, io.EOF
	}
	return i.inner.Next()
}

// Metadata 返回解析过程中收集的元信息
func (i *Iterator) Metadata() Metadata {
	if i == nil || i.inner == nil {
		return Metadata{}
	}
	return i.inner.meta
}

// FeatureCollection 表示完整的 FeatureCollection 数据
type FeatureCollection struct {
	Type string
	Metadata
	Features []Feature
}

// LoadFeatureCollection 读取并返回完整的 FeatureCollection
func LoadFeatureCollection(r io.Reader) (*FeatureCollection, error) {
	it, err := newRecordIterator(r)
	if err != nil {
		return nil, err
	}

	collection := &FeatureCollection{
		Type:     "FeatureCollection",
		Metadata: it.meta,
		Features: make([]Feature, 0),
	}

	for {
		feature, err := it.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		collection.Features = append(collection.Features, *feature)
	}

	return collection, nil
}
