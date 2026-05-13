package shapefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	"github.com/jonas-p/go-shp"
)

type shxEntry struct {
	OffsetBytes int64
	LengthBytes int64
}

func (p *Parser) sampleTableComponentsIndexed(ctx context.Context, components resource.ComponentReader, offset, limit int64, opts *format.ParseOptions) ([]map[string]interface{}, bool, error) {
	rangeReader, ok := components.(resource.ComponentRangeReader)
	if !ok {
		return nil, false, nil
	}
	componentMap := shapefileComponentsByExtension(components.Components())
	shpComponent, hasSHP := componentMap[".shp"]
	shxComponent, hasSHX := componentMap[".shx"]
	dbfComponent, hasDBF := componentMap[".dbf"]
	if !hasSHP || !hasSHX || !hasDBF {
		return nil, false, nil
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		return nil, false, nil
	}
	if limit == 0 {
		return []map[string]interface{}{}, true, nil
	}

	encodingName := ""
	if opts != nil {
		encodingName = opts.Encoding
	}
	if encodingName == "" || NormalizeDBFEncoding(encodingName) == "utf-8" {
		if cpgComponent, ok := componentMap[".cpg"]; ok {
			if text, err := readComponentTextPrefix(ctx, rangeReader, cpgComponent, 256); err == nil && strings.TrimSpace(text) != "" {
				encodingName = NormalizeDBFEncoding(strings.TrimSpace(text))
			}
		}
	}

	dbfHeader, err := readDBFHeaderIndexed(ctx, rangeReader, dbfComponent, encodingName)
	if err != nil {
		return nil, true, err
	}
	entries, err := readSHXWindow(ctx, rangeReader, shxComponent, offset, limit)
	if err != nil {
		return nil, true, err
	}
	if len(entries) == 0 {
		return []map[string]interface{}{}, true, nil
	}
	rows, err := readDBFRecordsIndexed(ctx, rangeReader, dbfComponent, dbfHeader, offset, int64(len(entries)), encodingName)
	if err != nil {
		return nil, true, err
	}
	shapes, err := readShapesIndexed(ctx, rangeReader, shpComponent, entries)
	if err != nil {
		return nil, true, err
	}

	geometryField := p.getGeometryFieldName()
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
				row[geometryField] = wktValue
			} else {
				row[geometryField] = nil
			}
		} else {
			row[geometryField] = nil
		}
	}
	return rows, true, nil
}

func shapefileComponentsByExtension(components []resource.ComponentRef) map[string]resource.ComponentRef {
	result := make(map[string]resource.ComponentRef, len(components))
	for _, component := range components {
		ext := strings.ToLower(filepath.Ext(component.Path))
		if ext == "" {
			continue
		}
		result[ext] = component
	}
	return result
}

func readComponentTextPrefix(ctx context.Context, reader resource.ComponentRangeReader, component resource.ComponentRef, length int64) (string, error) {
	rc, err := reader.OpenComponentRange(ctx, component, 0, length)
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

func readSHXWindow(ctx context.Context, reader resource.ComponentRangeReader, component resource.ComponentRef, offset, limit int64) ([]shxEntry, error) {
	start := int64(100) + offset*8
	length := limit * 8
	rc, err := reader.OpenComponentRange(ctx, component, start, length)
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

func readDBFHeaderIndexed(ctx context.Context, reader resource.ComponentRangeReader, component resource.ComponentRef, encodingName string) (*dbfHeaderInfo, error) {
	rc, err := reader.OpenComponentRange(ctx, component, 0, 32)
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
	rc, err = reader.OpenComponentRange(ctx, component, 0, int64(headerLength))
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

func readDBFRecordsIndexed(ctx context.Context, reader resource.ComponentRangeReader, component resource.ComponentRef, header *dbfHeaderInfo, rowIndex, count int64, encodingName string) ([]map[string]interface{}, error) {
	recordLength := int64(header.RecordLength)
	if count <= 0 {
		return []map[string]interface{}{}, nil
	}
	rc, err := reader.OpenComponentRange(ctx, component, int64(header.HeaderLength)+rowIndex*recordLength, recordLength*count)
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

func readShapesIndexed(ctx context.Context, reader resource.ComponentRangeReader, component resource.ComponentRef, entries []shxEntry) ([]shp.Shape, error) {
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
	rc, err := reader.OpenComponentRange(ctx, component, start, end-start)
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
		return nil, fmt.Errorf("unsupported indexed shapefile shape type: %v", shapeType)
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
