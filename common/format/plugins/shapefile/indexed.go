package shapefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/jonas-p/go-shp"
	"io"
	"path/filepath"
	"strings"
)

func (plugin *Plugin) sampleMultiTableIndexed(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef, offset, limit int64, opts *format.ParseOptions) ([]map[string]interface{}, bool, error) {
	rangeReader, ok := reader.(contentio.RangeReader)
	if !ok {
		return nil, false, nil
	}
	source, ok, err := newIndexedMultiTableReadSource(ctx, plugin, refs, rangeReader, opts)
	if err != nil || !ok {
		return nil, ok, err
	}
	return source.readRows(ctx, offset, limit, sridFromParseOptions(opts))
}

type indexedMultiTableReadSource struct {
	plugin        *Plugin
	rangeReader   contentio.RangeReader
	shpRef        format.RelatedRef
	shxRef        format.RelatedRef
	dbfRef        format.RelatedRef
	dbfHeader     *dbfHeaderInfo
	encodingName  string
	geometryField string
	opts          *format.ParseOptions
}

func newIndexedMultiTableReadSource(ctx context.Context, plugin *Plugin, refs []format.RelatedRef, rangeReader contentio.RangeReader, opts *format.ParseOptions) (*indexedMultiTableReadSource, bool, error) {
	refMap := shapefileRefsByExtension(refs)
	shpRef, hasSHP := refMap[extSHP]
	shxRef, hasSHX := refMap[extSHX]
	dbfRef, hasDBF := refMap[extDBF]
	if !hasSHP || !hasSHX || !hasDBF {
		return nil, false, nil
	}

	encodingName := ""
	if opts != nil {
		encodingName = opts.Encoding
	}
	if encodingName == "" || NormalizeDBFEncoding(encodingName) == "utf-8" {
		if cpgRef, ok := refMap[extCPG]; ok {
			if text, err := readRefTextPrefix(ctx, rangeReader, cpgRef, 256); err == nil && strings.TrimSpace(text) != "" {
				encodingName = NormalizeDBFEncoding(strings.TrimSpace(text))
			}
		}
	}

	dbfHeader, err := readDBFHeaderIndexed(ctx, rangeReader, dbfRef, encodingName)
	if err != nil {
		return nil, true, err
	}
	return &indexedMultiTableReadSource{
		plugin:        plugin,
		rangeReader:   rangeReader,
		shpRef:        shpRef,
		shxRef:        shxRef,
		dbfRef:        dbfRef,
		dbfHeader:     dbfHeader,
		encodingName:  encodingName,
		geometryField: plugin.getGeometryFieldName(opts),
		opts:          opts,
	}, true, nil
}

func (s *indexedMultiTableReadSource) describeTable(ctx context.Context, refs []format.RelatedRef, opts *format.ParseOptions) (*datatype.TableInfo, *datatype.SpatialInfo, error) {
	shpHeader, err := readSHPHeaderIndexed(ctx, s.rangeReader, s.shpRef)
	if err != nil {
		return nil, nil, err
	}
	encodingName := s.encodingName
	if opts != nil && opts.Encoding != "" {
		encodingName = opts.Encoding
	}
	crsDefinition := ""
	if opts != nil {
		crsDefinition = opts.CRSDefinition
	}
	refMap := shapefileRefsByExtension(refs)
	tableInfo, spatialInfo := buildShapefileTableInfo(shapefileTableInfoInput{
		GeometryField: s.geometryField,
		BaseName:      strings.TrimSuffix(filepath.Base(s.shpRef.Ref.Path), filepath.Ext(s.shpRef.Ref.Path)),
		Refs:          refs,
		SHPHeader:     shpHeader,
		DBFHeader:     s.dbfHeader,
		Encoding:      encodingName,
		HasPRJ:        hasRefExtension(refMap, extPRJ),
		HasCPG:        hasRefExtension(refMap, extCPG),
		CRSDefinition: crsDefinition,
	})
	return tableInfo, spatialInfo, nil
}

