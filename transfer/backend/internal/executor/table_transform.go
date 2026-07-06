package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	commonSpatial "github.com/addp/common/spatial"
)

type tableTransform interface {
	TransformTableInfo(tableInfo *datatype.TableInfo, spatialInfo *datatype.SpatialInfo) (*datatype.TableInfo, *datatype.SpatialInfo, error)
	TransformBatch(ctx context.Context, batch *engineplugin.BatchData) (*engineplugin.BatchData, error)
}

func buildTableTransforms(plans []TableTransformPlan, reprojecter GeometryBatchReprojectProvider) ([]tableTransform, error) {
	if len(plans) == 0 {
		return nil, nil
	}
	transforms := make([]tableTransform, 0, len(plans))
	for _, plan := range plans {
		switch strings.ToLower(strings.TrimSpace(plan.Type)) {
		case "", "field_mapping":
			if plan.FieldMapping == nil {
				return nil, fmt.Errorf("field_mapping transform requires config")
			}
			transform, err := newFieldMappingTransform(*plan.FieldMapping)
			if err != nil {
				return nil, err
			}
			transforms = append(transforms, transform)
		case "spatial_reproject":
			if plan.SpatialReproject == nil {
				return nil, fmt.Errorf("spatial_reproject transform requires config")
			}
			transform, err := newSpatialReprojectTransform(*plan.SpatialReproject, reprojecter)
			if err != nil {
				return nil, err
			}
			transforms = append(transforms, transform)
		default:
			return nil, fmt.Errorf("unsupported table transform type %q", plan.Type)
		}
	}
	return transforms, nil
}

func applyTableInfoTransforms(tableInfo *datatype.TableInfo, spatialInfo *datatype.SpatialInfo, transforms []tableTransform) (*datatype.TableInfo, *datatype.SpatialInfo, error) {
	next := tableInfo.Clone()
	nextSpatial := spatialInfo.Clone()
	for _, transform := range transforms {
		var err error
		next, nextSpatial, err = transform.TransformTableInfo(next, nextSpatial)
		if err != nil {
			return nil, nil, err
		}
	}
	if next == nil {
		next = &datatype.TableInfo{}
	}
	return next, nextSpatial, nil
}

func applyBatchTransforms(ctx context.Context, batch *engineplugin.BatchData, transforms []tableTransform) (*engineplugin.BatchData, error) {
	next := batch
	for _, transform := range transforms {
		var err error
		next, err = transform.TransformBatch(ctx, next)
		if err != nil {
			return nil, err
		}
	}
	return next, nil
}

type spatialReprojectTransform struct {
	reprojecter    GeometryBatchReprojectProvider
	geometryColumn string
	sourceCRS      string
	targetCRS      string
	needReproject  bool
}

func newSpatialReprojectTransform(plan SpatialReprojectTransformPlan, reprojecter GeometryBatchReprojectProvider) (*spatialReprojectTransform, error) {
	if strings.TrimSpace(plan.GeometryColumn) == "" {
		return nil, fmt.Errorf("spatial_reproject geometry_column is required")
	}
	sourceCRS := strings.TrimSpace(plan.SourceCRS)
	targetCRS := strings.TrimSpace(plan.TargetCRS)
	if sourceCRS == "" {
		return nil, fmt.Errorf("spatial_reproject source_crs is required")
	}
	if targetCRS == "" {
		targetCRS = "EPSG:4326"
	}
	if plan.Reproject && reprojecter == nil {
		return nil, fmt.Errorf("spatial_reproject requires geometry batch reprojecter")
	}
	return &spatialReprojectTransform{
		reprojecter:    reprojecter,
		geometryColumn: plan.GeometryColumn,
		sourceCRS:      sourceCRS,
		targetCRS:      targetCRS,
		needReproject:  plan.Reproject,
	}, nil
}

func (t *spatialReprojectTransform) TransformTableInfo(tableInfo *datatype.TableInfo, spatialInfo *datatype.SpatialInfo) (*datatype.TableInfo, *datatype.SpatialInfo, error) {
	nextTable := tableInfo.Clone()
	if nextTable == nil {
		nextTable = &datatype.TableInfo{}
	}
	nextSpatial := cloneSpatialInfoForReproject(spatialInfo, t.geometryColumn, t.targetCRS)
	return nextTable, nextSpatial, nil
}

