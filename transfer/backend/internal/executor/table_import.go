package executor

import (
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

func isCopyWriteMethod(method string) bool {
	switch method {
	case "copy", "postgres_copy":
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
	if info == nil || info.SpatialInfo == nil || !format.IsGeometryType(field.Type) {
		return nil
	}
	if info.SpatialInfo.GeometryColumn != "" && info.SpatialInfo.GeometryColumn != field.Name {
		return nil
	}
	attrs := map[string]interface{}{}
	if info.SpatialInfo.GeometryType != "" {
		attrs["geometry_type"] = info.SpatialInfo.GeometryType
	}
	if info.SpatialInfo.SRID > 0 {
		attrs["srid"] = info.SpatialInfo.SRID
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}
