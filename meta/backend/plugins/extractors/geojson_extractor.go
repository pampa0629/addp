package extractors

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/addp/common/format"
	"github.com/addp/common/format/geojson"
)

const (
	geoJSONSampleLimit   = 5
	defaultCoordinateSys = "EPSG:4326"
)

// GeoJSONExtractor GeoJSON文件的元数据提取器
type GeoJSONExtractor struct{}

func (e *GeoJSONExtractor) SupportedTypes() []string {
	return []string{
		"application/geo+json",
		"application/vnd.geo+json",
	}
}

func (e *GeoJSONExtractor) Priority() int {
	return 100
}

func (e *GeoJSONExtractor) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	content, err := io.ReadAll(input.Reader)
	if err != nil {
		return nil, fmt.Errorf("geojson extractor: read content failed: %w", err)
	}

	opts := format.DefaultParseOptions()
	parser := geojson.NewParser(opts)

	// 使用新接口 ParseTableInfo
	tableInfo, err := parser.ParseTableInfo(ctx, bytes.NewReader(content), opts)
	if err != nil {
		return nil, fmt.Errorf("geojson extractor: parse schema failed: %w", err)
	}

	// 使用新接口 ReadPreview
	records, err := parser.ReadPreview(ctx, bytes.NewReader(content), 0, geoJSONSampleLimit, opts)
	if err != nil {
		return nil, fmt.Errorf("geojson extractor: read sample records failed: %w", err)
	}

	collection, err := geojson.LoadFeatureCollection(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("geojson extractor: decode feature collection failed: %w", err)
	}

	metadata := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "GeoJSON",
			Size:         input.Size,
			ContentType:  input.ContentType,
			Encoding:     "UTF-8",
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	geometryField := determineGeometryFieldFromTableInfo(tableInfo)
	metadata.SchemaInfo = buildSchemaMetadataFromTableInfo(tableInfo, records, collection, geometryField)
	metadata.CustomAttrs["geo_metadata"] = buildGeoMetadataFromTableInfo(tableInfo, collection, geometryField)

	return metadata, nil
}

func determineGeometryFieldFromTableInfo(tableInfo *format.TableInfo) string {
	if tableInfo == nil {
		return "geometry"
	}
	// 从 Extensions 中提取 SpatialInfo
	for _, ext := range tableInfo.Extensions {
		if spatialInfo, ok := ext.(*format.SpatialInfo); ok {
			if spatialInfo.GeometryColumn != "" {
				return spatialInfo.GeometryColumn
			}
		}
	}
	return "geometry"
}

func buildSchemaMetadataFromTableInfo(tableInfo *format.TableInfo, records []map[string]interface{}, collection *geojson.FeatureCollection, geometryField string) *format.SchemaMetadata {
	if tableInfo == nil {
		return nil
	}

	columns := make([]format.ColumnMetadata, 0, len(tableInfo.Fields))
	for _, field := range tableInfo.Fields {
		columns = append(columns, format.ColumnMetadata{
			Name:     field.Name,
			Type:     string(field.Type),
			Nullable: field.Nullable,
		})
	}

	rowCount := int64(-1)
	if tableInfo.RowCount != nil {
		rowCount = *tableInfo.RowCount
	} else if collection != nil {
		rowCount = int64(len(collection.Features))
	}

	sample := make([]map[string]interface{}, 0, len(records))
	for _, record := range records {
		row := make(map[string]interface{})
		for key, value := range record {
			if key == geometryField {
				continue
			}
			row[key] = value
		}
		if len(row) > 0 {
			sample = append(sample, row)
		}
	}

	extra := map[string]interface{}{
		"geometry_column": geometryField,
	}
	if collection != nil && len(collection.Metadata.BoundingBox) > 0 {
		extra["bounding_box"] = collection.Metadata.BoundingBox
	}
	if collection != nil && collection.Metadata.CoordinateSystem != "" {
		extra["coordinate_system"] = collection.Metadata.CoordinateSystem
	}

	return &format.SchemaMetadata{
		Columns:    columns,
		RowCount:   rowCount,
		SampleData: sample,
		Extra:      extra,
	}
}

func buildGeoMetadataFromTableInfo(tableInfo *format.TableInfo, collection *geojson.FeatureCollection, geometryField string) map[string]interface{} {
	result := map[string]interface{}{
		"geometry_field": geometryField,
	}

	if tableInfo != nil && tableInfo.RowCount != nil {
		result["feature_count"] = *tableInfo.RowCount
	}

	// 从 Extensions 中提取 SpatialInfo
	var spatialInfo *format.SpatialInfo
	if tableInfo != nil {
		for _, ext := range tableInfo.Extensions {
			if si, ok := ext.(*format.SpatialInfo); ok {
				spatialInfo = si
				break
			}
		}
	}

	if spatialInfo != nil {
		if spatialInfo.GeometryType != "" {
			result["geometry_types"] = []string{spatialInfo.GeometryType}
			result["geometry_type"] = spatialInfo.GeometryType
		}
		if spatialInfo.SRID != 0 {
			result["coordinate_system"] = fmt.Sprintf("EPSG:%d", spatialInfo.SRID)
		} else {
			result["coordinate_system"] = defaultCoordinateSys
		}
	}

	// 从 collection 提取 bounding box
	if collection != nil && len(collection.Metadata.BoundingBox) > 0 {
		result["bounding_box"] = collection.Metadata.BoundingBox
		result["dimensions"] = inferDimensions(len(collection.Metadata.BoundingBox))
	}

	// 从 collection 提取几何类型
	if collection != nil {
		geometryTypes := extractGeometryTypesFromCollection(collection)
		if len(geometryTypes) > 0 && spatialInfo == nil {
			result["geometry_types"] = geometryTypes
			result["geometry_type"] = geometryTypes[0]
		}
	}

	return result
}

func extractGeometryTypesFromCollection(collection *geojson.FeatureCollection) []string {
	if collection == nil {
		return nil
	}

	typesSet := make(map[string]struct{})
	for _, feature := range collection.Features {
		if gt := feature.GeometryType(); gt != "" {
			typesSet[gt] = struct{}{}
		}
	}

	types := make([]string, 0, len(typesSet))
	for t := range typesSet {
		types = append(types, t)
	}
	return types
}

func inferDimensions(bboxLen int) string {
	switch bboxLen {
	case 4:
		return "2D"
	case 6:
		return "3D"
	default:
		return ""
	}
}
