package spatial

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/gogama/flatgeobuf/flatgeobuf"
	"github.com/gogama/flatgeobuf/flatgeobuf/flat"
	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/twpayne/go-geom"
)

type FlatGeobufPropertyType string

const (
	FlatGeobufPropertyBool    FlatGeobufPropertyType = "bool"
	FlatGeobufPropertyInt64   FlatGeobufPropertyType = "int64"
	FlatGeobufPropertyFloat64 FlatGeobufPropertyType = "float64"
	FlatGeobufPropertyString  FlatGeobufPropertyType = "string"
	FlatGeobufPropertyJSON    FlatGeobufPropertyType = "json"
	FlatGeobufPropertyBinary  FlatGeobufPropertyType = "binary"
)

type FlatGeobufColumn struct {
	Name string
	Type FlatGeobufPropertyType
}

type FlatGeobufFeature struct {
	Geometry         interface{}
	GeometryEncoding string
	GeometrySRID     int
	Properties       map[string]interface{}
}

type FlatGeobufOptions struct {
	Name            string
	SRID            int
	CRSName         string
	CRSWKT          string
	Columns         []FlatGeobufColumn
	GeometryType    string
	FeatureCount    uint64
	DefaultEncoding string
}

type FlatGeobufFeatureReader interface {
	NextFlatGeobufFeature(ctx context.Context) (*FlatGeobufFeature, error)
}

// WriteFlatGeobuf writes a FlatGeobuf feature stream. The first implementation
// intentionally writes an unindexed file: direct quick view is budget-limited
// and reads the whole material, while indexed/range FlatGeobuf can be added as
// a separate optimization without changing the row encoding contract.
func WriteFlatGeobuf(ctx context.Context, w io.Writer, reader FlatGeobufFeatureReader, opts FlatGeobufOptions) error {
	if w == nil {
		return fmt.Errorf("FlatGeobuf writer is nil")
	}
	if reader == nil {
		return fmt.Errorf("FlatGeobuf feature reader is nil")
	}
	defaultEncoding := opts.DefaultEncoding
	if defaultEncoding == "" {
		defaultEncoding = string(GeometryEncodingEWKB)
	}
	if NormalizeGeometryEncoding(defaultEncoding) == "" {
		return fmt.Errorf("unsupported default geometry encoding %q", defaultEncoding)
	}

	fileWriter := flatgeobuf.NewFileWriter(w)
	header, err := buildFlatGeobufHeader(opts)
	if err != nil {
		return err
	}
	if _, err := fileWriter.Header(header); err != nil {
		_ = fileWriter.Close()
		return fmt.Errorf("write FlatGeobuf header: %w", err)
	}

	for {
		if err := ctx.Err(); err != nil {
			_ = fileWriter.Close()
			return err
		}
		feature, err := reader.NextFlatGeobufFeature(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			_ = fileWriter.Close()
			return fmt.Errorf("read FlatGeobuf feature: %w", err)
		}
		if feature == nil {
			break
		}
		if feature.GeometryEncoding == "" {
			feature.GeometryEncoding = defaultEncoding
		}
		if feature.GeometrySRID == 0 {
			feature.GeometrySRID = opts.SRID
		}
		flatFeature, err := buildFlatGeobufFeature(*feature, opts.Columns, flatGeobufHeaderGeometryType(opts.GeometryType))
		if err != nil {
			_ = fileWriter.Close()
			return err
		}
		if _, err := fileWriter.Data([]flat.Feature{*flatFeature}); err != nil {
			_ = fileWriter.Close()
			return fmt.Errorf("write FlatGeobuf feature: %w", err)
		}
	}
	if err := fileWriter.Close(); err != nil {
		return fmt.Errorf("close FlatGeobuf writer: %w", err)
	}
	return nil
}

type flatGeobufGeometry struct {
	geometryType flat.GeometryType
	xy           []float64
	z            []float64
	m            []float64
	ends         []uint32
	parts        []flatGeobufGeometry
}

