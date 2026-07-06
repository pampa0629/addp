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
	applySpatialCRSDefinitionWriteOption(next)
	return next
}

func applySpatialCRSDefinitionWriteOption(opts *format.WriteOptions) {
	if opts == nil || opts.SpatialInfo == nil {
		return
	}
	if opts.ExtraParams != nil {
		if existing := strings.TrimSpace(optionString(opts.ExtraParams, format.CRSDefinitionOptionKey)); existing != "" {
			return
		}
	}
	definition := primaryCRSDefinition(opts.SpatialInfo)
	if definition == nil || strings.TrimSpace(definition.Definition) == "" {
		return
	}
	switch strings.TrimSpace(definition.DefinitionEncoding) {
	case datatype.CRSDefinitionEncodingWKT, datatype.CRSDefinitionEncodingESRIWKT:
	default:
		return
	}
	if opts.ExtraParams == nil {
		opts.ExtraParams = map[string]interface{}{}
	}
	opts.ExtraParams[format.CRSDefinitionOptionKey] = strings.TrimSpace(definition.Definition)
}

func primaryCRSDefinition(spatialInfo *datatype.SpatialInfo) *datatype.CRSDefinition {
	if spatialInfo == nil {
		return nil
	}
	if ref := spatialInfo.PrimaryCRSRef(); ref != "" {
		if definition := spatialInfo.CRSDefinitionByID(ref); definition != nil {
			return definition
		}
	}
	if len(spatialInfo.CRSDefinitions) == 1 {
		return &spatialInfo.CRSDefinitions[0]
	}
	return nil
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
	if datatype.ParseGeometryType(geometryType) == datatype.GeometryTypeGeometry && spatialInfo != nil {
		if fallbackGeometryType := strings.TrimSpace(spatialInfo.PrimaryGeometryType()); fallbackGeometryType != "" && datatype.ParseGeometryType(fallbackGeometryType) != datatype.GeometryTypeGeometry {
			geometryType = fallbackGeometryType
		}
	}
	if geometryField == "" && geometryType == "" {
		return spatialInfo
	}
	next := datatype.NewSingleGeometrySpatialInfo(geometryField, geometryType, srid, dimension)
	if spatialInfo != nil {
		next.CRSRef = spatialInfo.CRSRef
		next.CRSDefinitions = append([]datatype.CRSDefinition(nil), spatialInfo.CRSDefinitions...)
		next.IndexName = spatialInfo.IndexName
		if spatialInfo.Extent != nil {
			extent := *spatialInfo.Extent
			next.Extent = &extent
		}
		if spatialInfo.HasSpatialIndex != nil {
			hasSpatialIndex := *spatialInfo.HasSpatialIndex
			next.HasSpatialIndex = &hasSpatialIndex
		}
		if primary := spatialInfo.PrimaryGeometry(); primary != nil && len(next.GeometryColumns) > 0 {
			if strings.TrimSpace(next.GeometryColumns[0].CRSRef) == "" {
				next.GeometryColumns[0].CRSRef = strings.TrimSpace(primary.CRSRef)
			}
			if next.GeometryColumns[0].CRSRef == "" && primary.SRID != nil && *primary.SRID > 0 {
				next.GeometryColumns[0].CRSRef = datatype.EPSGCRSRef(*primary.SRID)
			}
		}
	}
	return next
}

func optionString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
