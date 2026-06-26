package geojsonformat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/shared/jsonrecords"
	"github.com/addp/common/resume"
	commonSpatial "github.com/addp/common/spatial"
)

type Plugin struct {
	options       *format.ParseOptions
	geometryField string
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin(nil)); err != nil {
		panic(err)
	}
}

func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{
		options:       opts,
		geometryField: geometryFieldFromOptions(opts, jsonrecords.DefaultGeometryField),
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatGeoJSON
}

func (p *Plugin) SupportsAccessIndex() bool {
	return true
}

func (p *Plugin) SpatialEncodingCapability() format.SpatialEncodingCapability {
	return format.SpatialEncodingCapability{
		GeometryReadEncodings:  []format.GeometryEncoding{format.GeometryEncodingGeoJSON, format.GeometryEncodingEWKB},
		GeometryWriteEncodings: []format.GeometryEncoding{format.GeometryEncodingGeoJSON, format.GeometryEncodingEWKB},
		DefaultReadEncoding:    format.GeometryEncodingGeoJSON,
		DefaultWriteEncoding:   format.GeometryEncodingGeoJSON,
		NativeReadEncoding:     format.GeometryEncodingGeoJSON,
		NativeWriteEncoding:    format.GeometryEncodingGeoJSON,
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	return jsonrecords.LooksLikeGeoJSONFeatureCollection(peek)
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-geojson",
		Format:         format.FormatGeoJSON,
		I18nKey:        "format.geojson",
		DataType:       datatype.Table,
		Layouts:        []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".geojson"}, MimeTypes: []string{"application/geo+json", "application/vnd.geo+json"}},
	}
}

func (p *Plugin) DescribeFormat(ctx context.Context, input io.Reader, options *format.ParseOptions) (map[string]interface{}, error) {
	iter, err := newGeoJSONRecordIterator(input)
	if err != nil {
		return nil, err
	}

	builder := jsonrecords.NewMetadataBuilder()
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
	info["structure"] = jsonrecords.StructureGeoJSONFeatureSet
	if len(iter.Meta.BoundingBox) == 4 {
		info["bbox"] = iter.Meta.BoundingBox
	}
	if iter.Meta.CoordinateSystem != "" {
		info["crs"] = iter.Meta.CoordinateSystem
	}
	return info, nil
}

