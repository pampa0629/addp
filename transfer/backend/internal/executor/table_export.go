package executor

import (
	"github.com/addp/common/datatype"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
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
			Type:         datatype.FieldType(field.Type),
			Nullable:     field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		})
		applySpatialInfoFromField(info, field)
	}
	if len(info.Fields) == 0 && len(batch.Rows) > 0 {
		names := make([]string, 0, len(batch.Rows[0]))
		for name := range batch.Rows[0] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			info.Fields = append(info.Fields, format.FieldInfo{Name: name, Type: datatype.FieldTypeUnknown})
		}
	}
	return info
}

func applySpatialInfoFromField(info *format.TableInfo, field engineplugin.FieldInfo) {
	if info == nil || !datatype.IsSpatialFieldType(datatype.FieldType(field.Type)) {
		return
	}
	geometryType := ""
	srid := 0
	dimension := 0
	if field.Attributes == nil {
		if info.SpatialInfo.PrimaryGeometryName() == "" {
			info.SpatialInfo = datatype.NewSingleGeometrySpatialInfo(field.Name, "", 0, 0)
		}
		return
	}
	geometryType = commonJSON.InterfaceString(field.Attributes["geometry_type"])
	srid = int(commonJSON.InterfaceInt64(field.Attributes["srid"]))
	dimension = int(commonJSON.InterfaceInt64(field.Attributes["dimension"]))
	if info.SpatialInfo == nil || info.SpatialInfo.PrimaryGeometryName() == "" {
		info.SpatialInfo = datatype.NewSingleGeometrySpatialInfo(field.Name, geometryType, srid, dimension)
		return
	}
	column := info.SpatialInfo.PrimaryGeometry()
	if column == nil || column.Name != field.Name {
		return
	}
	if column.GeometryType == "" {
		column.GeometryType = geometryType
	}
	if column.SRID == nil && srid > 0 {
		column.SRID = &srid
	}
	if column.Dimension == nil && dimension > 0 {
		column.Dimension = &dimension
	}
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
