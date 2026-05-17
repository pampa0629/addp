package executor

import (
	"sort"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

const defaultBatchSize = 1000

func tableInfoFromBatch(batch *engineplugin.BatchData) *format.TableInfo {
	info := &format.TableInfo{}
	if batch == nil {
		return info
	}
	info.Fields = make([]format.FieldInfo, 0, len(batch.Fields))
	for _, field := range batch.Fields {
		name := field.Name
		if name == "" {
			continue
		}
		info.Fields = append(info.Fields, format.FieldInfo{
			Name:         name,
			Type:         format.FieldType(field.Type),
			OriginalType: field.NativeType,
			Nullable:     field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		})
	}
	if len(info.Fields) == 0 && len(batch.Rows) > 0 {
		names := make([]string, 0, len(batch.Rows[0]))
		for name := range batch.Rows[0] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			info.Fields = append(info.Fields, format.FieldInfo{Name: name, Type: format.FieldTypeUnknown})
		}
	}
	return info
}

func resourceRefFromCatalogPath(path engineplugin.CatalogPath) resource.ResourceRef {
	stringPath := path.StringPath()
	return resource.NewResourceRef(stringPath, resource.ResourceRoleMain)
}

func applySpatialInfoFromOptions(info *format.TableInfo, opts *format.WriteOptions) {
	if info == nil || opts == nil || opts.ExtraParams == nil {
		return
	}
	geometryField := optionString(opts.ExtraParams, "geometry_field")
	geometryType := optionString(opts.ExtraParams, "geometry_type")
	if geometryField == "" && geometryType == "" {
		return
	}
	if info.SpatialInfo == nil {
		info.SpatialInfo = &format.SpatialInfo{}
	}
	if geometryField != "" {
		info.SpatialInfo.GeometryColumn = geometryField
		for i := range info.Fields {
			if strings.EqualFold(info.Fields[i].Name, geometryField) {
				info.Fields[i].Type = format.FieldTypeGeometry
				break
			}
		}
	}
	if geometryType != "" {
		info.SpatialInfo.GeometryType = geometryType
	}
}

func optionString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