func (p *Plugin) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableDescribeResult, error) {
	geometryField := p.geometryField
	opts := p.options
	if options != nil {
		opts = options
	}
	geometryField = geometryFieldFromOptions(opts, geometryField)

	iter, err := newGeoJSONRecordIterator(input)
	if err != nil {
		return nil, err
	}

	builder := jsonrecords.NewTableInfoBuilder(geometryField)
	featureCount := int64(0)
	index := p.newSparseRowIndex(opts, iter.DataStartOffset)

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
		p.recordSparseRowAnchor(index, featureCount, iter.Decoder.InputOffset())
	}
	index.RowCount = featureCount

	buildResult := builder.Build()
	tableInfo := buildResult.Table
	tableInfo.Name = "geojson_features"
	tableInfo.RowCount = &featureCount
	tableInfo.PrimaryKey = nil
	spatialInfo := buildResult.Spatial
	formatInfo := geoJSONFormatInfo(iter.Meta)

	if nextSpatialInfo, coordinateRangeOutOfWGS84 := geoJSONSpatialInfoFromBuilder(geometryField, spatialInfo, iter.Meta, builder); nextSpatialInfo != nil {
		spatialInfo = nextSpatialInfo
		if coordinateRangeOutOfWGS84 {
			formatInfo["coordinate_range_out_of_wgs84"] = true
		}
	}

	result := &format.TableDescribeResult{
		Table:      tableInfo,
		Spatial:    spatialInfo,
		FormatInfo: formatInfo,
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

func geoJSONFormatInfo(meta jsonrecords.Metadata) map[string]interface{} {
	info := map[string]interface{}{
		"structure": jsonrecords.StructureGeoJSONFeatureSet,
	}
	if len(meta.BoundingBox) == 4 {
		info["bbox"] = meta.BoundingBox
	}
	if meta.CoordinateSystem != "" {
		info["crs"] = meta.CoordinateSystem
	}
	return info
}

func geoJSONSpatialInfoFromBuilder(geometryField string, existing *datatype.SpatialInfo, meta jsonrecords.Metadata, builder *jsonrecords.TableInfoBuilder) (*datatype.SpatialInfo, bool) {
	geometryType := builder.GeometryType()
	if geometryType == "" {
		return existing, false
	}
	spatialGeometryField := geometryField
	if existing != nil && existing.PrimaryGeometryName() != "" {
		spatialGeometryField = existing.PrimaryGeometryName()
	}
	extent, hasExtent := geoJSONSpatialExtent(meta, builder)
	srid, crsRef, coordinateRangeOutOfWGS84 := geoJSONSpatialReference(meta.CoordinateSystem, builder.SRID(), extent, hasExtent)

	spatialInfo := datatype.NewSingleGeometrySpatialInfo(spatialGeometryField, geometryType, srid, 2)
	if primary := spatialInfo.PrimaryGeometry(); primary != nil && crsRef != "" {
		primary.CRSRef = crsRef
	}
	if hasExtent {
		spatialInfo.Extent = &extent
	}
	return spatialInfo, coordinateRangeOutOfWGS84
}

func geoJSONSpatialExtent(meta jsonrecords.Metadata, builder *jsonrecords.TableInfoBuilder) (datatype.BoundingBox, bool) {
	if len(meta.BoundingBox) == 4 {
		return datatype.BoundingBox{
			meta.BoundingBox[0],
			meta.BoundingBox[1],
			meta.BoundingBox[2],
			meta.BoundingBox[3],
		}, true
	}
	if bbox, ok := builder.BoundingBox(); ok {
		return datatype.BoundingBox(bbox), true
	}
	return datatype.BoundingBox{}, false
}

func geoJSONSpatialReference(crs string, builderSRID int, extent datatype.BoundingBox, hasExtent bool) (int, string, bool) {
	if srid := commonSpatial.ParseSRID(crs); srid > 0 {
		return srid, datatype.EPSGCRSRef(srid), false
	}
	if strings.TrimSpace(crs) != "" {
		return 0, "", false
	}
	if builderSRID > 0 {
		return builderSRID, datatype.EPSGCRSRef(builderSRID), false
	}
	if hasExtent && !boundingBoxWithinWGS84(extent) {
		return 0, "", true
	}
	return commonSpatial.SRIDWGS84, datatype.EPSGCRSRef(commonSpatial.SRIDWGS84), false
}

func boundingBoxWithinWGS84(extent datatype.BoundingBox) bool {
	minX, minY, maxX, maxY := extent[0], extent[1], extent[2], extent[3]
	if math.IsNaN(minX) || math.IsNaN(minY) || math.IsNaN(maxX) || math.IsNaN(maxY) ||
		math.IsInf(minX, 0) || math.IsInf(minY, 0) || math.IsInf(maxX, 0) || math.IsInf(maxY, 0) {
		return false
	}
	if minX > maxX || minY > maxY {
		return false
	}
	return minX >= -180 && maxX <= 180 && minY >= -90 && maxY <= 90
}

func (p *Plugin) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	geometryField := p.geometryField
	if options != nil {
		geometryField = geometryFieldFromOptions(options, geometryField)
	}
	if options != nil && options.TableSample != nil && options.TableSample.InputIsPositioned {
		return p.samplePositionedTable(ctx, input, offset, limit, options, geometryField)
	}

	iter, err := newGeoJSONRecordIterator(input)
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

	features := make([]*jsonrecords.Feature, 0)
	builder := jsonrecords.NewTableInfoBuilder(geometryField)
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
		builder.AddFeature(feature)
		features = append(features, feature)
		read++
	}

	records, err := featuresToRecords(features, geometryField, geometryEncoding(options), geoJSONRecordSRID(iter.Meta, builder))
	if err != nil {
		return nil, err
	}
	return format.ApplyFieldSelectionToRows(records, optionsFieldSelection(options)), nil
}

