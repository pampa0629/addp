package parquet

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	flatbuffers "github.com/google/flatbuffers/go"
	parquetgo "github.com/parquet-go/parquet-go"
	parquetfmt "github.com/parquet-go/parquet-go/format"

	"github.com/addp/common/datatype"
)

const arrowSchemaKey = "ARROW:schema"
const maxArrowSchemaBytes = 1 << 20

func parquetTemporalUnit(unit string) parquetgo.TimeUnit {
	switch unit {
	case "us":
		return parquetgo.Microsecond
	case "ns":
		return parquetgo.Nanosecond
	default:
		return parquetgo.Millisecond
	}
}

func temporalNode(field datatype.FieldInfo) parquetgo.Node {
	unit := parquetTemporalUnit(field.Temporal.Unit)
	if field.Type == datatype.FieldTypeTime {
		return parquetgo.TimeAdjusted(unit, false)
	}
	return parquetgo.TimestampAdjusted(unit, field.Temporal.Timezone != "")
}

func temporalWriterValue(value any, field datatype.FieldInfo) (any, error) {
	if value == nil && field.Nullable {
		return nil, nil
	}
	ticks, ok := value.(int64)
	t := field.Temporal
	if !ok || !t.ValidTicks(field.Type, ticks) {
		return nil, fmt.Errorf("invalid native temporal ticks")
	}
	if t.Unit == "s" {
		if ticks < math.MinInt64/1000 || ticks > math.MaxInt64/1000 {
			return nil, fmt.Errorf("timestamp seconds overflow parquet milliseconds")
		}
		ticks *= 1000
	}
	if field.Type == datatype.FieldTypeTime && (t.Unit == "s" || t.Unit == "ms") {
		return int32(ticks), nil
	}
	return ticks, nil
}

func parquetTemporalInfo(logical *parquetfmt.LogicalType) *datatype.TemporalInfo {
	if logical == nil {
		return nil
	}
	var unit parquetfmt.TimeUnit
	info := &datatype.TemporalInfo{}
	switch {
	case logical.Time != nil && !logical.Time.IsAdjustedToUTC:
		unit = logical.Time.Unit
	case logical.Timestamp != nil:
		unit = logical.Timestamp.Unit
		if logical.Timestamp.IsAdjustedToUTC {
			info.Timezone = "UTC"
		}
	default:
		return nil
	}
	switch {
	case unit.Millis != nil:
		info.Unit = "ms"
	case unit.Micros != nil:
		info.Unit = "us"
	case unit.Nanos != nil:
		info.Unit = "ns"
	default:
		return nil
	}
	return info
}

func temporalReaderValue(value parquetgo.Value, field datatype.FieldInfo) (any, error) {
	if value.IsNull() {
		return nil, nil
	}
	var ticks int64
	switch value.Kind() {
	case parquetgo.Int32:
		ticks = int64(value.Int32())
	case parquetgo.Int64:
		ticks = value.Int64()
	default:
		return nil, fmt.Errorf("invalid parquet temporal physical type")
	}
	if !field.Temporal.ValidTicks(field.Type, ticks) {
		return nil, fmt.Errorf("parquet time is outside a day")
	}
	return ticks, nil
}

func arrowWriterSchema(schema *parquetgo.Schema, fields []datatype.FieldInfo) (string, error) {
	byName := make(map[string]datatype.FieldInfo, len(fields))
	hasZone := false
	for _, field := range fields {
		byName[field.Name] = field
		hasZone = hasZone || (field.Temporal != nil && field.Temporal.Timezone != "")
	}
	if !hasZone {
		return "", nil
	}
	arrowFields := make([]arrow.Field, 0, len(fields))
	for _, column := range schema.Fields() {
		field := byName[column.Name()]
		typ := arrowWriterType(field, column)
		if typ == nil {
			return "", fmt.Errorf("cannot represent field %q in parquet Arrow schema", field.Name)
		}
		arrowFields = append(arrowFields, arrow.Field{Name: field.Name, Type: typ, Nullable: field.Nullable})
	}
	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf, ipc.WithSchema(arrow.NewSchema(arrowFields, nil)))
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("encode parquet Arrow schema: %w", err)
	}
	data := buf.Bytes()
	// Parquet stores the framed IPC schema message, without the stream EOS.
	messageSize := int(binary.LittleEndian.Uint32(data[4:8])) + 8
	if messageSize > maxArrowSchemaBytes {
		return "", fmt.Errorf("parquet Arrow schema exceeds metadata limit")
	}
	return base64.StdEncoding.EncodeToString(data[:messageSize]), nil
}