func buildFlatGeobufHeader(opts FlatGeobufOptions) (*flat.Header, error) {
	builder := flatbuffers.NewBuilder(1024)

	var nameOffset flatbuffers.UOffsetT
	if opts.Name != "" {
		nameOffset = builder.CreateString(opts.Name)
	}
	columnsOffset, err := buildFlatGeobufColumns(builder, opts.Columns, flat.HeaderStartColumnsVector)
	if err != nil {
		return nil, err
	}
	crsOffset := buildFlatGeobufCRS(builder, opts)

	flat.HeaderStart(builder)
	if nameOffset > 0 {
		flat.HeaderAddName(builder, nameOffset)
	}
	flat.HeaderAddIndexNodeSize(builder, 0)
	flat.HeaderAddGeometryType(builder, flatGeobufHeaderGeometryType(opts.GeometryType))
	if opts.FeatureCount > 0 {
		flat.HeaderAddFeaturesCount(builder, opts.FeatureCount)
	}
	if columnsOffset > 0 {
		flat.HeaderAddColumns(builder, columnsOffset)
	}
	if crsOffset > 0 {
		flat.HeaderAddCrs(builder, crsOffset)
	}
	headerOffset := flat.HeaderEnd(builder)
	flat.FinishSizePrefixedHeaderBuffer(builder, headerOffset)
	headerBytes := alignFlatGeobufSizePrefixedHeader(builder.FinishedBytes())
	return flat.GetSizePrefixedRootAsHeader(headerBytes, 0), nil
}

func alignFlatGeobufSizePrefixedHeader(headerBytes []byte) []byte {
	const magicLength = 8
	padding := (flatbuffers.SizeFloat64 - ((magicLength + len(headerBytes)) % flatbuffers.SizeFloat64)) % flatbuffers.SizeFloat64
	if padding == 0 {
		return headerBytes
	}
	aligned := make([]byte, len(headerBytes)+padding)
	copy(aligned, headerBytes)
	headerLength := binary.LittleEndian.Uint32(aligned[:flatbuffers.SizeUint32])
	binary.LittleEndian.PutUint32(aligned[:flatbuffers.SizeUint32], headerLength+uint32(padding))
	return aligned
}

func buildFlatGeobufCRS(builder *flatbuffers.Builder, opts FlatGeobufOptions) flatbuffers.UOffsetT {
	if opts.SRID <= 0 && opts.CRSName == "" && opts.CRSWKT == "" {
		return 0
	}
	var orgOffset, nameOffset, wktOffset, codeStringOffset flatbuffers.UOffsetT
	if opts.SRID > 0 {
		orgOffset = builder.CreateString("EPSG")
		codeStringOffset = builder.CreateString(fmt.Sprintf("%d", opts.SRID))
	}
	if opts.CRSName != "" {
		nameOffset = builder.CreateString(opts.CRSName)
	}
	if opts.CRSWKT != "" {
		wktOffset = builder.CreateString(opts.CRSWKT)
	}
	flat.CrsStart(builder)
	if orgOffset > 0 {
		flat.CrsAddOrg(builder, orgOffset)
	}
	if opts.SRID > 0 && opts.SRID <= math.MaxInt32 {
		flat.CrsAddCode(builder, int32(opts.SRID))
	}
	if codeStringOffset > 0 {
		flat.CrsAddCodeString(builder, codeStringOffset)
	}
	if nameOffset > 0 {
		flat.CrsAddName(builder, nameOffset)
	}
	if wktOffset > 0 {
		flat.CrsAddWkt(builder, wktOffset)
	}
	return flat.CrsEnd(builder)
}

func buildFlatGeobufColumns(builder *flatbuffers.Builder, columns []FlatGeobufColumn, startVector func(*flatbuffers.Builder, int) flatbuffers.UOffsetT) (flatbuffers.UOffsetT, error) {
	if len(columns) == 0 {
		return 0, nil
	}
	offsets := make([]flatbuffers.UOffsetT, len(columns))
	for i, column := range columns {
		if column.Name == "" {
			return 0, fmt.Errorf("FlatGeobuf column[%d] name is empty", i)
		}
		columnType, err := flatGeobufColumnType(column.Type)
		if err != nil {
			return 0, fmt.Errorf("FlatGeobuf column %q: %w", column.Name, err)
		}
		nameOffset := builder.CreateString(column.Name)
		flat.ColumnStart(builder)
		flat.ColumnAddName(builder, nameOffset)
		flat.ColumnAddType(builder, columnType)
		offsets[i] = flat.ColumnEnd(builder)
	}
	startVector(builder, len(offsets))
	for i := len(offsets) - 1; i >= 0; i-- {
		builder.PrependUOffsetT(offsets[i])
	}
	return builder.EndVector(len(offsets)), nil
}

