package spatial

import (
	"context"
	"io"
	"strings"

	"github.com/addp/common/datatype"
)

type FlatGeobufBatchReadFunc func(ctx context.Context, limit int) ([]map[string]interface{}, error)

type FlatGeobufBatchFeatureReader struct {
	readBatch      FlatGeobufBatchReadFunc
	bufferedRows   []map[string]interface{}
	geometryColumn string
	columns        []FlatGeobufColumn
	srid           int
	rowLimit       int
	readRows       int
}

func NewFlatGeobufBatchFeatureReader(
	readBatch FlatGeobufBatchReadFunc,
	bufferedRows []map[string]interface{},
	geometryColumn string,
	fields []datatype.FieldInfo,
	spatialInfo *datatype.SpatialInfo,
	rowLimit int,
) (*FlatGeobufBatchFeatureReader, FlatGeobufOptions) {
	columns := FlatGeobufColumnsFromFields(fields, geometryColumn)
	if rowLimit > 0 && len(bufferedRows) > rowLimit {
		bufferedRows = bufferedRows[:rowLimit]
	}
	reader := &FlatGeobufBatchFeatureReader{
		readBatch:      readBatch,
		bufferedRows:   bufferedRows,
		geometryColumn: geometryColumn,
		columns:        columns,
		srid:           FlatGeobufSourceSRID(spatialInfo, geometryColumn),
		rowLimit:       rowLimit,
		readRows:       len(bufferedRows),
	}
	return reader, FlatGeobufOptionsFromSpatialInfo("quick_view", columns, spatialInfo, geometryColumn)
}

func (r *FlatGeobufBatchFeatureReader) NextFlatGeobufFeature(ctx context.Context) (*FlatGeobufFeature, error) {
	for {
		if len(r.bufferedRows) == 0 {
			if r.rowLimit > 0 && r.readRows >= r.rowLimit {
				return nil, io.EOF
			}
			limit := 512
			if r.rowLimit > 0 && r.readRows+limit > r.rowLimit {
				limit = r.rowLimit - r.readRows
			}
			rows, err := r.readBatch(ctx, limit)
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				return nil, io.EOF
			}
			if len(rows) > limit {
				rows = rows[:limit]
			}
			r.readRows += len(rows)
			r.bufferedRows = rows
		}
		row := r.bufferedRows[0]
		r.bufferedRows = r.bufferedRows[1:]
		if feature := FlatGeobufFeatureFromRow(row, r.geometryColumn, r.columns, r.srid); feature != nil {
			return feature, nil
		}
	}
}

func FlatGeobufFeatureFromRow(row map[string]interface{}, geometryColumn string, columns []FlatGeobufColumn, srid int) *FlatGeobufFeature {
	if len(row) == 0 {
		return nil
	}
	geometry, ok := row[geometryColumn]
	if !ok || geometry == nil {
		for key, value := range row {
			if strings.EqualFold(strings.TrimSpace(key), geometryColumn) {
				geometry = value
				ok = true
				break
			}
		}
	}
	if !ok || geometry == nil {
		return nil
	}
	properties := make(map[string]interface{}, len(columns))
	for _, column := range columns {
		if value, ok := row[column.Name]; ok {
			properties[column.Name] = value
		}
	}
	return &FlatGeobufFeature{
		Geometry:         geometry,
		GeometryEncoding: string(GeometryEncodingEWKB),
		GeometrySRID:     srid,
		Properties:       properties,
	}
}

func FlatGeobufOptionsFromSpatialInfo(name string, columns []FlatGeobufColumn, spatialInfo *datatype.SpatialInfo, geometryColumn string) FlatGeobufOptions {
	srid := FlatGeobufSourceSRID(spatialInfo, geometryColumn)
	crsRef := strings.TrimSpace(FlatGeobufSourceCRS(spatialInfo, geometryColumn))
	opts := FlatGeobufOptions{
		Name:            name,
		SRID:            srid,
		CRSName:         crsRef,
		Columns:         columns,
		GeometryType:    FlatGeobufGeometryType(spatialInfo, geometryColumn),
		DefaultEncoding: string(GeometryEncodingEWKB),
	}
	if definition := FlatGeobufCRSDefinition(spatialInfo, crsRef); definition != nil {
		opts.CRSWKT = definition.Definition
	}
	return opts
}

