package executor

import (
	"sort"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

const defaultBatchSize = 1000

func tableInfoFromBatch(batch *engineplugin.BatchData) *format.TableInfo {
	info := &format.TableInfo{}
	if batch == nil {
		return info
	}
	info.Fields = make([]datatype.FieldInfo, 0, len(batch.Fields))
	for _, field := range batch.Fields {
		name := field.Name
		if name == "" {
			continue
		}
		field.Name = name
		info.Fields = append(info.Fields, field)
	}
	info.SpatialInfo = spatialInfoFromBatch(batch)
	if len(info.Fields) == 0 && len(batch.Rows) > 0 {
		names := make([]string, 0, len(batch.Rows[0]))
		for name := range batch.Rows[0] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			info.Fields = append(info.Fields, datatype.FieldInfo{Name: name, Type: datatype.FieldTypeUnknown})
		}
	}
	return info
}

func spatialInfoFromBatch(batch *engineplugin.BatchData) *datatype.SpatialInfo {
	if batch == nil {
		return nil
	}
	if batch.Spatial != nil {
		return batch.Spatial.Clone()
	}
	for _, field := range batch.Fields {
		if field.Name == "" || !datatype.IsSpatialFieldType(field.Type) {
			continue
		}
		return datatype.NewSingleGeometrySpatialInfo(field.Name, "", 0, 0)
	}
	return nil
}

func contentRefFromCatalogPath(path engineplugin.CatalogPath) contentio.Ref {
	stringPath := path.StringPath()
	return contentio.NewRef(stringPath, contentio.RoleMain)
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
	srid := info.SpatialInfo.PrimarySRIDValue()
	dimension := info.SpatialInfo.PrimaryDimensionValue()
	if geometryField != "" {
		for i := range info.Fields {
			if strings.EqualFold(info.Fields[i].Name, geometryField) {
				info.Fields[i].Type = datatype.FieldTypeGeometry
				break
			}
		}
	}
	if geometryField == "" {
		geometryField = info.SpatialInfo.PrimaryGeometryName()
	}
	if geometryType == "" {
		geometryType = info.SpatialInfo.PrimaryGeometryType()
	}
	info.SpatialInfo = datatype.NewSingleGeometrySpatialInfo(geometryField, geometryType, srid, dimension)
}

func optionString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
