package executor

import (
	"github.com/addp/common/datatype"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

func isCopyWriteMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "copy", "postgres_copy":
		return true
	default:
		return false
	}
}

func tableInfoFields(info *format.TableInfo) []engineplugin.FieldInfo {
	if info == nil {
		return nil
	}
	fields := make([]engineplugin.FieldInfo, 0, len(info.Fields))
	for _, field := range info.Fields {
		if field.Name == "" {
			continue
		}
		fields = append(fields, engineplugin.FieldInfo{
			Name:       field.Name,
			Type:       string(field.Type),
			Nullable:   field.Nullable,
			PrimaryKey: field.IsPrimaryKey,
			Comment:    field.Comment,
			Attributes: fieldAttributes(info, field),
		})
	}
	return fields
}

func fieldAttributes(info *format.TableInfo, field format.FieldInfo) map[string]interface{} {
	if info == nil || info.SpatialInfo == nil || !datatype.IsSpatialFieldType(field.Type) {
		return nil
	}
	geometryColumn := format.PrimaryGeometryColumn(info.SpatialInfo)
	if geometryColumn != "" && geometryColumn != field.Name {
		return nil
	}
	attrs := map[string]interface{}{}
	if geometryType := format.PrimaryGeometryType(info.SpatialInfo); geometryType != "" {
		attrs["geometry_type"] = geometryType
	}
	if srid := format.PrimaryGeometrySRID(info.SpatialInfo); srid > 0 {
		attrs["srid"] = srid
	}
	if dimension := format.PrimaryGeometryDimension(info.SpatialInfo); dimension > 0 {
		attrs["dimension"] = dimension
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}
