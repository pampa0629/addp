package geojson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/addp/common/format"
)

const defaultGeometryField = "geometry"

// Parser 提供 GeoJSON 的通用解析能力
type Parser struct {
	options       *format.ParseOptions
	geometryField string
}

// NewParser 创建 GeoJSON 解析器
// geometry_field 可以通过 ParseOptions.ExtraParams["geometry_field"] 重写
func NewParser(opts *format.ParseOptions) *Parser {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	geometryField := defaultGeometryField
	if opts.ExtraParams != nil {
		if v, ok := opts.ExtraParams["geometry_field"].(string); ok && v != "" {
			geometryField = v
		}
	}
	return &Parser{
		options:       opts,
		geometryField: geometryField,
	}
}

// SupportedFormats 返回支持的格式
func (p *Parser) SupportedFormats() []format.FormatType {
	return []format.FormatType{format.FormatGeoJSON}
}

// ============ FileTableParser 接口实现 ============

// ParseTableInfo 从 GeoJSON 文件中提取 TableInfo
// 实现 format.FileTableParser 接口
func (p *Parser) ParseTableInfo(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	// 使用传入的 options，如果为 nil 则使用默认的
	opts := p.options
	if options != nil {
		opts = options
		// 更新 geometry field
		if opts.ExtraParams != nil {
			if v, ok := opts.ExtraParams["geometry_field"].(string); ok && v != "" {
				p.geometryField = v
			}
		}
	}

	iter, err := newIterator(input)
	if err != nil {
		return nil, err
	}

	builder := newSchemaBuilder(p.geometryField)
	featureCount := int64(0)

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
	}

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

	// 构建 SpatialInfo 扩展
	var extensions []format.ExtensionInfo
	geometryType := builder.GeometryType()
	if geometryType != "" {
		srid := 4326 // GeoJSON 默认 WGS84
		if iter.meta.CoordinateSystem != "" {
			// 尝试从坐标系统字符串解析 SRID
			// 简化处理：如果包含EPSG信息则提取
			if strings.Contains(strings.ToUpper(iter.meta.CoordinateSystem), "EPSG:") {
				// 可以进一步解析
			}
		}

		spatialInfo := &format.SpatialInfo{
			GeometryColumn: p.geometryField,
			GeometryType:   geometryType,
			SRID:           srid,
			Dimension:      2, // GeoJSON 主要是 2D
		}
		extensions = append(extensions, spatialInfo)
	}

	// 构建 TableInfo
	tableInfo := &format.TableInfo{
		Name:       "geojson_features", // GeoJSON 没有表名，使用默认值
		RowCount:   &featureCount,
		Fields:     fields,
		PrimaryKey: []string{}, // GeoJSON 没有主键
		Extensions: extensions,
	}

	return tableInfo, nil
}

// ReadPreview 读取 GeoJSON 数据预览
// 实现 format.FileTableParser 接口
func (p *Parser) ReadPreview(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	iter, err := newIterator(input)
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

		record := feature.ToRecord(p.geometryField)
		records = append(records, record)
		read++
	}

	return records, nil
}

// Feature 表示单条 GeoJSON Feature
type Feature struct {
	ID         interface{}
	Geometry   map[string]interface{}
	Properties map[string]interface{}
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
	record[geometryField] = f.Geometry
	return record
}

// Metadata 解析的附加信息
type Metadata struct {
	BoundingBox      []float64
	CoordinateSystem string
}

type iterator struct {
	decoder      *json.Decoder
	meta         Metadata
	geometrySeen bool
}

func newIterator(r io.Reader) (*iterator, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()

	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("geojson: failed to read root token: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("geojson: expected object start")
	}

	it := &iterator{
		decoder: dec,
		meta:    Metadata{},
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

			return it, nil
		default:
			var skip interface{}
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("geojson: failed to skip key %q: %w", key, err)
			}
		}
	}

	return nil, fmt.Errorf("geojson: features array not found")
}

// Next 读取下一条 Feature
func (it *iterator) Next() (*Feature, error) {
	if !it.decoder.More() {
		if err := it.finishArray(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}

	var raw rawFeature
	if err := it.decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("geojson: failed to decode feature: %w", err)
	}

	feature := &Feature{
		ID:         normalizeValue(raw.ID),
		Geometry:   normalizeGeometry(raw.Geometry),
		Properties: normalizeProperties(raw.Properties),
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
	fields = append(fields, format.Field{
		Name:     geometryField,
		Type:     format.FieldTypeGeometry,
		Nullable: false,
	})

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
	if len(fields) > 0 {
		schema.GeometryField = &geometryField
	}

	return schema
}

// ---------- Metadata builder ----------

type metadataBuilder struct {
	count         int64
	geometryTypes map[string]struct{}
	propertySet   map[string]struct{}
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
	case float32, float64:
		return format.FieldTypeFloat
	case json.Number:
		str := v.String()
		if strings.Contains(str, ".") {
			return format.FieldTypeFloat
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
		if current == format.FieldTypeFloat || next == format.FieldTypeFloat {
			return format.FieldTypeFloat
		}
		if current == format.FieldTypeDecimal || next == format.FieldTypeDecimal {
			return format.FieldTypeDecimal
		}
		return format.FieldTypeFloat
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
	case format.FieldTypeInt, format.FieldTypeBigInt, format.FieldTypeFloat, format.FieldTypeDecimal:
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

func init() {
	parser := NewParser(nil)
	// 注册为 FileTableParser
	_ = format.RegisterFileTableParser(parser)
}

// Iterator 为外部提供流式读取能力
type Iterator struct {
	inner *iterator
}

// NewFeatureIterator 创建新的 Feature 迭代器
func NewFeatureIterator(r io.Reader) (*Iterator, error) {
	it, err := newIterator(r)
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
	it, err := newIterator(r)
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