func (t *spatialReprojectTransform) TransformBatch(ctx context.Context, batch *engineplugin.BatchData) (*engineplugin.BatchData, error) {
	if batch == nil {
		return nil, nil
	}
	geometryColumn := t.geometryColumn
	if batch.Spatial != nil && batch.Spatial.PrimaryGeometryName() != "" {
		geometryColumn = batch.Spatial.PrimaryGeometryName()
	}
	if geometryColumn == "" {
		return nil, fmt.Errorf("spatial_reproject requires geometry column")
	}

	geometries := make([][]byte, 0, len(batch.Rows))
	for _, row := range batch.Rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, ok := row[geometryColumn]
		if !ok || value == nil {
			geometries = append(geometries, nil)
			continue
		}
		geometryBytes, err := geometryValueToEWKB(value)
		if err != nil {
			return nil, fmt.Errorf("convert geometry column %q: %w", geometryColumn, err)
		}
		geometries = append(geometries, geometryBytes)
	}

	var reprojected [][]byte
	var err error
	if t.needReproject {
		if t.reprojecter == nil {
			return nil, fmt.Errorf("spatial_reproject requires geometry batch reprojecter")
		}
		reprojected, err = t.reprojecter.ReprojectGeometryBatch(ctx, geometries, t.sourceCRS, t.targetCRS, geometryColumn)
		if err != nil {
			return nil, err
		}
	} else {
		reprojected = geometries
	}
	if len(reprojected) != len(batch.Rows) {
		return nil, fmt.Errorf("spatial_reproject returned %d geometries for %d rows", len(reprojected), len(batch.Rows))
	}

	next := &engineplugin.BatchData{
		Rows:    make([]map[string]interface{}, 0, len(batch.Rows)),
		Fields:  append([]datatype.FieldInfo(nil), batch.Fields...),
		Spatial: cloneSpatialInfoForReproject(batch.Spatial, geometryColumn, t.targetCRS),
		Offset:  batch.Offset,
		Hints:   cloneBatchHints(batch.Hints),
	}
	for i, row := range batch.Rows {
		nextRow := make(map[string]interface{}, len(row))
		for key, value := range row {
			nextRow[key] = value
		}
		if reprojected[i] == nil {
			nextRow[geometryColumn] = nil
		} else {
			nextRow[geometryColumn] = reprojected[i]
		}
		next.Rows = append(next.Rows, nextRow)
	}
	return next, nil
}

func geometryValueToEWKB(value interface{}) ([]byte, error) {
	geometry, err := commonSpatial.ParseGeometryValue(value)
	if err != nil {
		return nil, err
	}
	return commonSpatial.GeomToEWKB(geometry, geometry.SRID())
}

func cloneSpatialInfoForReproject(source *datatype.SpatialInfo, geometryColumn, targetCRS string) *datatype.SpatialInfo {
	if source == nil {
		srid := commonSpatial.ParseSRID(targetCRS)
		info := datatype.NewSingleGeometrySpatialInfo(geometryColumn, "", srid, 0)
		info.CRSRef = targetCRS
		if len(info.GeometryColumns) > 0 {
			info.GeometryColumns[0].CRSRef = targetCRS
		}
		if srid > 0 {
			info.SRID = &srid
		}
		return info
	}
	next := source.Clone()
	if next == nil {
		next = datatype.NewSingleGeometrySpatialInfo(geometryColumn, "", 0, 0)
	}
	srid := commonSpatial.ParseSRID(targetCRS)
	next.PrimaryGeometryColumn = geometryColumn
	next.CRSDefinitions = nil
	next.Extent = nil
	next.HasSpatialIndex = nil
	next.IndexName = ""
	next.CRSRef = targetCRS
	if len(next.GeometryColumns) > 0 {
		next.GeometryColumns[0].Name = geometryColumn
		next.GeometryColumns[0].CRSRef = targetCRS
		if srid > 0 {
			next.GeometryColumns[0].SRID = &srid
		}
	}
	if srid > 0 {
		next.SRID = &srid
	}
	return next
}

func cloneBatchHints(hints map[string]interface{}) map[string]interface{} {
	if len(hints) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(hints))
	for key, value := range hints {
		cloned[key] = value
	}
	return cloned
}

type fieldMappingTransform struct {
	mode   FieldMappingMode
	fields []FieldMappingFieldPlan
}