func (s *indexedMultiTableReadSource) readRows(ctx context.Context, offset, limit int64, srid int) ([]map[string]interface{}, bool, error) {
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		return nil, true, nil
	}
	if limit == 0 {
		return []map[string]interface{}{}, true, nil
	}
	entries, err := readSHXWindow(ctx, s.rangeReader, s.shxRef, offset, limit)
	if err != nil {
		return nil, true, err
	}
	if len(entries) == 0 {
		return []map[string]interface{}{}, true, nil
	}
	rows, err := readDBFRecordsIndexed(ctx, s.rangeReader, s.dbfRef, s.dbfHeader, offset, int64(len(entries)), s.encodingName)
	if err != nil {
		return nil, true, err
	}
	shapes, err := readShapesIndexed(ctx, s.rangeReader, s.shpRef, entries)
	if err != nil {
		return nil, true, err
	}

	for i := range entries {
		select {
		case <-ctx.Done():
			return rows, true, ctx.Err()
		default:
		}
		row := rows[i]
		shape := shapes[i]
		if shape != nil {
			if geometryValue, err := shapeToRowValue(shape, s.opts, srid); err == nil {
				row[s.geometryField] = geometryValue
			} else {
				row[s.geometryField] = nil
			}
		} else {
			row[s.geometryField] = nil
		}
	}
	return rows, true, nil
}

type shxEntry struct {
	OffsetBytes int64
	LengthBytes int64
}

func readSHPHeaderIndexed(ctx context.Context, reader contentio.RangeReader, ref format.RelatedRef) (*shpHeaderInfo, error) {
	rc, err := reader.OpenRange(ctx, ref.Ref, 32, 68)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return parseSHPHeaderBytes(data)
}

func readSHXWindow(ctx context.Context, reader contentio.RangeReader, ref format.RelatedRef, offset, limit int64) ([]shxEntry, error) {
	start := int64(100) + offset*8
	length := limit * 8
	rc, err := reader.OpenRange(ctx, ref.Ref, start, length)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	count := len(data) / 8
	entries := make([]shxEntry, 0, count)
	for i := 0; i < count; i++ {
		chunk := data[i*8 : i*8+8]
		offsetWords := int64(binary.BigEndian.Uint32(chunk[0:4]))
		lengthWords := int64(binary.BigEndian.Uint32(chunk[4:8]))
		entries = append(entries, shxEntry{
			OffsetBytes: offsetWords * 2,
			LengthBytes: lengthWords * 2,
		})
	}
	return entries, nil
}

func shapefileRefsByExtension(refs []format.RelatedRef) map[string]format.RelatedRef {
	result := make(map[string]format.RelatedRef, len(refs))
	for _, ref := range refs {
		ext := strings.ToLower(filepath.Ext(ref.Ref.Path))
		if ext == "" {
			continue
		}
		result[ext] = ref
	}
	return result
}