func buildFlatGeobufFeature(feature FlatGeobufFeature, columns []FlatGeobufColumn, headerGeometryType flat.GeometryType) (*flat.Feature, error) {
	geometry, err := DecodeGeometryValue(feature.Geometry, feature.GeometryEncoding, feature.GeometrySRID)
	if err != nil {
		return nil, fmt.Errorf("decode FlatGeobuf feature geometry: %w", err)
	}
	flatGeometry, err := flatGeobufGeometryFromGeom(geometry)
	if err != nil {
		return nil, err
	}
	flatGeometry = alignFlatGeobufGeometryToHeader(flatGeometry, headerGeometryType)

	builder := flatbuffers.NewBuilder(2048)
	geometryOffset, err := buildFlatGeobufGeometry(builder, flatGeometry)
	if err != nil {
		return nil, err
	}
	properties, err := encodeFlatGeobufProperties(feature.Properties, columns)
	if err != nil {
		return nil, err
	}
	var propertiesOffset flatbuffers.UOffsetT
	if len(properties) > 0 {
		propertiesOffset = builder.CreateByteVector(properties)
	}

	flat.FeatureStart(builder)
	flat.FeatureAddGeometry(builder, geometryOffset)
	if propertiesOffset > 0 {
		flat.FeatureAddProperties(builder, propertiesOffset)
	}
	featureOffset := flat.FeatureEnd(builder)
	flat.FinishSizePrefixedFeatureBuffer(builder, featureOffset)
	return flat.GetSizePrefixedRootAsFeature(builder.FinishedBytes(), 0), nil
}

func buildFlatGeobufGeometry(builder *flatbuffers.Builder, geometry flatGeobufGeometry) (flatbuffers.UOffsetT, error) {
	var xyOffset, zOffset, mOffset, endsOffset, partsOffset flatbuffers.UOffsetT
	if len(geometry.xy) > 0 {
		xyOffset = createFloat64Vector(builder, geometry.xy)
	}
	if len(geometry.z) > 0 {
		zOffset = createFloat64Vector(builder, geometry.z)
	}
	if len(geometry.m) > 0 {
		mOffset = createFloat64Vector(builder, geometry.m)
	}
	if len(geometry.ends) > 0 {
		endsOffset = createUint32Vector(builder, geometry.ends)
	}
	if len(geometry.parts) > 0 {
		partOffsets := make([]flatbuffers.UOffsetT, len(geometry.parts))
		for i := range geometry.parts {
			offset, err := buildFlatGeobufGeometry(builder, geometry.parts[i])
			if err != nil {
				return 0, err
			}
			partOffsets[i] = offset
		}
		partsOffset = builder.CreateVectorOfTables(partOffsets)
	}

	flat.GeometryStart(builder)
	flat.GeometryAddType(builder, geometry.geometryType)
	if xyOffset > 0 {
		flat.GeometryAddXy(builder, xyOffset)
	}
	if zOffset > 0 {
		flat.GeometryAddZ(builder, zOffset)
	}
	if mOffset > 0 {
		flat.GeometryAddM(builder, mOffset)
	}
	if endsOffset > 0 {
		flat.GeometryAddEnds(builder, endsOffset)
	}
	if partsOffset > 0 {
		flat.GeometryAddParts(builder, partsOffset)
	}
	return flat.GeometryEnd(builder), nil
}

func flatGeobufGeometryFromGeom(geometry geom.T) (flatGeobufGeometry, error) {
	switch g := geometry.(type) {
	case *geom.Point:
		return flatGeobufSimpleGeometry(flat.GeometryTypePoint, g.Layout(), g.FlatCoords(), nil)
	case *geom.LineString:
		return flatGeobufSimpleGeometry(flat.GeometryTypeLineString, g.Layout(), g.FlatCoords(), nil)
	case *geom.LinearRing:
		return flatGeobufSimpleGeometry(flat.GeometryTypeLineString, g.Layout(), g.FlatCoords(), nil)
	case *geom.Polygon:
		return flatGeobufSimpleGeometry(flat.GeometryTypePolygon, g.Layout(), g.FlatCoords(), g.Ends())
	case *geom.MultiPoint:
		return flatGeobufSimpleGeometry(flat.GeometryTypeMultiPoint, g.Layout(), g.FlatCoords(), nil)
	case *geom.MultiLineString:
		return flatGeobufSimpleGeometry(flat.GeometryTypeMultiLineString, g.Layout(), g.FlatCoords(), g.Ends())
	case *geom.MultiPolygon:
		result := flatGeobufGeometry{geometryType: flat.GeometryTypeMultiPolygon}
		for i := 0; i < g.NumPolygons(); i++ {
			part, err := flatGeobufGeometryFromGeom(g.Polygon(i))
			if err != nil {
				return flatGeobufGeometry{}, err
			}
			result.parts = append(result.parts, part)
		}
		return result, nil
	case *geom.GeometryCollection:
		result := flatGeobufGeometry{geometryType: flat.GeometryTypeGeometryCollection}
		for i := 0; i < g.NumGeoms(); i++ {
			part, err := flatGeobufGeometryFromGeom(g.Geom(i))
			if err != nil {
				return flatGeobufGeometry{}, err
			}
			result.parts = append(result.parts, part)
		}
		return result, nil
	default:
		return flatGeobufGeometry{}, fmt.Errorf("unsupported FlatGeobuf geometry type %T", geometry)
	}
}