func (p *Plugin) samplePositionedTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions, geometryField string) ([]map[string]interface{}, error) {
	if options.TableSample.InputStartsAtRow > offset {
		return nil, fmt.Errorf("positioned GeoJSON reader starts at row %d after requested offset %d", options.TableSample.InputStartsAtRow, offset)
	}

	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read positioned GeoJSON sample: %w", err)
	}
	iter, err := jsonrecords.NewRecordIterator(bytes.NewReader(jsonrecords.JSONArrayFragment(data)))
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
	features := make([]*jsonrecords.Feature, 0)
	builder := jsonrecords.NewTableInfoBuilder(geometryField)
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
		builder.AddFeature(feature)
		features = append(features, feature)
	}
	records, err := featuresToRecords(features, geometryField, geometryEncoding(options), geoJSONRecordSRID(iter.Meta, builder))
	if err != nil {
		return nil, err
	}
	return format.ApplyFieldSelectionToRows(records, optionsFieldSelection(options)), nil
}

func (p *Plugin) OpenTableReader(ctx context.Context, input io.Reader, options *format.ParseOptions) (format.TableReader, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "geojson.table_reader"); err != nil {
			return nil, err
		}
	}
	if input == nil {
		return nil, fmt.Errorf("geojson table reader requires input")
	}
	geometryField := p.geometryField
	if options != nil {
		geometryField = geometryFieldFromOptions(options, geometryField)
	}
	iter, err := newGeoJSONRecordIterator(input)
	if err != nil {
		return nil, err
	}
	return &tableReader{
		iter:             iter,
		meta:             iter.Meta,
		geometryField:    geometryField,
		geometryEncoding: geometryEncoding(options),
		selection:        optionsFieldSelection(options),
	}, nil
}

func (p *Plugin) OpenTableWriter(ctx context.Context, output io.Writer, tableInfo *datatype.TableInfo, options *format.WriteOptions) (format.TableWriter, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "geojson.table_writer"); err != nil {
			return nil, err
		}
	}
	if output == nil {
		return nil, fmt.Errorf("geojson table writer requires output")
	}
	if err := validateGeoJSONWriteSpatialInfo(options); err != nil {
		return nil, err
	}

	opts := geoJSONWriteOptions(options)
	writer := &tableWriter{
		output:        output,
		fields:        writerFields(tableInfo),
		pretty:        opts.pretty,
		geometryField: opts.geometryField,
		idField:       opts.idField,
	}
	if writer.geometryField == "" && options != nil && options.SpatialInfo != nil {
		writer.geometryField = strings.TrimSpace(options.SpatialInfo.PrimaryGeometryName())
	}
	if writer.geometryField == "" {
		writer.geometryField = p.geometryField
	}
	if _, err := writer.output.Write([]byte(`{"type":"FeatureCollection","features":[`)); err != nil {
		return nil, fmt.Errorf("failed to start GeoJSON feature collection: %w", err)
	}
	return writer, nil
}

func validateGeoJSONWriteSpatialInfo(options *format.WriteOptions) error {
	if options == nil || options.SpatialInfo == nil {
		return nil
	}
	spatialInfo := options.SpatialInfo
	if srid := spatialInfo.PrimarySRIDValue(); srid > 0 && srid != commonSpatial.SRIDWGS84 {
		return fmt.Errorf("geojson writer requires EPSG:4326 geometry, got SRID %d", srid)
	}
	crsRef := strings.TrimSpace(spatialInfo.PrimaryCRSRef())
	if crsRef == "" {
		return nil
	}
	if srid := commonSpatial.ParseSRID(crsRef); srid > 0 && srid != commonSpatial.SRIDWGS84 {
		return fmt.Errorf("geojson writer requires EPSG:4326 geometry, got %s", crsRef)
	}
	return nil
}

