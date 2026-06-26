package spatial

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const (
	GeometryBatchArrowEncodingWKB  = "wkb"
	GeometryBatchArrowEncodingEWKB = "ewkb"

	geometryBatchArrowMetadataPrefix = "addp.geometry."
)

type GeometryBatchArrowOptions struct {
	GeometryColumn   string
	GeometryEncoding string
	SourceCRS        string
	TargetCRS        string
}

type GeometryBatchArrow struct {
	GeometryColumn   string
	GeometryEncoding string
	SourceCRS        string
	TargetCRS        string
	Geometries       [][]byte
}

func EncodeGeometryBatchArrow(geometries [][]byte, opts GeometryBatchArrowOptions) ([]byte, error) {
	geometryColumn := opts.GeometryColumn
	if geometryColumn == "" {
		geometryColumn = "geometry"
	}
	geometryEncoding := normalizeGeometryBatchArrowEncoding(opts.GeometryEncoding)
	if geometryEncoding == "" {
		return nil, fmt.Errorf("unsupported geometry batch encoding %q", opts.GeometryEncoding)
	}
	metadata := arrow.NewMetadata(
		[]string{
			geometryBatchArrowMetadataPrefix + "column",
			geometryBatchArrowMetadataPrefix + "encoding",
			geometryBatchArrowMetadataPrefix + "source_crs",
			geometryBatchArrowMetadataPrefix + "target_crs",
		},
		[]string{
			geometryColumn,
			geometryEncoding,
			opts.SourceCRS,
			opts.TargetCRS,
		},
	)
	schema := arrow.NewSchema([]arrow.Field{
		{Name: geometryColumn, Type: arrow.BinaryTypes.Binary, Nullable: true},
	}, &metadata)

	builder := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer builder.Release()
	binaryBuilder, ok := builder.Field(0).(*array.BinaryBuilder)
	if !ok {
		return nil, fmt.Errorf("geometry batch arrow builder is not binary")
	}
	for _, geometry := range geometries {
		if geometry == nil {
			binaryBuilder.AppendNull()
			continue
		}
		binaryBuilder.Append(geometry)
	}

	record := builder.NewRecord()
	defer record.Release()

	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	if err := writer.Write(record); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("write geometry batch arrow: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close geometry batch arrow writer: %w", err)
	}
	return buf.Bytes(), nil
}

func DecodeGeometryBatchArrow(data []byte) (*GeometryBatchArrow, error) {
	reader, err := ipc.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open geometry batch arrow reader: %w", err)
	}
	defer reader.Release()

	result := &GeometryBatchArrow{}
	for reader.Next() {
		record := reader.Record()
		if record == nil {
			continue
		}
		if result.GeometryColumn == "" && record.Schema() != nil && record.Schema().NumFields() > 0 {
			result.GeometryColumn = record.Schema().Field(0).Name
		}
		if result.GeometryColumn == "" {
			return nil, fmt.Errorf("geometry batch arrow record is missing geometry column")
		}
		if result.GeometryEncoding == "" && record.Schema() != nil && record.Schema().HasMetadata() {
			metadata := record.Schema().Metadata()
			result.GeometryColumn = firstNonEmpty(metadataValue(metadata, geometryBatchArrowMetadataPrefix+"column"), result.GeometryColumn)
			result.GeometryEncoding = normalizeGeometryBatchArrowEncoding(metadataValue(metadata, geometryBatchArrowMetadataPrefix+"encoding"))
			if result.GeometryEncoding == "" {
				return nil, fmt.Errorf("unsupported geometry batch encoding %q", metadataValue(metadata, geometryBatchArrowMetadataPrefix+"encoding"))
			}
			result.SourceCRS = metadataValue(metadata, geometryBatchArrowMetadataPrefix+"source_crs")
			result.TargetCRS = metadataValue(metadata, geometryBatchArrowMetadataPrefix+"target_crs")
		}
		binaryArray, ok := record.Column(0).(*array.Binary)
		if !ok {
			return nil, fmt.Errorf("geometry batch arrow column must be binary")
		}
		for i := 0; i < int(record.NumRows()); i++ {
			if binaryArray.IsNull(i) {
				result.Geometries = append(result.Geometries, nil)
				continue
			}
			value := append([]byte(nil), binaryArray.Value(i)...)
			result.Geometries = append(result.Geometries, value)
		}
		record.Release()
	}
	if err := reader.Err(); err != nil {
		return nil, fmt.Errorf("read geometry batch arrow: %w", err)
	}
	if len(result.Geometries) == 0 && result.GeometryColumn == "" {
		return nil, fmt.Errorf("geometry batch arrow payload is empty")
	}
	if result.GeometryEncoding == "" {
		result.GeometryEncoding = GeometryBatchArrowEncodingEWKB
	}
	if result.GeometryColumn == "" {
		result.GeometryColumn = "geometry"
	}
	return result, nil
}

func NormalizeGeometryBatchArrowEncoding(encoding string) string {
	return normalizeGeometryBatchArrowEncoding(encoding)
}

func normalizeGeometryBatchArrowEncoding(encoding string) string {
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	switch encoding {
	case GeometryBatchArrowEncodingEWKB:
		return GeometryBatchArrowEncodingEWKB
	case GeometryBatchArrowEncodingWKB:
		return GeometryBatchArrowEncodingWKB
	case "":
		return GeometryBatchArrowEncodingEWKB
	default:
		return ""
	}
}

func metadataValue(metadata arrow.Metadata, key string) string {
	value, ok := metadata.GetValue(key)
	if !ok {
		return ""
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
