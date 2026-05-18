package shapefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/jonas-p/go-shp"
)

type shxEntry struct {
	OffsetBytes int64
	LengthBytes int64
}

var errUnsupportedIndexedShapeType = errors.New("unsupported indexed shapefile shape type")

func isIndexedSampleFallbackError(err error) bool {
	return errors.Is(err, errUnsupportedIndexedShapeType)
}

func (plugin *Plugin) sampleMultiTableIndexed(ctx context.Context, refs contentio.MultiReader, offset, limit int64, opts *format.ParseOptions) ([]map[string]interface{}, bool, error) {
	rangeReader, ok := refs.(contentio.MultiRangeReader)
	if !ok {
		return nil, false, nil
	}
	source, ok, err := newIndexedMultiTableReadSource(ctx, plugin, refs.Refs(), rangeReader, opts)
	if err != nil || !ok {
		return nil, ok, err
	}
	return source.readRows(ctx, offset, limit)
}

type indexedMultiTableReadSource struct {
	plugin        *Plugin
	rangeReader   contentio.MultiRangeReader
	shpRef        contentio.Ref
	shxRef        contentio.Ref
	dbfRef        contentio.Ref
	dbfHeader     *dbfHeaderInfo
	encodingName  string
	geometryField string
}

func newIndexedMultiTableReadSource(ctx context.Context, plugin *Plugin, refs []contentio.Ref, rangeReader contentio.MultiRangeReader, opts *format.ParseOptions) (*indexedMultiTableReadSource, bool, error) {
	refMap := shapefileRefsByExtension(refs)
	shpRef, hasSHP := refMap[".shp"]
	shxRef, hasSHX := refMap[".shx"]
	dbfRef, hasDBF := refMap[".dbf"]
	if !hasSHP || !hasSHX || !hasDBF {
		return nil, false, nil
	}

	encodingName := ""
	if opts != nil {
		encodingName = opts.Encoding
	}
	if encodingName == "" || NormalizeDBFEncoding(encodingName) == "utf-8" {
		if cpgRef, ok := refMap[".cpg"]; ok {
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
		geometryField: plugin.getGeometryFieldName(),
	}, true, nil
}

func (s *indexedMultiTableReadSource) describeTable(ctx context.Context, refs []contentio.Ref, opts *format.ParseOptions) (*format.TableInfo, error) {
	shpHeader, err := readSHPHeaderIndexed(ctx, s.rangeReader, s.shpRef)
	if err != nil {
		return nil, err
	}
	encodingName := s.encodingName
	if opts != nil && opts.Encoding != "" {
		encodingName = opts.Encoding
	}
	geomType := determineShapefileGeometryType(shpHeader.ShapeType)
	fields := make([]format.FieldInfo, 0, len(s.dbfHeader.Fields)+1)
	fields = append(fields, format.FieldInfo{
		Name:         s.geometryField,
		Type:         format.FieldTypeGeometry,
		OriginalType: geomType,
		Nullable:     false,
		IsPrimaryKey: false,
		Comment:      "Shapefile geometry field",
	})
	mapper := format.GetTypeMapper("shapefile")
	for _, field := range s.dbfHeader.Fields {
		fieldType := format.FieldTypeUnknown
		if mapper != nil {
			fieldType = mapper.ToCommon(field.RawType)
		}
		fields = append(fields, format.FieldInfo{
			Name:         field.Name,
			Type:         fieldType,
			OriginalType: field.RawType,
			Nullable:     true,
			Size:         field.Size,
			Precision:    field.Precision,
		})
	}

	rowCount := int64(s.dbfHeader.RecordCount)
	spatialInfo := &format.SpatialInfo{
		GeometryColumn: s.geometryField,
		GeometryType:   geomType,
		SRID:           0,
		BoundingBox:    &shpHeader.BBox,
		Dimension:      2,
	}
	if opts != nil && opts.SpatialRefSys != "" {
		if srid := commonSpatial.ParseSRID(opts.SpatialRefSys); srid > 0 {
			spatialInfo.SRID = srid
		}
	}
	refMap := shapefileRefsByExtension(refs)
	shapefileInfo := &FormatInfo{
		Encoding:   NormalizeDBFEncoding(encodingName),
		ShapeType:  geomType,
		HasPRJ:     hasRefExtension(refMap, ".prj"),
		HasCPG:     hasRefExtension(refMap, ".cpg"),
		DBFVersion: s.dbfHeader.Version,
	}
	info := &Info{
		BaseName:      strings.TrimSuffix(filepath.Base(s.shpRef.Path), filepath.Ext(s.shpRef.Path)),
		RefExtensions: refExtensions(refs),
		HasPRJ:        shapefileInfo.HasPRJ,
		HasCPG:        shapefileInfo.HasCPG,
		ShapeType:     geomType,
		DBFVersion:    s.dbfHeader.Version,
		Encoding:      shapefileInfo.Encoding,
	}
	return &format.TableInfo{
		Name:        "shapefile_data",
		RowCount:    &rowCount,
		Fields:      fields,
		PrimaryKey:  []string{},
		SpatialInfo: spatialInfo,
		FormatInfo:  map[string]interface{}{"shapefile": mergeFormatInfo(shapefileInfo.FormatAttributes(), info.FormatAttributes())},
	}, nil
}

func (s *indexedMultiTableReadSource) readRows(ctx context.Context, offset, limit int64) ([]map[string]interface{}, bool, error) {
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
			if wktValue, err := ShapeToWKT(shape); err == nil {
				row[s.geometryField] = wktValue
			} else {
				row[s.geometryField] = nil
			}
		} else {
			row[s.geometryField] = nil
		}
	}
	return rows, true, nil
}