type tableReader struct {
	iter             *jsonrecords.RecordIterator
	meta             jsonrecords.Metadata
	geometryField    string
	geometryEncoding format.GeometryEncoding
	selection        *format.FieldSelectionOptions
	tableInfo        *datatype.TableInfo
	spatialInfo      *datatype.SpatialInfo
	closed           bool
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
		return nil, fmt.Errorf("geojson table reader is closed")
	}
	if limit < 0 {
		return nil, fmt.Errorf("geojson table reader limit cannot be negative")
	}
	if limit == 0 {
		limit = 1
	}

	rows := make([]map[string]interface{}, 0, limit)
	features := make([]*jsonrecords.Feature, 0, limit)
	builder := jsonrecords.NewTableInfoBuilder(r.geometryField)
	for len(features) < limit {
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
		features = append(features, feature)
	}
	if len(features) > 0 {
		result := builder.Build()
		if spatialInfo, _ := geoJSONSpatialInfoFromBuilder(r.geometryField, result.Spatial, r.meta, builder); spatialInfo != nil {
			result.Spatial = spatialInfo
		}
		records, err := featuresToRecords(features, r.geometryField, r.geometryEncoding, geoJSONRecordSRID(r.meta, builder))
		if err != nil {
			return rows, err
		}
		rows = records
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
	output        io.Writer
	fields        []string
	pretty        bool
	geometryField string
	idField       string
	wroteRows     bool
	closed        bool
}

