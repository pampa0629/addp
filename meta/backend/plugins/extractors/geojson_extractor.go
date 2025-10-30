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

	parser := geojson.NewParser(nil)

	schema, err := parser.ParseSchema(ctx, bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("geojson extractor: parse schema failed: %w", err)
	}

	records, err := parser.ReadRecords(ctx, bytes.NewReader(content), 0, geoJSONSampleLimit)
	if err != nil {
		return nil, fmt.Errorf("geojson extractor: read sample records failed: %w", err)
	}

	metaMap, err := parser.ExtractMetadata(ctx, bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("geojson extractor: extract metadata failed: %w", err)
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

	geometryField := determineGeometryField(schema)
	metadata.SchemaInfo = buildSchemaMetadata(schema, records, collection, geometryField)
	metadata.CustomAttrs["geo_metadata"] = buildGeoMetadata(metaMap, collection, geometryField)

	return metadata, nil
}

func determineGeometryField(schema *format.Schema) string {
	if schema == nil {
		return "geometry"
	}
	if schema.GeometryField != nil && *schema.GeometryField != "" {
		return *schema.GeometryField
	}
	return "geometry"
}

func buildSchemaMetadata(schema *format.Schema, records []map[string]interface{}, collection *geojson.FeatureCollection, geometryField string) *format.SchemaMetadata {
	if schema == nil {
		return nil
	}

	columns := make([]format.ColumnMetadata, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		columns = append(columns, format.ColumnMetadata{
			Name:     field.Name,
			Type:     string(field.Type),
			Nullable: field.Nullable,
		})
	}

	rowCount := int64(-1)
	if schema.RecordCount != nil {
		rowCount = *schema.RecordCount
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

func buildGeoMetadata(meta map[string]interface{}, collection *geojson.FeatureCollection, geometryField string) map[string]interface{} {
	result := map[string]interface{}{
		"geometry_field": geometryField,
	}

	if fc, ok := meta["feature_count"]; ok {
		result["feature_count"] = fc
	}

	if bbox := extractBoundingBox(meta, collection); bbox != nil {
		result["bounding_box"] = bbox
		result["dimensions"] = inferDimensions(len(bbox))
	}

	geometryTypes := extractGeometryTypes(meta, collection)
	if len(geometryTypes) > 0 {
		result["geometry_types"] = geometryTypes
		result["geometry_type"] = geometryTypes[0]
	}

	coordinateSystem := extractCoordinateSystem(meta, collection)
	if coordinateSystem != "" {
		result["coordinate_system"] = coordinateSystem
	} else {
		result["coordinate_system"] = defaultCoordinateSys
	}

	if props, ok := meta["properties"]; ok {
		result["properties"] = props
	}

	return result
}

func extractGeometryTypes(meta map[string]interface{}, collection *geojson.FeatureCollection) []string {
	if meta != nil {
		if types, ok := meta["geometry_types"].([]string); ok {
			return types
		}
		if raw, ok := meta["geometry_types"].([]interface{}); ok {
			out := make([]string, 0, len(raw))
			for _, item := range raw {
				if s, ok := item.(string); ok {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}

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

func extractBoundingBox(meta map[string]interface{}, collection *geojson.FeatureCollection) []float64 {
	if meta != nil {
		if bbox, ok := meta["bounding_box"].([]float64); ok {
			return bbox
		}
		if raw, ok := meta["bounding_box"].([]interface{}); ok {
			out := make([]float64, 0, len(raw))
			for _, item := range raw {
				switch v := item.(type) {
				case float64:
					out = append(out, v)
				case float32:
					out = append(out, float64(v))
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if collection != nil && len(collection.Metadata.BoundingBox) > 0 {
		return collection.Metadata.BoundingBox
	}
	return nil
}

func extractCoordinateSystem(meta map[string]interface{}, collection *geojson.FeatureCollection) string {
	if meta != nil {
		if cs, ok := meta["coordinate_system"].(string); ok && cs != "" {
			return cs
		}
	}
	if collection != nil && collection.Metadata.CoordinateSystem != "" {
		return collection.Metadata.CoordinateSystem
	}
	return ""
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