func newFieldMappingTransform(plan FieldMappingTransformPlan) (*fieldMappingTransform, error) {
	mode := plan.Mode
	if mode == "" {
		mode = FieldMappingModeProject
	}
	if mode != FieldMappingModeProject && mode != FieldMappingModePassthrough {
		return nil, fmt.Errorf("unsupported field_mapping mode %q", mode)
	}
	if len(plan.Fields) == 0 {
		return nil, fmt.Errorf("field_mapping transform requires fields")
	}
	fields := make([]FieldMappingFieldPlan, 0, len(plan.Fields))
	for _, field := range plan.Fields {
		field.Source = strings.TrimSpace(field.Source)
		field.Target = strings.TrimSpace(field.Target)
		field.TargetType = strings.TrimSpace(field.TargetType)
		field.Format = strings.TrimSpace(field.Format)
		if field.Target == "" {
			return nil, fmt.Errorf("field_mapping field target is required")
		}
		fields = append(fields, field)
	}
	return &fieldMappingTransform{mode: mode, fields: fields}, nil
}

func (t *fieldMappingTransform) TransformTableInfo(tableInfo *datatype.TableInfo, spatialInfo *datatype.SpatialInfo) (*datatype.TableInfo, *datatype.SpatialInfo, error) {
	if t == nil {
		return tableInfo.Clone(), spatialInfo.Clone(), nil
	}
	source := tableInfo.Clone()
	if source == nil {
		source = &datatype.TableInfo{}
	}

	var next *datatype.TableInfo
	if t.mode == FieldMappingModePassthrough {
		next = source.Clone()
	} else {
		sourceCopy := source.Clone()
		next = &datatype.TableInfo{
			Name:       source.Name,
			PrimaryKey: sourceCopy.PrimaryKey,
			RowCount:   sourceCopy.RowCount,
			SizeBytes:  sourceCopy.SizeBytes,
			CreatedAt:  sourceCopy.CreatedAt,
			UpdatedAt:  sourceCopy.UpdatedAt,
		}
	}
	if next == nil {
		next = &datatype.TableInfo{}
	}
	var nextSpatial *datatype.SpatialInfo
	if t.mode == FieldMappingModePassthrough {
		nextSpatial = spatialInfo.Clone()
	}

	for _, mapping := range t.fields {
		field := fieldInfoForMapping(source, mapping)
		upsertFieldInfo(next, field)
		if datatype.IsSpatialFieldType(field.Type) {
			nextSpatial = cloneSpatialInfoForColumn(spatialInfo, mapping.Target)
			if nextSpatial == nil {
				nextSpatial = datatype.NewSingleGeometrySpatialInfo(mapping.Target, "", 0, 0)
			}
		}
	}
	return next, nextSpatial, nil
}

func (t *fieldMappingTransform) TransformBatch(ctx context.Context, batch *engineplugin.BatchData) (*engineplugin.BatchData, error) {
	if t == nil || batch == nil {
		return batch, nil
	}
	fields := fieldMappingBatchFields(batch.Fields, t.fields, t.mode)
	next := &engineplugin.BatchData{
		Rows:    make([]map[string]interface{}, 0, len(batch.Rows)),
		Fields:  fields,
		Spatial: fieldMappingBatchSpatial(batch.Spatial, fields, t.fields, t.mode),
		Offset:  batch.Offset,
	}
	for _, row := range batch.Rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nextRow := make(map[string]interface{}, len(t.fields))
		if t.mode == FieldMappingModePassthrough {
			for key, value := range row {
				nextRow[key] = value
			}
		}
		for _, mapping := range t.fields {
			value, ok := row[mapping.Source]
			if mapping.Source == "" || !ok || value == nil {
				value = mapping.Default
			}
			nextRow[mapping.Target] = value
		}
		next.Rows = append(next.Rows, nextRow)
	}
	return next, nil
}

func fieldInfoForMapping(tableInfo *datatype.TableInfo, mapping FieldMappingFieldPlan) datatype.FieldInfo {
	field := findFieldInfo(tableInfo, mapping.Source)
	if field.Name == "" {
		field = findFieldInfo(tableInfo, mapping.Target)
	}
	field.Name = mapping.Target
	if mapping.TargetType != "" {
		field.Type = datatype.FieldType(mapping.TargetType)
	}
	field.Nullable = mapping.Nullable
	if mapping.Format != "" {
		field.Comment = mapping.Format
	}
	if field.Type == "" {
		field.Type = datatype.FieldTypeUnknown
	}
	return field
}