func readRefTextPrefix(ctx context.Context, reader contentio.RangeReader, ref format.RelatedRef, length int64) (string, error) {
	rc, err := reader.OpenRange(ctx, ref.Ref, 0, length)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func hasRefExtension(refs map[string]format.RelatedRef, ext string) bool {
	_, ok := refs[strings.ToLower(ext)]
	return ok
}

func readDBFHeaderIndexed(ctx context.Context, reader contentio.RangeReader, ref format.RelatedRef, encodingName string) (*dbfHeaderInfo, error) {
	rc, err := reader.OpenRange(ctx, ref.Ref, 0, 32)
	if err != nil {
		return nil, err
	}
	header, err := io.ReadAll(rc)
	closeErr := rc.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(header) < 32 {
		return nil, fmt.Errorf("invalid DBF header length: %d", len(header))
	}
	headerLength := int(binary.LittleEndian.Uint16(header[8:10]))
	if headerLength < 33 {
		return nil, fmt.Errorf("invalid DBF header length: %d", headerLength)
	}
	rc, err = reader.OpenRange(ctx, ref.Ref, 0, int64(headerLength))
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(rc)
	closeErr = rc.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return parseDBFHeaderBytes(data, encodingName)
}

func readDBFRecordsIndexed(ctx context.Context, reader contentio.RangeReader, ref format.RelatedRef, header *dbfHeaderInfo, rowIndex, count int64, encodingName string) ([]map[string]interface{}, error) {
	recordLength := int64(header.RecordLength)
	if count <= 0 {
		return []map[string]interface{}{}, nil
	}
	rc, err := reader.OpenRange(ctx, ref.Ref, int64(header.HeaderLength)+rowIndex*recordLength, recordLength*count)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) < recordLength*count {
		return nil, fmt.Errorf("short DBF records: got %d bytes, want %d", len(data), recordLength*count)
	}
	rows := make([]map[string]interface{}, 0, count)
	for i := int64(0); i < count; i++ {
		start := i * recordLength
		row, err := parseDBFRecordBytes(data[start:start+recordLength], header, encodingName)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseDBFRecordBytes(data []byte, header *dbfHeaderInfo, encodingName string) (map[string]interface{}, error) {
	if len(data) < header.RecordLength {
		return nil, fmt.Errorf("short DBF record: got %d bytes, want %d", len(data), header.RecordLength)
	}
	row := make(map[string]interface{}, len(header.Fields)+1)
	pos := 1
	for _, field := range header.Fields {
		rawBytes := bytes.Trim(data[pos:pos+field.Size], "\x00")
		raw := strings.TrimSpace(DecodeDBFText(string(rawBytes), encodingName))
		if raw == "" {
			row[field.Name] = nil
		} else {
			row[field.Name] = parseDBFAttributeWithInfo(field, raw)
		}
		pos += field.Size
	}
	return row, nil
}

var errUnsupportedIndexedShapeType = errors.New("unsupported indexed shapefile shape type")

func isIndexedSampleFallbackError(err error) bool {
	return errors.Is(err, errUnsupportedIndexedShapeType)
}

func readShapesIndexed(ctx context.Context, reader contentio.RangeReader, ref format.RelatedRef, entries []shxEntry) ([]shp.Shape, error) {
	if len(entries) == 0 {
		return []shp.Shape{}, nil
	}
	start := entries[0].OffsetBytes
	end := entries[0].OffsetBytes + entries[0].LengthBytes + 8
	for _, entry := range entries[1:] {
		if entry.OffsetBytes < start {
			start = entry.OffsetBytes
		}
		recordEnd := entry.OffsetBytes + entry.LengthBytes + 8
		if recordEnd > end {
			end = recordEnd
		}
	}
	rc, err := reader.OpenRange(ctx, ref.Ref, start, end-start)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	shapes := make([]shp.Shape, 0, len(entries))
	for _, entry := range entries {
		relativeStart := entry.OffsetBytes - start
		relativeEnd := relativeStart + entry.LengthBytes + 8
		if relativeStart < 0 || relativeEnd > int64(len(data)) {
			return nil, fmt.Errorf("short SHP record window: got %d bytes, need [%d,%d)", len(data), relativeStart, relativeEnd)
		}
		shape, err := parseShapeRecordBytes(data[relativeStart:relativeEnd])
		if err != nil {
			return nil, err
		}
		shapes = append(shapes, shape)
	}
	return shapes, nil
}

func parseShapeRecordBytes(data []byte) (shp.Shape, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("short SHP record: %d bytes", len(data))
	}
	shapeType := shp.ShapeType(binary.LittleEndian.Uint32(data[8:12]))
	content := bytes.NewReader(data[12:])
	return parseShapeContent(shapeType, content)
}

type indexedPartedShape struct {
	Box       shp.Box
	NumParts  int32
	NumPoints int32
	Parts     []int32
	Points    []shp.Point
}

type indexedMultiPointShape struct {
	Box       shp.Box
	NumPoints int32
	Points    []shp.Point
}

func parseShapeContent(shapeType shp.ShapeType, content *bytes.Reader) (shp.Shape, error) {
	switch shapeType {
	case shp.NULL:
		return &shp.Null{}, nil
	case shp.POINT:
		return readIndexedPoint[shp.Point](content)
	case shp.POINTZ:
		return readIndexedPoint[shp.PointZ](content)
	case shp.POINTM:
		return readIndexedPoint[shp.PointM](content)
	case shp.POLYLINE:
		return readIndexedPolyLineContent(content)
	case shp.POLYLINEZ:
		return readIndexedPolyLineZContent(content)
	case shp.POLYLINEM:
		return readIndexedPolyLineMContent(content)
	case shp.POLYGON:
		line, err := readIndexedPolyLineContent(content)
		if err != nil {
			return nil, err
		}
		polygon := shp.Polygon(*line)
		return &polygon, nil
	case shp.POLYGONZ:
		line, err := readIndexedPolyLineZContent(content)
		if err != nil {
			return nil, err
		}
		polygon := shp.PolygonZ(*line)
		return &polygon, nil
	case shp.POLYGONM:
		return readIndexedPolygonMContent(content)
	case shp.MULTIPOINT:
		return readIndexedMultiPointContent(content)
	case shp.MULTIPOINTZ:
		return readIndexedMultiPointZContent(content)
	case shp.MULTIPOINTM:
		return readIndexedMultiPointMContent(content)
	default:
		return nil, fmt.Errorf("%w: %v", errUnsupportedIndexedShapeType, shapeType)
	}
}

func readIndexedPoint[T shp.Point | shp.PointZ | shp.PointM](content *bytes.Reader) (*T, error) {
	var point T
	if err := binary.Read(content, binary.LittleEndian, &point); err != nil {
		return nil, err
	}
	return &point, nil
}

func readIndexedPolyLineContent(content *bytes.Reader) (*shp.PolyLine, error) {
	base, err := readIndexedPartedShape(content)
	if err != nil {
		return nil, err
	}
	return &shp.PolyLine{
		Box:       base.Box,
		NumParts:  base.NumParts,
		NumPoints: base.NumPoints,
		Parts:     base.Parts,
		Points:    base.Points,
	}, nil
}

func readIndexedPolyLineZContent(content *bytes.Reader) (*shp.PolyLineZ, error) {
	base, err := readIndexedPartedShape(content)
	if err != nil {
		return nil, err
	}
	zRange, zArray, err := readIndexedFloat64RangeAndArray(content, base.NumPoints)
	if err != nil {
		return nil, err
	}
	mRange, mArray, err := readIndexedOptionalFloat64RangeAndArray(content, base.NumPoints)
	if err != nil {
		return nil, err
	}
	return &shp.PolyLineZ{
		Box:       base.Box,
		NumParts:  base.NumParts,
		NumPoints: base.NumPoints,
		Parts:     base.Parts,
		Points:    base.Points,
		ZRange:    zRange,
		ZArray:    zArray,
		MRange:    mRange,
		MArray:    mArray,
	}, nil
}

func readIndexedPolyLineMContent(content *bytes.Reader) (*shp.PolyLineM, error) {
	base, err := readIndexedPartedShape(content)
	if err != nil {
		return nil, err
	}
	mRange, mArray, err := readIndexedOptionalFloat64RangeAndArray(content, base.NumPoints)
	if err != nil {
		return nil, err
	}
	return &shp.PolyLineM{
		Box:       base.Box,
		NumParts:  base.NumParts,
		NumPoints: base.NumPoints,
		Parts:     base.Parts,
		Points:    base.Points,
		MRange:    mRange,
		MArray:    mArray,
	}, nil
}

func readIndexedPolygonMContent(content *bytes.Reader) (*shp.PolygonM, error) {
	base, err := readIndexedPartedShape(content)
	if err != nil {
		return nil, err
	}
	mRange, mArray, err := readIndexedOptionalFloat64RangeAndArray(content, base.NumPoints)
	if err != nil {
		return nil, err
	}
	return &shp.PolygonM{
		Box:       base.Box,
		NumParts:  base.NumParts,
		NumPoints: base.NumPoints,
		Parts:     base.Parts,
		Points:    base.Points,
		MRange:    mRange,
		MArray:    mArray,
	}, nil
}

func readIndexedMultiPointContent(content *bytes.Reader) (*shp.MultiPoint, error) {
	base, err := readIndexedMultiPointShape(content)
	if err != nil {
		return nil, err
	}
	return &shp.MultiPoint{
		Box:       base.Box,
		NumPoints: base.NumPoints,
		Points:    base.Points,
	}, nil
}

func readIndexedMultiPointZContent(content *bytes.Reader) (*shp.MultiPointZ, error) {
	base, err := readIndexedMultiPointShape(content)
	if err != nil {
		return nil, err
	}
	zRange, zArray, err := readIndexedFloat64RangeAndArray(content, base.NumPoints)
	if err != nil {
		return nil, err
	}
	mRange, mArray, err := readIndexedOptionalFloat64RangeAndArray(content, base.NumPoints)
	if err != nil {
		return nil, err
	}
	return &shp.MultiPointZ{
		Box:       base.Box,
		NumPoints: base.NumPoints,
		Points:    base.Points,
		ZRange:    zRange,
		ZArray:    zArray,
		MRange:    mRange,
		MArray:    mArray,
	}, nil
}

func readIndexedMultiPointMContent(content *bytes.Reader) (*shp.MultiPointM, error) {
	base, err := readIndexedMultiPointShape(content)
	if err != nil {
		return nil, err
	}
	mRange, mArray, err := readIndexedOptionalFloat64RangeAndArray(content, base.NumPoints)
	if err != nil {
		return nil, err
	}
	return &shp.MultiPointM{
		Box:       base.Box,
		NumPoints: base.NumPoints,
		Points:    base.Points,
		MRange:    mRange,
		MArray:    mArray,
	}, nil
}

func readIndexedPartedShape(content *bytes.Reader) (*indexedPartedShape, error) {
	var shape indexedPartedShape
	if err := binary.Read(content, binary.LittleEndian, &shape.Box); err != nil {
		return nil, err
	}
	if err := binary.Read(content, binary.LittleEndian, &shape.NumParts); err != nil {
		return nil, err
	}
	if err := binary.Read(content, binary.LittleEndian, &shape.NumPoints); err != nil {
		return nil, err
	}
	if err := validateIndexedCount("parts", shape.NumParts); err != nil {
		return nil, err
	}
	if err := validateIndexedCount("points", shape.NumPoints); err != nil {
		return nil, err
	}
	if err := ensureIndexedContentBytes(content, int64(shape.NumParts)*4+int64(shape.NumPoints)*16, "part and point arrays"); err != nil {
		return nil, err
	}
	shape.Parts = make([]int32, shape.NumParts)
	shape.Points = make([]shp.Point, shape.NumPoints)
	if err := binary.Read(content, binary.LittleEndian, &shape.Parts); err != nil {
		return nil, err
	}
	if err := binary.Read(content, binary.LittleEndian, &shape.Points); err != nil {
		return nil, err
	}
	return &shape, nil
}

func readIndexedMultiPointShape(content *bytes.Reader) (*indexedMultiPointShape, error) {
	var shape indexedMultiPointShape
	if err := binary.Read(content, binary.LittleEndian, &shape.Box); err != nil {
		return nil, err
	}
	if err := binary.Read(content, binary.LittleEndian, &shape.NumPoints); err != nil {
		return nil, err
	}
	if err := validateIndexedCount("points", shape.NumPoints); err != nil {
		return nil, err
	}
	if err := ensureIndexedContentBytes(content, int64(shape.NumPoints)*16, "point array"); err != nil {
		return nil, err
	}
	shape.Points = make([]shp.Point, shape.NumPoints)
	if err := binary.Read(content, binary.LittleEndian, &shape.Points); err != nil {
		return nil, err
	}
	return &shape, nil
}

func readIndexedFloat64RangeAndArray(content *bytes.Reader, count int32) ([2]float64, []float64, error) {
	var valueRange [2]float64
	if err := validateIndexedCount("float64 values", count); err != nil {
		return valueRange, nil, err
	}
	if err := ensureIndexedContentBytes(content, 16+int64(count)*8, "float64 range and array"); err != nil {
		return valueRange, nil, err
	}
	if err := binary.Read(content, binary.LittleEndian, &valueRange); err != nil {
		return valueRange, nil, err
	}
	values := make([]float64, count)
	if err := binary.Read(content, binary.LittleEndian, &values); err != nil {
		return valueRange, nil, err
	}
	return valueRange, values, nil
}

func readIndexedOptionalFloat64RangeAndArray(content *bytes.Reader, count int32) ([2]float64, []float64, error) {
	var valueRange [2]float64
	if err := validateIndexedCount("float64 values", count); err != nil {
		return valueRange, nil, err
	}
	if content.Len() == 0 {
		return valueRange, make([]float64, count), nil
	}
	return readIndexedFloat64RangeAndArray(content, count)
}

func validateIndexedCount(name string, count int32) error {
	if count < 0 {
		return fmt.Errorf("invalid indexed shape %s count: %d", name, count)
	}
	return nil
}

func ensureIndexedContentBytes(content *bytes.Reader, need int64, label string) error {
	if need < 0 {
		return fmt.Errorf("invalid indexed shape byte count for %s: %d", label, need)
	}
	if int64(content.Len()) < need {
		return fmt.Errorf("short indexed shape content for %s: got %d bytes, want at least %d", label, content.Len(), need)
	}
	return nil
}
