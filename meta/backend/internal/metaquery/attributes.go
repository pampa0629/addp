package metaquery

import (
	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
)

func FieldsFromMetaItem(item models.MetaItem) ([]datatype.FieldInfo, error) {
	info := tableInfoFromMetaAttributes(item.Attributes)
	if info == nil {
		return nil, nil
	}
	return append([]datatype.FieldInfo(nil), info.Fields...), nil
}

func SpatialMetadataFromItem(item models.MetaItem) (*models.SpatialMetadataResponse, error) {
	spatialMeta := &models.SpatialMetadataResponse{
		Fields: []datatype.FieldInfo{},
	}

	applySpatialInfo(spatialMeta, spatialInfoFromMetaAttributes(item.Attributes))
	applyTableInfo(spatialMeta, tableInfoFromMetaAttributes(item.Attributes))

	if item.RowCount != nil {
		spatialMeta.RowCount = *item.RowCount
	}

	return spatialMeta, nil
}

func tableInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.TableInfo {
	return datatype.TableInfoFromPayload(commonJSON.Section(attrs, "type_info.table"), "")
}

func spatialInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.SpatialInfo {
	return datatype.SpatialInfoFromPayload(commonJSON.Section(attrs, "capabilities.spatial"))
}

func applySpatialInfo(spatialMeta *models.SpatialMetadataResponse, spatialInfo *datatype.SpatialInfo) {
	if spatialMeta == nil || spatialInfo == nil {
		return
	}
	primary := spatialInfo.PrimaryGeometry()
	spatialMeta.GeometryColumn = spatialInfo.PrimaryGeometryName()
	if primary != nil {
		if primary.GeometryType != "" {
			spatialMeta.GeometryTypes = []string{primary.GeometryType}
		}
		if primary.SRID != nil {
			spatialMeta.SRID = *primary.SRID
		}
	}
	if spatialMeta.SRID == 0 && spatialInfo.SRID != nil {
		spatialMeta.SRID = *spatialInfo.SRID
	}
	if spatialInfo.Extent != nil {
		extent := *spatialInfo.Extent
		spatialMeta.Extent = []float64{extent[0], extent[1], extent[2], extent[3]}
	}
}

func applyTableInfo(spatialMeta *models.SpatialMetadataResponse, tableInfo *datatype.TableInfo) {
	if spatialMeta == nil || tableInfo == nil {
		return
	}
	if len(tableInfo.PrimaryKey) > 0 {
		spatialMeta.PrimaryKey = tableInfo.PrimaryKey[0]
	}
	if len(tableInfo.Fields) > 0 {
		spatialMeta.Fields = append([]datatype.FieldInfo(nil), tableInfo.Fields...)
	}
	if tableInfo.RowCount != nil {
		spatialMeta.RowCount = *tableInfo.RowCount
	}
}