func fieldMappingBatchFields(source []datatype.FieldInfo, mappings []FieldMappingFieldPlan, mode FieldMappingMode) []datatype.FieldInfo {
	if mode == FieldMappingModePassthrough {
		fields := append([]datatype.FieldInfo(nil), source...)
		for _, mapping := range mappings {
			field := engineFieldForMapping(source, mapping)
			fields = upsertEngineField(fields, field)
		}
		return fields
	}
	fields := make([]datatype.FieldInfo, 0, len(mappings))
	for _, mapping := range mappings {
		fields = append(fields, engineFieldForMapping(source, mapping))
	}
	return fields
}

func engineFieldForMapping(source []datatype.FieldInfo, mapping FieldMappingFieldPlan) datatype.FieldInfo {
	field := findEngineField(source, mapping.Source)
	if field.Name == "" {
		field = findEngineField(source, mapping.Target)
	}
	field.Name = mapping.Target
	if mapping.TargetType != "" {
		field.Type = datatype.FieldType(mapping.TargetType)
	}
	field.Nullable = mapping.Nullable
	return field
}

func fieldMappingBatchSpatial(source *datatype.SpatialInfo, fields []datatype.FieldInfo, mappings []FieldMappingFieldPlan, mode FieldMappingMode) *datatype.SpatialInfo {
	if source == nil {
		for _, field := range fields {
			if datatype.IsSpatialFieldType(field.Type) {
				return datatype.NewSingleGeometrySpatialInfo(field.Name, "", 0, 0)
			}
		}
		return nil
	}
	if mode == FieldMappingModePassthrough {
		return source.Clone()
	}
	primaryName := source.PrimaryGeometryName()
	for _, mapping := range mappings {
		field := findEngineField(fields, mapping.Target)
		if !datatype.IsSpatialFieldType(field.Type) {
			continue
		}
		if primaryName != "" && !strings.EqualFold(mapping.Source, primaryName) && !strings.EqualFold(mapping.Target, primaryName) {
			continue
		}
		return cloneSpatialInfoForColumn(source, mapping.Target)
	}
	return nil
}

func cloneSpatialInfoForColumn(source *datatype.SpatialInfo, columnName string) *datatype.SpatialInfo {
	if source == nil || columnName == "" {
		return nil
	}
	spatial := datatype.NewSingleGeometrySpatialInfo(
		columnName,
		source.PrimaryGeometryType(),
		source.PrimarySRIDValue(),
		source.PrimaryDimensionValue(),
	)
	spatial.CRSRef = source.CRSRef
	spatial.CRSDefinitions = append([]datatype.CRSDefinition(nil), source.CRSDefinitions...)
	if primary := source.PrimaryGeometry(); primary != nil && len(spatial.GeometryColumns) > 0 {
		spatial.GeometryColumns[0].CRSRef = strings.TrimSpace(primary.CRSRef)
	}
	if source.Extent != nil {
		extent := *source.Extent
		spatial.Extent = &extent
	}
	if source.HasSpatialIndex != nil {
		hasSpatialIndex := *source.HasSpatialIndex
		spatial.HasSpatialIndex = &hasSpatialIndex
	}
	spatial.IndexName = source.IndexName
	return spatial
}

func findFieldInfo(tableInfo *datatype.TableInfo, name string) datatype.FieldInfo {
	if tableInfo == nil || name == "" {
		return datatype.FieldInfo{}
	}
	for _, field := range tableInfo.Fields {
		if strings.EqualFold(field.Name, name) {
			return field
		}
	}
	return datatype.FieldInfo{}
}

func findEngineField(fields []datatype.FieldInfo, name string) datatype.FieldInfo {
	if name == "" {
		return datatype.FieldInfo{}
	}
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) {
			return field
		}
	}
	return datatype.FieldInfo{}
}

func upsertFieldInfo(info *datatype.TableInfo, field datatype.FieldInfo) {
	for i := range info.Fields {
		if strings.EqualFold(info.Fields[i].Name, field.Name) {
			info.Fields[i] = field
			return
		}
	}
	info.Fields = append(info.Fields, field)
}

func upsertEngineField(fields []datatype.FieldInfo, field datatype.FieldInfo) []datatype.FieldInfo {
	for i := range fields {
		if strings.EqualFold(fields[i].Name, field.Name) {
			fields[i] = field
			return fields
		}
	}
	return append(fields, field)
}