func shapefileRefsByExtension(refs []contentio.Ref) map[string]contentio.Ref {
	result := make(map[string]contentio.Ref, len(refs))
	for _, ref := range refs {
		ext := strings.ToLower(filepath.Ext(ref.Path))
		if ext == "" {
			continue
		}
		result[ext] = ref
	}
	return result
}

func readRefTextPrefix(ctx context.Context, reader contentio.MultiRangeReader, ref contentio.Ref, length int64) (string, error) {
	rc, err := reader.OpenRange(ctx, ref, 0, length)
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

func hasRefExtension(refs map[string]contentio.Ref, ext string) bool {
	_, ok := refs[strings.ToLower(ext)]
	return ok
}

func readSHPHeaderIndexed(ctx context.Context, reader contentio.MultiRangeReader, ref contentio.Ref) (*shpHeaderInfo, error) {
	rc, err := reader.OpenRange(ctx, ref, 32, 68)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	if len(data) < 68 {
		return nil, fmt.Errorf("invalid SHP header length: %d", len(data)+32)
	}
	shapeType := shp.ShapeType(binary.LittleEndian.Uint32(data[0:4]))
	var bbox [4]float64
	for i := range bbox {
		start := 4 + i*8
		bbox[i] = math.Float64frombits(binary.LittleEndian.Uint64(data[start : start+8]))
	}
	return &shpHeaderInfo{ShapeType: shapeType, BBox: bbox}, nil
}

func readSHXWindow(ctx context.Context, reader contentio.MultiRangeReader, ref contentio.Ref, offset, limit int64) ([]shxEntry, error) {
	start := int64(100) + offset*8
	length := limit * 8
	rc, err := reader.OpenRange(ctx, ref, start, length)
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

func readDBFHeaderIndexed(ctx context.Context, reader contentio.MultiRangeReader, ref contentio.Ref, encodingName string) (*dbfHeaderInfo, error) {
	rc, err := reader.OpenRange(ctx, ref, 0, 32)
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
	rc, err = reader.OpenRange(ctx, ref, 0, int64(headerLength))
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

func parseDBFHeaderBytes(data []byte, encodingName string) (*dbfHeaderInfo, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("invalid DBF header length: %d", len(data))
	}
	headerLength := int(binary.LittleEndian.Uint16(data[8:10]))
	if headerLength < 33 || len(data) < headerLength {
		return nil, fmt.Errorf("invalid DBF header length: %d", headerLength)
	}
	fieldCount := (headerLength - 33) / 32
	fields := make([]FieldInfo, 0, fieldCount)
	for i := 0; i < fieldCount; i++ {
		start := 32 + i*32
		desc := data[start : start+32]
		var name [11]byte
		copy(name[:], desc[0:11])
		fieldType := desc[11]
		fields = append(fields, FieldInfo{
			Name:      decodeDBFName(name, encodingName),
			Type:      DecodeDBFFieldType(fieldType),
			RawType:   string(fieldType),
			Size:      int(desc[16]),
			Precision: int(desc[17]),
		})
	}
	return &dbfHeaderInfo{
		Version:      data[0],
		RecordCount:  int32(binary.LittleEndian.Uint32(data[4:8])),
		HeaderLength: headerLength,
		RecordLength: int(binary.LittleEndian.Uint16(data[10:12])),
		Fields:       fields,
	}, nil
}

func readDBFRecordsIndexed(ctx context.Context, reader contentio.MultiRangeReader, ref contentio.Ref, header *dbfHeaderInfo, rowIndex, count int64, encodingName string) ([]map[string]interface{}, error) {
	recordLength := int64(header.RecordLength)
	if count <= 0 {
		return []map[string]interface{}{}, nil
	}
	rc, err := reader.OpenRange(ctx, ref, int64(header.HeaderLength)+rowIndex*recordLength, recordLength*count)
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
			row[field.Name] = ParseDBFAttribute([]byte(field.RawType)[0], raw)
		}
		pos += field.Size
	}
	return row, nil
}

