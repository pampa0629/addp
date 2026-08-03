package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/addp/common/datatype"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type BatchArrowStreamWriter struct {
	fields []datatype.FieldInfo
	schema *arrow.Schema
	writer *ipc.Writer
}

func NewBatchArrowStreamWriter(destination io.Writer, fields []datatype.FieldInfo) (*BatchArrowStreamWriter, error) {
	if destination == nil || len(fields) == 0 {
		return nil, fmt.Errorf("Arrow stream destination and fields are required")
	}
	arrowFields := make([]arrow.Field, len(fields))
	for index, field := range fields {
		if field.Name == "" {
			return nil, fmt.Errorf("Arrow field %d has no name", index)
		}
		arrowFields[index] = arrow.Field{Name: field.Name, Type: arrowDataType(field.Type), Nullable: field.Nullable}
	}
	schema := arrow.NewSchema(arrowFields, nil)
	return &BatchArrowStreamWriter{
		fields: append([]datatype.FieldInfo(nil), fields...),
		schema: schema,
		writer: ipc.NewWriter(destination, ipc.WithSchema(schema)),
	}, nil
}

func (w *BatchArrowStreamWriter) WriteBatch(batch *BatchData) error {
	if w == nil || w.writer == nil || batch == nil {
		return fmt.Errorf("Arrow stream writer and batch are required")
	}
	if len(batch.Fields) != len(w.fields) {
		return fmt.Errorf("Arrow batch field count changed during stream")
	}
	for index := range w.fields {
		if batch.Fields[index].Name != w.fields[index].Name || batch.Fields[index].Type != w.fields[index].Type {
			return fmt.Errorf("Arrow batch schema changed at field %d", index)
		}
	}
	builder := array.NewRecordBuilder(memory.NewGoAllocator(), w.schema)
	defer builder.Release()
	for _, row := range batch.Rows {
		for index, field := range w.fields {
			if err := appendArrowValue(builder.Field(index), field.Type, row[field.Name]); err != nil {
				return fmt.Errorf("encode Arrow field %q: %w", field.Name, err)
			}
		}
	}
	record := builder.NewRecord()
	defer record.Release()
	return w.writer.Write(record)
}

func (w *BatchArrowStreamWriter) Close() error {
	if w == nil || w.writer == nil {
		return nil
	}
	err := w.writer.Close()
	w.writer = nil
	return err
}

func arrowDataType(fieldType datatype.FieldType) arrow.DataType {
	switch fieldType {
	case datatype.FieldTypeBool:
		return arrow.FixedWidthTypes.Boolean
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		return arrow.PrimitiveTypes.Int64
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble:
		return arrow.PrimitiveTypes.Float64
	case datatype.FieldTypeBytes, datatype.FieldTypeGeometry:
		return arrow.BinaryTypes.Binary
	case datatype.FieldTypeDate:
		return arrow.FixedWidthTypes.Date32
	case datatype.FieldTypeTimestamp:
		return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	default:
		return arrow.BinaryTypes.String
	}
}

func appendArrowValue(builder array.Builder, fieldType datatype.FieldType, value any) error {
	if value == nil {
		builder.AppendNull()
		return nil
	}
	switch typed := builder.(type) {
	case *array.BooleanBuilder:
		parsed, ok := value.(bool)
		if !ok {
			return fmt.Errorf("expected bool, got %T", value)
		}
		typed.Append(parsed)
	case *array.Int64Builder:
		parsed, err := arrowInt64(value)
		if err != nil {
			return err
		}
		typed.Append(parsed)
	case *array.Float64Builder:
		parsed, err := arrowFloat64(value)
		if err != nil {
			return err
		}
		typed.Append(parsed)
	case *array.BinaryBuilder:
		switch parsed := value.(type) {
		case []byte:
			typed.Append(parsed)
		case string:
			typed.Append([]byte(parsed))
		default:
			return fmt.Errorf("expected bytes, got %T", value)
		}
	case *array.Date32Builder:
		parsed, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("expected time.Time date, got %T", value)
		}
		typed.Append(arrow.Date32FromTime(parsed))
	case *array.TimestampBuilder:
		parsed, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("expected time.Time timestamp, got %T", value)
		}
		stamp, err := arrow.TimestampFromTime(parsed.UTC(), arrow.Microsecond)
		if err != nil {
			return err
		}
		typed.Append(stamp)
	case *array.StringBuilder:
		text, err := arrowString(fieldType, value)
		if err != nil {
			return err
		}
		typed.Append(text)
	default:
		return fmt.Errorf("unsupported Arrow builder %T", builder)
	}
	return nil
}

func arrowInt64(value any) (int64, error) {
	switch parsed := value.(type) {
	case int:
		return int64(parsed), nil
	case int8:
		return int64(parsed), nil
	case int16:
		return int64(parsed), nil
	case int32:
		return int64(parsed), nil
	case int64:
		return parsed, nil
	case uint:
		return int64(parsed), nil
	case uint8:
		return int64(parsed), nil
	case uint16:
		return int64(parsed), nil
	case uint32:
		return int64(parsed), nil
	case string:
		return strconv.ParseInt(parsed, 10, 64)
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func arrowFloat64(value any) (float64, error) {
	switch parsed := value.(type) {
	case float32:
		return float64(parsed), nil
	case float64:
		return parsed, nil
	case int64:
		return float64(parsed), nil
	case string:
		return strconv.ParseFloat(parsed, 64)
	default:
		return 0, fmt.Errorf("expected number, got %T", value)
	}
}

func arrowString(fieldType datatype.FieldType, value any) (string, error) {
	if fieldType == datatype.FieldTypeTime {
		if parsed, ok := value.(time.Time); ok {
			return parsed.Format("15:04:05.999999999Z07:00"), nil
		}
	}
	switch parsed := value.(type) {
	case string:
		return parsed, nil
	case []byte:
		return string(parsed), nil
	case fmt.Stringer:
		return parsed.String(), nil
	}
	if fieldType == datatype.FieldTypeJSON || fieldType == datatype.FieldTypeArray ||
		fieldType == datatype.FieldTypeMixed || fieldType == datatype.FieldTypeUnknown {
		encoded, err := json.Marshal(value)
		return string(encoded), err
	}
	return fmt.Sprint(value), nil
}