func arrowWriterType(field datatype.FieldInfo, column parquetgo.Field) arrow.DataType {
	logical := column.Type().LogicalType()
	if temporal := parquetTemporalInfo(logical); temporal != nil {
		unit := map[string]arrow.TimeUnit{"ms": arrow.Millisecond, "us": arrow.Microsecond, "ns": arrow.Nanosecond}[temporal.Unit]
		if field.Type == datatype.FieldTypeTimestamp {
			return &arrow.TimestampType{Unit: unit, TimeZone: field.Temporal.Timezone}
		}
		if unit == arrow.Millisecond {
			return &arrow.Time32Type{Unit: unit}
		}
		return &arrow.Time64Type{Unit: unit}
	}
	switch field.Type {
	case datatype.FieldTypeDate:
		return arrow.FixedWidthTypes.Date32
	case datatype.FieldTypeDecimal:
		switch {
		case field.Precision <= 38:
			return &arrow.Decimal128Type{Precision: int32(field.Precision), Scale: int32(field.Scale)}
		case field.Precision <= 76:
			return &arrow.Decimal256Type{Precision: int32(field.Precision), Scale: int32(field.Scale)}
		default:
			return nil
		}
	case datatype.FieldTypeUUID:
		return &arrow.FixedSizeBinaryType{ByteWidth: 16}
	}
	switch column.Type().Kind() {
	case parquetgo.Boolean:
		return arrow.FixedWidthTypes.Boolean
	case parquetgo.Int32:
		return arrow.PrimitiveTypes.Int32
	case parquetgo.Int64:
		return arrow.PrimitiveTypes.Int64
	case parquetgo.Float:
		return arrow.PrimitiveTypes.Float32
	case parquetgo.Double:
		return arrow.PrimitiveTypes.Float64
	case parquetgo.ByteArray:
		if logical != nil && logical.UTF8 != nil {
			return arrow.BinaryTypes.String
		}
		return arrow.BinaryTypes.Binary
	default:
		return nil
	}
}

func parquetFileFields(file *parquetgo.File) ([]datatype.FieldInfo, error) {
	fields := extractFields(file.Schema())
	adjusted := make(map[string]*datatype.TemporalInfo)
	for _, field := range fields {
		if field.Type == datatype.FieldTypeTimestamp && field.Temporal != nil && field.Temporal.Timezone != "" {
			adjusted[field.Name] = field.Temporal
		}
	}
	if encoded, ok := file.Lookup(arrowSchemaKey); ok && len(adjusted) != 0 {
		if err := readArrowTimezones(encoded, adjusted); err != nil {
			return nil, err
		}
	}
	return fields, nil
}