func readShapesIndexed(ctx context.Context, reader contentio.MultiRangeReader, ref contentio.Ref, entries []shxEntry) ([]shp.Shape, error) {
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
	rc, err := reader.OpenRange(ctx, ref, start, end-start)
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

func parseShapeContent(shapeType shp.ShapeType, content *bytes.Reader) (shp.Shape, error) {
	switch shapeType {
	case shp.NULL:
		return &shp.Null{}, nil
	case shp.POINT:
		var point shp.Point
		if err := binary.Read(content, binary.LittleEndian, &point); err != nil {
			return nil, err
		}
		return &point, nil
	case shp.POLYLINE:
		return readPolyLineContent(content)
	case shp.POLYGON:
		line, err := readPolyLineContent(content)
		if err != nil {
			return nil, err
		}
		polygon := shp.Polygon(*line)
		return &polygon, nil
	case shp.MULTIPOINT:
		return readMultiPointContent(content)
	default:
		return nil, fmt.Errorf("%w: %v", errUnsupportedIndexedShapeType, shapeType)
	}
}

func readPolyLineContent(content *bytes.Reader) (*shp.PolyLine, error) {
	var line shp.PolyLine
	if err := binary.Read(content, binary.LittleEndian, &line.Box); err != nil {
		return nil, err
	}
	if err := binary.Read(content, binary.LittleEndian, &line.NumParts); err != nil {
		return nil, err
	}
	if err := binary.Read(content, binary.LittleEndian, &line.NumPoints); err != nil {
		return nil, err
	}
	line.Parts = make([]int32, line.NumParts)
	line.Points = make([]shp.Point, line.NumPoints)
	if err := binary.Read(content, binary.LittleEndian, &line.Parts); err != nil {
		return nil, err
	}
	if err := binary.Read(content, binary.LittleEndian, &line.Points); err != nil {
		return nil, err
	}
	return &line, nil
}

func readMultiPointContent(content *bytes.Reader) (*shp.MultiPoint, error) {
	var multi shp.MultiPoint
	if err := binary.Read(content, binary.LittleEndian, &multi.Box); err != nil {
		return nil, err
	}
	if err := binary.Read(content, binary.LittleEndian, &multi.NumPoints); err != nil {
		return nil, err
	}
	multi.Points = make([]shp.Point, multi.NumPoints)
	if err := binary.Read(content, binary.LittleEndian, &multi.Points); err != nil {
		return nil, err
	}
	return &multi, nil
}