func (w *tableWriter) WriteRows(ctx context.Context, rows []map[string]interface{}) error {
	if w.closed {
		return fmt.Errorf("geojson table writer is closed")
	}
	for _, row := range rows {
		if err := contextErr(ctx); err != nil {
			return err
		}
		data, err := marshalJSONTableRow(geoJSONFeatureRow(jsonTableRow(row, w.fields), w.geometryField, w.idField), w.pretty)
		if err != nil {
			return err
		}
		if w.wroteRows {
			if _, err := w.output.Write([]byte(",")); err != nil {
				return fmt.Errorf("failed to write GeoJSON feature separator: %w", err)
			}
		}
		if w.pretty {
			if _, err := w.output.Write([]byte("\n    ")); err != nil {
				return fmt.Errorf("failed to write GeoJSON row prefix: %w", err)
			}
			data = bytes.ReplaceAll(data, []byte("\n"), []byte("\n    "))
		}
		if _, err := w.output.Write(data); err != nil {
			return fmt.Errorf("failed to write GeoJSON feature: %w", err)
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
	suffix := []byte(`]}`)
	if w.pretty && w.wroteRows {
		suffix = []byte("\n  ]\n}")
	}
	if _, err := w.output.Write(suffix); err != nil {
		return fmt.Errorf("failed to close GeoJSON feature collection: %w", err)
	}
	w.closed = true
	return nil
}

type geoJSONWriterOptions struct {
	pretty        bool
	geometryField string
	idField       string
}

func geoJSONWriteOptions(options *format.WriteOptions) geoJSONWriterOptions {
	opts := geoJSONWriterOptions{}
	if options == nil || options.ExtraParams == nil {
		return opts
	}
	if v, ok := options.ExtraParams["pretty"].(bool); ok {
		opts.pretty = v
	}
	opts.geometryField = strings.TrimSpace(formatOptionString(options.ExtraParams["geometry_field"]))
	opts.idField = strings.TrimSpace(formatOptionString(options.ExtraParams["id_field"]))
	return opts
}

func newGeoJSONRecordIterator(input io.Reader) (*jsonrecords.RecordIterator, error) {
	iter, err := jsonrecords.NewRecordIterator(input)
	if err != nil {
		return nil, err
	}
	if iter.Structure != jsonrecords.StructureGeoJSONFeatureSet {
		return nil, fmt.Errorf("geojson table requires FeatureCollection")
	}
	return iter, nil
}

func (p *Plugin) newSparseRowIndex(opts *format.ParseOptions, headerBytes int64) *datatype.AccessIndex {
	step := int64(5000)
	if opts != nil && opts.AccessIndexStep > 0 {
		step = opts.AccessIndexStep
	}
	return &datatype.AccessIndex{
		Kind:        datatype.AccessIndexKindSparseRow,
		DataType:    datatype.Table,
		Format:      string(format.FormatGeoJSON),
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

func writerFields(tableInfo *datatype.TableInfo) []string {
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
		geometryField = jsonrecords.DefaultGeometryField
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
	if bytes, ok := value.([]byte); ok {
		geometry, err := commonSpatial.ParseGeometryValue(bytes)
		if err == nil {
			if geom, geomErr := commonSpatial.GeomToGeoJSONGeometry(geometry); geomErr == nil {
				return geom
			}
		}
	}
	if geom := jsonrecords.GeometryValue(value); geom != nil {
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

func marshalJSONTableRow(row map[string]interface{}, pretty bool) ([]byte, error) {
	if pretty {
		data, err := json.MarshalIndent(row, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to encode GeoJSON feature: %w", err)
		}
		return data, nil
	}
	data, err := json.Marshal(row)
	if err != nil {
		return nil, fmt.Errorf("failed to encode GeoJSON feature: %w", err)
	}
	return data, nil
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

func geometryFieldFromOptions(opts *format.ParseOptions, fallback string) string {
	if fallback == "" {
		fallback = jsonrecords.DefaultGeometryField
	}
	if opts == nil || opts.ExtraParams == nil {
		return fallback
	}
	if v, ok := opts.ExtraParams["geometry_field"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func geometryEncoding(opts *format.ParseOptions) format.GeometryEncoding {
	if opts == nil || opts.GeometryEncoding == "" || opts.GeometryEncoding == format.GeometryEncodingWKT {
		return format.GeometryEncodingGeoJSON
	}
	return opts.GeometryEncoding
}

func geoJSONRecordSRID(meta jsonrecords.Metadata, builder *jsonrecords.TableInfoBuilder) int {
	extent, hasExtent := geoJSONSpatialExtent(meta, builder)
	srid, _, _ := geoJSONSpatialReference(meta.CoordinateSystem, builder.SRID(), extent, hasExtent)
	return srid
}

func featuresToRecords(features []*jsonrecords.Feature, geometryField string, encoding format.GeometryEncoding, srid int) ([]map[string]interface{}, error) {
	records := make([]map[string]interface{}, 0, len(features))
	for _, feature := range features {
		record, err := featureToRecord(feature, geometryField, encoding, srid)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func featureToRecord(feature *jsonrecords.Feature, geometryField string, encoding format.GeometryEncoding, srid int) (map[string]interface{}, error) {
	record := feature.ToRecord(geometryField)
	if feature == nil || feature.GeometryType() == "" {
		return record, nil
	}
	field := geometryField
	if feature.GeometryField != "" {
		field = feature.GeometryField
	}
	switch encoding {
	case "", format.GeometryEncodingGeoJSON:
		return record, nil
	case format.GeometryEncodingEWKB:
		geometry, err := commonSpatial.GeoJSONGeometryToGeom(feature.Geometry, srid)
		if err != nil {
			return nil, err
		}
		data, err := commonSpatial.GeomToEWKB(geometry, srid)
		if err != nil {
			return nil, err
		}
		record[field] = data
		return record, nil
	default:
		return nil, fmt.Errorf("unsupported GeoJSON geometry encoding: %s", encoding)
	}
}

func optionsFieldSelection(opts *format.ParseOptions) *format.FieldSelectionOptions {
	if opts == nil {
		return nil
	}
	return opts.FieldSelection
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
