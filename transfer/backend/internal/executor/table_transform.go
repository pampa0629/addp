package executor

import (
	"context"
	"fmt"
	"github.com/addp/common/datatype"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

type tableTransform interface {
	TransformSchema(schema *format.TableInfo) (*format.TableInfo, error)
	TransformBatch(ctx context.Context, batch *engineplugin.BatchData) (*engineplugin.BatchData, error)
}

func buildTableTransforms(plans []TableTransformPlan) ([]tableTransform, error) {
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
		default:
			return nil, fmt.Errorf("unsupported table transform type %q", plan.Type)
		}
	}
	return transforms, nil
}

func applySchemaTransforms(schema *format.TableInfo, transforms []tableTransform) (*format.TableInfo, error) {
	next := cloneTableInfo(schema)
	for _, transform := range transforms {
		var err error
		next, err = transform.TransformSchema(next)
		if err != nil {
			return nil, err
		}
	}
	if next == nil {
		next = &format.TableInfo{}
	}
	return next, nil
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

func (t *fieldMappingTransform) TransformSchema(schema *format.TableInfo) (*format.TableInfo, error) {
	if t == nil {
		return cloneTableInfo(schema), nil
	}
	source := cloneTableInfo(schema)
	if source == nil {
		source = &format.TableInfo{}
	}

	var next *format.TableInfo
	if t.mode == FieldMappingModePassthrough {
		next = cloneTableInfo(source)
	} else {
		next = &format.TableInfo{
			Name:       source.Name,
			PrimaryKey: append([]string(nil), source.PrimaryKey...),
			FormatInfo: cloneMap(source.FormatInfo),
		}
		if source.RowCount != nil {
			rowCount := *source.RowCount
			next.RowCount = &rowCount
		}
	}
	if next == nil {
		next = &format.TableInfo{}
	}

	for _, mapping := range t.fields {
		field := fieldInfoForMapping(source, mapping)
		upsertFieldInfo(next, field)
		if datatype.IsSpatialFieldType(field.Type) {
			geometryType := format.PrimaryGeometryType(source.SpatialInfo)
			srid := format.PrimaryGeometrySRID(source.SpatialInfo)
			dimension := format.PrimaryGeometryDimension(source.SpatialInfo)
			next.SpatialInfo = format.NewSingleGeometrySpatialInfo(mapping.Target, geometryType, srid, dimension)
			if source.SpatialInfo != nil {
				if source.SpatialInfo.Extent != nil {
					extent := *source.SpatialInfo.Extent
					next.SpatialInfo.Extent = &extent
				}
				if source.SpatialInfo.HasSpatialIndex != nil {
					hasSpatialIndex := *source.SpatialInfo.HasSpatialIndex
					next.SpatialInfo.HasSpatialIndex = &hasSpatialIndex
				}
				next.SpatialInfo.IndexName = source.SpatialInfo.IndexName
			}
		}
	}
	return next, nil
}

func (t *fieldMappingTransform) TransformBatch(ctx context.Context, batch *engineplugin.BatchData) (*engineplugin.BatchData, error) {
	if t == nil || batch == nil {
		return batch, nil
	}
	next := &engineplugin.BatchData{
		Rows:   make([]map[string]interface{}, 0, len(batch.Rows)),
		Fields: fieldMappingBatchFields(batch.Fields, t.fields, t.mode),
		Offset: batch.Offset,
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

func fieldInfoForMapping(schema *format.TableInfo, mapping FieldMappingFieldPlan) format.FieldInfo {
	field := findFieldInfo(schema, mapping.Source)
	if field.Name == "" {
		field = findFieldInfo(schema, mapping.Target)
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

func fieldMappingBatchFields(source []engineplugin.FieldInfo, mappings []FieldMappingFieldPlan, mode FieldMappingMode) []engineplugin.FieldInfo {
	if mode == FieldMappingModePassthrough {
		fields := append([]engineplugin.FieldInfo(nil), source...)
		for _, mapping := range mappings {
			field := engineFieldForMapping(source, mapping)
			fields = upsertEngineField(fields, field)
		}
		return fields
	}
	fields := make([]engineplugin.FieldInfo, 0, len(mappings))
	for _, mapping := range mappings {
		fields = append(fields, engineFieldForMapping(source, mapping))
	}
	return fields
}

func engineFieldForMapping(source []engineplugin.FieldInfo, mapping FieldMappingFieldPlan) engineplugin.FieldInfo {
	field := findEngineField(source, mapping.Source)
	if field.Name == "" {
		field = findEngineField(source, mapping.Target)
	}
	field.Name = mapping.Target
	if mapping.TargetType != "" {
		field.Type = mapping.TargetType
	}
	field.Nullable = mapping.Nullable
	return field
}

func findFieldInfo(schema *format.TableInfo, name string) format.FieldInfo {
	if schema == nil || name == "" {
		return format.FieldInfo{}
	}
	for _, field := range schema.Fields {
		if strings.EqualFold(field.Name, name) {
			return field
		}
	}
	return format.FieldInfo{}
}

func findEngineField(fields []engineplugin.FieldInfo, name string) engineplugin.FieldInfo {
	if name == "" {
		return engineplugin.FieldInfo{}
	}
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) {
			return field
		}
	}
	return engineplugin.FieldInfo{}
}

func upsertFieldInfo(info *format.TableInfo, field format.FieldInfo) {
	for i := range info.Fields {
		if strings.EqualFold(info.Fields[i].Name, field.Name) {
			info.Fields[i] = field
			return
		}
	}
	info.Fields = append(info.Fields, field)
}

func upsertEngineField(fields []engineplugin.FieldInfo, field engineplugin.FieldInfo) []engineplugin.FieldInfo {
	for i := range fields {
		if strings.EqualFold(fields[i].Name, field.Name) {
			fields[i] = field
			return fields
		}
	}
	return append(fields, field)
}

func cloneTableInfo(info *format.TableInfo) *format.TableInfo {
	if info == nil {
		return nil
	}
	next := *info
	next.Fields = append([]format.FieldInfo(nil), info.Fields...)
	next.PrimaryKey = append([]string(nil), info.PrimaryKey...)
	next.FormatInfo = cloneMap(info.FormatInfo)
	if info.RowCount != nil {
		rowCount := *info.RowCount
		next.RowCount = &rowCount
	}
	next.SpatialInfo = info.SpatialInfo.Clone()
	return &next
}

func cloneMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	next := make(map[string]interface{}, len(values))
	for key, value := range values {
		next[key] = value
	}
	return next
}