func alignFlatGeobufGeometryToHeader(geometry flatGeobufGeometry, headerGeometryType flat.GeometryType) flatGeobufGeometry {
	switch {
	case headerGeometryType == flat.GeometryTypeMultiPoint && geometry.geometryType == flat.GeometryTypePoint:
		geometry.geometryType = flat.GeometryTypeMultiPoint
	case headerGeometryType == flat.GeometryTypeMultiLineString && geometry.geometryType == flat.GeometryTypeLineString:
		geometry.geometryType = flat.GeometryTypeMultiLineString
	case headerGeometryType == flat.GeometryTypeMultiPolygon && geometry.geometryType == flat.GeometryTypePolygon:
		geometry = flatGeobufGeometry{
			geometryType: flat.GeometryTypeMultiPolygon,
			parts:        []flatGeobufGeometry{geometry},
		}
	}
	return geometry
}

func flatGeobufSimpleGeometry(geometryType flat.GeometryType, layout geom.Layout, flatCoords []float64, ends []int) (flatGeobufGeometry, error) {
	xy, z, m, err := splitFlatCoords(layout, flatCoords)
	if err != nil {
		return flatGeobufGeometry{}, err
	}
	result := flatGeobufGeometry{geometryType: geometryType, xy: xy, z: z, m: m}
	for _, end := range ends {
		if end <= 0 {
			continue
		}
		coordCount := end / layout.Stride()
		if coordCount <= 0 {
			continue
		}
		result.ends = append(result.ends, uint32(coordCount))
	}
	return result, nil
}

func splitFlatCoords(layout geom.Layout, flatCoords []float64) (xy, z, m []float64, err error) {
	stride := layout.Stride()
	if stride < 2 {
		return nil, nil, nil, fmt.Errorf("unsupported geometry layout %s", layout)
	}
	if len(flatCoords)%stride != 0 {
		return nil, nil, nil, fmt.Errorf("flat coordinates length %d is not divisible by layout stride %d", len(flatCoords), stride)
	}
	zIndex := layout.ZIndex()
	mIndex := layout.MIndex()
	for i := 0; i+stride <= len(flatCoords); i += stride {
		xy = append(xy, flatCoords[i], flatCoords[i+1])
		if zIndex >= 0 {
			z = append(z, flatCoords[i+zIndex])
		}
		if mIndex >= 0 {
			m = append(m, flatCoords[i+mIndex])
		}
	}
	return xy, z, m, nil
}

func encodeFlatGeobufProperties(properties map[string]interface{}, columns []FlatGeobufColumn) ([]byte, error) {
	if len(properties) == 0 || len(columns) == 0 {
		return nil, nil
	}
	var buffer bytes.Buffer
	writer := flatgeobuf.NewPropWriter(&buffer)
	for i, column := range columns {
		value, ok := properties[column.Name]
		if !ok || value == nil {
			continue
		}
		if i > math.MaxUint16 {
			return nil, fmt.Errorf("FlatGeobuf property column index overflows uint16: %d", i)
		}
		if _, err := writer.WriteUShort(uint16(i)); err != nil {
			return nil, err
		}
		if err := writeFlatGeobufPropertyValue(writer, column.Type, value); err != nil {
			return nil, fmt.Errorf("FlatGeobuf property %q: %w", column.Name, err)
		}
	}
	return buffer.Bytes(), nil
}