// readArrowTimezones reads only top-level timestamp facts from the standard IPC
// schema. It never materializes arbitrary Arrow types, metadata, or record data.
func readArrowTimezones(encoded string, adjusted map[string]*datatype.TemporalInfo) (err error) {
	defer func() {
		// FlatBuffers accessors panic on out-of-bounds offsets.
		if recover() != nil {
			err = fmt.Errorf("invalid parquet Arrow schema")
		}
	}()
	if len(encoded) > base64.StdEncoding.EncodedLen(maxArrowSchemaBytes) {
		return fmt.Errorf("parquet Arrow schema exceeds metadata limit")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) < 8 {
		return fmt.Errorf("invalid parquet Arrow schema")
	}
	// Both legacy and continuation-marker IPC message framing occur in Parquet.
	size, body := binary.LittleEndian.Uint32(data), data[4:]
	if size == math.MaxUint32 {
		size, body = binary.LittleEndian.Uint32(body), body[4:]
	}
	if int(size) != len(body) || len(body) < 4 {
		return fmt.Errorf("invalid parquet Arrow schema frame")
	}
	// Vtable slots and enum discriminants are from Arrow IPC Message.fbs and
	// Schema.fbs. Keep traversal flat and bounded by the actual schema bytes.
	const (
		messageHeaderTypeSlot = 6
		messageHeaderSlot     = 8
		schemaMessage         = 1
		schemaFieldsSlot      = 6
		fieldNameSlot         = 4
		fieldTypeKindSlot     = 8
		fieldTypeSlot         = 10
		timestampKind         = 10
		timestampUnitSlot     = 4
		timestampZoneSlot     = 6
	)
	message := flatbuffers.Table{Bytes: body, Pos: flatbuffers.GetUOffsetT(body)}
	header := message.Offset(messageHeaderSlot)
	if message.GetByteSlot(messageHeaderTypeSlot, 0) != schemaMessage || header == 0 {
		return fmt.Errorf("parquet Arrow metadata is not a schema message")
	}
	var schema flatbuffers.Table
	message.Union(&schema, flatbuffers.UOffsetT(header))
	fieldVector := flatbuffers.UOffsetT(schema.Offset(schemaFieldsSlot))
	if fieldVector == 0 {
		return fmt.Errorf("parquet Arrow schema has no fields")
	}
	start, count := schema.Vector(fieldVector), schema.VectorLen(fieldVector)
	if int(start) > len(body) || count > (len(body)-int(start))/4 {
		return fmt.Errorf("invalid parquet Arrow field vector")
	}
	seen := make(map[string]bool, len(adjusted))
	for i := 0; i < count; i++ {
		field := flatbuffers.Table{Bytes: body, Pos: schema.Indirect(start + flatbuffers.UOffsetT(4*i))}
		if field.GetByteSlot(fieldTypeKindSlot, 0) != timestampKind {
			continue
		}
		nameOffset := flatbuffers.UOffsetT(field.Offset(fieldNameSlot))
		if nameOffset == 0 {
			continue
		}
		name := field.ByteVector(field.Pos + nameOffset)
		if len(name) > 1024 {
			continue
		}
		temporal := adjusted[string(name)]
		if temporal == nil {
			continue
		}
		if seen[string(name)] {
			return fmt.Errorf("parquet Arrow schema repeats a timestamp field")
		}
		seen[string(name)] = true
		typeOffset := flatbuffers.UOffsetT(field.Offset(fieldTypeSlot))
		if typeOffset == 0 {
			continue
		}
		var timestamp flatbuffers.Table
		field.Union(&timestamp, typeOffset)
		unit := timestamp.GetInt16Slot(timestampUnitSlot, 0)
		zoneOffset := flatbuffers.UOffsetT(timestamp.Offset(timestampZoneSlot))
		if zoneOffset == 0 {
			continue
		}
		zone := timestamp.String(timestamp.Pos + zoneOffset)
		if zone == "" {
			continue
		}
		units := [...]string{"s", "ms", "us", "ns"}
		if unit < 0 || int(unit) >= len(units) || (units[unit] != temporal.Unit && !(unit == 0 && temporal.Unit == "ms")) {
			return fmt.Errorf("parquet Arrow timestamp unit contradicts parquet schema")
		}
		declared := datatype.TemporalInfo{Unit: temporal.Unit, Timezone: zone}
		if !declared.ValidFor(datatype.FieldTypeTimestamp) {
			return fmt.Errorf("invalid parquet Arrow timestamp timezone")
		}
		temporal.Timezone = zone
	}
	return nil
}