func ResolveFlatGeobufGeometryColumn(requested string, spatialInfo *datatype.SpatialInfo, fields []datatype.FieldInfo) string {
	if value := strings.TrimSpace(requested); value != "" {
		return value
	}
	if spatialInfo != nil {
		if value := strings.TrimSpace(spatialInfo.PrimaryGeometryName()); value != "" {
			return value
		}
		for _, value := range spatialInfo.GeometryColumnNames() {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	for _, field := range fields {
		if datatype.IsSpatialFieldType(field.Type) && strings.TrimSpace(field.Name) != "" {
			return strings.TrimSpace(field.Name)
		}
	}
	return ""
}

func FlatGeobufColumnsFromFields(fields []datatype.FieldInfo, geometryColumn string) []FlatGeobufColumn {
	columns := make([]FlatGeobufColumn, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || strings.EqualFold(name, geometryColumn) || datatype.IsSpatialFieldType(field.Type) {
			continue
		}
		columns = append(columns, FlatGeobufColumn{Name: name, Type: FlatGeobufPropertyTypeFromField(field)})
	}
	return columns
}

func FlatGeobufPropertyTypeFromField(field datatype.FieldInfo) FlatGeobufPropertyType {
	switch field.Type {
	case datatype.FieldTypeBool:
		return FlatGeobufPropertyBool
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		return FlatGeobufPropertyInt64
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal:
		return FlatGeobufPropertyFloat64
	case datatype.FieldTypeBytes:
		return FlatGeobufPropertyBinary
	case datatype.FieldTypeJSON, datatype.FieldTypeArray, datatype.FieldTypeMixed:
		return FlatGeobufPropertyJSON
	default:
		return FlatGeobufPropertyString
	}
}

func FlatGeobufSourceSRID(spatialInfo *datatype.SpatialInfo, geometryColumn string) int {
	if spatialInfo == nil {
		return 0
	}
	for _, column := range spatialInfo.GeometryColumns {
		if strings.EqualFold(strings.TrimSpace(column.Name), geometryColumn) && column.SRID != nil {
			return *column.SRID
		}
	}
	return spatialInfo.PrimarySRIDValue()
}

func FlatGeobufSourceCRS(spatialInfo *datatype.SpatialInfo, geometryColumn string) string {
	if spatialInfo == nil {
		return ""
	}
	for _, column := range spatialInfo.GeometryColumns {
		if strings.EqualFold(strings.TrimSpace(column.Name), geometryColumn) {
			if value := strings.TrimSpace(column.CRSRef); value != "" {
				return value
			}
			if column.SRID != nil && *column.SRID > 0 {
				return datatype.EPSGCRSRef(*column.SRID)
			}
		}
	}
	return spatialInfo.PrimaryCRSRef()
}

func FlatGeobufGeometryType(spatialInfo *datatype.SpatialInfo, geometryColumn string) string {
	if spatialInfo == nil {
		return ""
	}
	for _, column := range spatialInfo.GeometryColumns {
		if strings.EqualFold(strings.TrimSpace(column.Name), geometryColumn) {
			return column.GeometryType
		}
	}
	return spatialInfo.PrimaryGeometryType()
}

func FlatGeobufCRSDefinition(spatialInfo *datatype.SpatialInfo, crsRef string) *datatype.CRSDefinition {
	if spatialInfo == nil {
		return nil
	}
	if value := strings.TrimSpace(crsRef); value != "" {
		if definition := spatialInfo.CRSDefinitionByID(value); definition != nil {
			return definition
		}
	}
	if primary := spatialInfo.PrimaryCRSRef(); primary != "" {
		return spatialInfo.CRSDefinitionByID(primary)
	}
	return nil
}