func writeFlatGeobufPropertyValue(writer *flatgeobuf.PropWriter, propertyType FlatGeobufPropertyType, value interface{}) error {
	switch propertyType {
	case FlatGeobufPropertyBool:
		typed, ok := value.(bool)
		if !ok {
			return fmt.Errorf("expected bool, got %T", value)
		}
		_, err := writer.WriteBool(typed)
		return err
	case FlatGeobufPropertyInt64:
		typed, err := int64Value(value)
		if err != nil {
			return err
		}
		_, err = writer.WriteLong(typed)
		return err
	case FlatGeobufPropertyFloat64:
		typed, err := float64Value(value)
		if err != nil {
			return err
		}
		_, err = writer.WriteDouble(typed)
		return err
	case FlatGeobufPropertyString:
		_, err := writer.WriteString(fmt.Sprint(value))
		return err
	case FlatGeobufPropertyJSON:
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = writer.WriteBinary(data)
		return err
	case FlatGeobufPropertyBinary:
		typed, ok := value.([]byte)
		if !ok {
			return fmt.Errorf("expected []byte, got %T", value)
		}
		_, err := writer.WriteBinary(typed)
		return err
	default:
		return fmt.Errorf("unsupported FlatGeobuf property type %q", propertyType)
	}
}

func flatGeobufColumnType(propertyType FlatGeobufPropertyType) (flat.ColumnType, error) {
	switch propertyType {
	case FlatGeobufPropertyBool:
		return flat.ColumnTypeBool, nil
	case FlatGeobufPropertyInt64:
		return flat.ColumnTypeLong, nil
	case FlatGeobufPropertyFloat64:
		return flat.ColumnTypeDouble, nil
	case FlatGeobufPropertyString:
		return flat.ColumnTypeString, nil
	case FlatGeobufPropertyJSON:
		return flat.ColumnTypeJson, nil
	case FlatGeobufPropertyBinary:
		return flat.ColumnTypeBinary, nil
	default:
		return flat.ColumnTypeString, fmt.Errorf("unsupported FlatGeobuf property type %q", propertyType)
	}
}

func flatGeobufHeaderGeometryType(geometryType string) flat.GeometryType {
	switch strings.ToLower(strings.TrimSpace(geometryType)) {
	case "point":
		return flat.GeometryTypePoint
	case "linestring":
		return flat.GeometryTypeLineString
	case "polygon":
		return flat.GeometryTypePolygon
	case "multipoint":
		return flat.GeometryTypeMultiPoint
	case "multilinestring":
		return flat.GeometryTypeMultiLineString
	case "multipolygon":
		return flat.GeometryTypeMultiPolygon
	case "geometrycollection":
		return flat.GeometryTypeGeometryCollection
	default:
		return flat.GeometryTypeUnknown
	}
}

func createFloat64Vector(builder *flatbuffers.Builder, values []float64) flatbuffers.UOffsetT {
	builder.StartVector(flatbuffers.SizeFloat64, len(values), flatbuffers.SizeFloat64)
	for i := len(values) - 1; i >= 0; i-- {
		builder.PrependFloat64(values[i])
	}
	return builder.EndVector(len(values))
}

func createUint32Vector(builder *flatbuffers.Builder, values []uint32) flatbuffers.UOffsetT {
	builder.StartVector(flatbuffers.SizeUint32, len(values), flatbuffers.SizeUint32)
	for i := len(values) - 1; i >= 0; i-- {
		builder.PrependUint32(values[i])
	}
	return builder.EndVector(len(values))
}

func int64Value(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int8:
		return int64(typed), nil
	case int16:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case uint:
		if uint64(typed) > math.MaxInt64 {
			return 0, fmt.Errorf("uint value overflows int64")
		}
		return int64(typed), nil
	case uint8:
		return int64(typed), nil
	case uint16:
		return int64(typed), nil
	case uint32:
		return int64(typed), nil
	case uint64:
		if typed > math.MaxInt64 {
			return 0, fmt.Errorf("uint64 value overflows int64")
		}
		return int64(typed), nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed {
			return 0, fmt.Errorf("float64 value is not an integer")
		}
		if typed < float64(math.MinInt64) || typed >= -float64(math.MinInt64) {
			return 0, fmt.Errorf("float64 value overflows int64")
		}
		return int64(typed), nil
	case json.Number:
		value, err := typed.Int64()
		if err != nil {
			return 0, err
		}
		return value, nil
	case string:
		value, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, err
		}
		return value, nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func float64Value(value interface{}) (float64, error) {
	switch typed := value.(type) {
	case float32:
		return float64(typed), nil
	case float64:
		return typed, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		integer, err := int64Value(value)
		if err != nil {
			return 0, err
		}
		return float64(integer), nil
	case json.Number:
		value, err := typed.Float64()
		if err != nil {
			return 0, err
		}
		return value, nil
	case string:
		value, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, err
		}
		return value, nil
	default:
		return 0, fmt.Errorf("expected number, got %T", value)
	}
}
