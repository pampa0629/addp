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

func tableInfoFromBatch(batch *engineplugin.BatchData) *datatype.TableInfo {
	info := &datatype.TableInfo{}
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

func writeOptionsWithSpatialInfo(opts *format.WriteOptions, info *datatype.TableInfo, spatialInfo *datatype.SpatialInfo) *format.WriteOptions {
	next := format.DefaultWriteOptions()
	if opts != nil {
		*next = *opts
	}
	next.SpatialInfo = spatialInfoForWriteOptions(next, info, spatialInfo)
	return next
}

func spatialInfoForWriteOptions(opts *format.WriteOptions, info *datatype.TableInfo, fallback *datatype.SpatialInfo) *datatype.SpatialInfo {
	var spatialInfo *datatype.SpatialInfo
	if opts != nil && opts.SpatialInfo != nil {
		spatialInfo = opts.SpatialInfo.Clone()
	}
	if spatialInfo == nil {
		spatialInfo = fallback.Clone()
	}
	if spatialInfo == nil {
		spatialInfo = spatialInfoFromTableInfoOrFields(info)
	}
	geometryField := ""
	geometryType := ""
	if opts != nil && opts.ExtraParams != nil {
		geometryField = optionString(opts.ExtraParams, "geometry_field")
		geometryType = optionString(opts.ExtraParams, "geometry_type")
	}
	if geometryField == "" && geometryType == "" {
		return spatialInfo
	}
	srid := 0
	dimension := 0
	if spatialInfo != nil {
		srid = spatialInfo.PrimarySRIDValue()
		dimension = spatialInfo.PrimaryDimensionValue()
	}
	if geometryField != "" && info != nil {
		for i := range info.Fields {
			if strings.EqualFold(info.Fields[i].Name, geometryField) {
				info.Fields[i].Type = datatype.FieldTypeGeometry
				break
			}
		}
	}
	if geometryField == "" && spatialInfo != nil {
		geometryField = spatialInfo.PrimaryGeometryName()
	}
	if geometryType == "" && spatialInfo != nil {
		geometryType = spatialInfo.PrimaryGeometryType()
	}
	if geometryField == "" && geometryType == "" {
		return spatialInfo
	}
	return datatype.NewSingleGeometrySpatialInfo(geometryField, geometryType, srid, dimension)
}

func optionString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
