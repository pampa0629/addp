package catalogview

import (
	"strings"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
)

func tableInfoFromMetaAttributes(attrs map[string]interface{}, fallbackName string) *datatype.TableInfo {
	return datatype.TableInfoFromPayload(commonJSON.Section(attrs, "type_info.table"), fallbackName)
}

func spatialInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.SpatialInfo {
	return datatype.SpatialInfoFromPayload(commonJSON.Section(attrs, "capabilities.spatial"))
}

func itemLayoutFromMetaAttributes(attrs map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(commonJSON.StringFromSections(attrs, "layout", "item")))
}

func dataTypeFromMetaAttributes(attrs map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(commonJSON.StringFromSections(attrs, "data_type", "item")))
}

func formatNameFromMetaAttributes(attrs map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(commonJSON.StringFromSections(attrs, "format", "item")))
}

func physicalPathFromMetaAttributes(attrs map[string]interface{}) string {
	return strings.TrimSpace(commonJSON.String(attrs, "storage", "physical_path"))
}

func objectCountFromMetaAttributes(attrs map[string]interface{}) int64 {
	return commonJSON.Int64(attrs, "storage", "object_count")
}

func tableFieldCountFromMetaAttributes(attrs map[string]interface{}) int {
	tableInfo := tableInfoFromMetaAttributes(attrs, "")
	if tableInfo == nil {
		return 0
	}
	if len(tableInfo.Fields) > 0 {
		return len(tableInfo.Fields)
	}
	tableAttrs := commonJSON.Section(attrs, "type_info.table")
	for _, key := range []string{"field_count", "column_count"} {
		if count := int(commonJSON.InterfaceInt64(tableAttrs[key])); count > 0 {
			return count
		}
	}
	return 0
}

func tableRowCountFromMetaAttributes(attrs map[string]interface{}) int64 {
	tableInfo := tableInfoFromMetaAttributes(attrs, "")
	if tableInfo == nil || tableInfo.RowCount == nil {
		return 0
	}
	return *tableInfo.RowCount
}

func spatialSummaryFromMetaAttributes(attrs map[string]interface{}) map[string]interface{} {
	info := spatialInfoFromMetaAttributes(attrs)
	if info == nil {
		return nil
	}
	geometryColumn := info.PrimaryGeometryName()
	summary := map[string]interface{}{}
	if geometryColumn != "" {
		summary["geometry"] = geometryColumn
	}
	for _, column := range info.GeometryColumns {
		if geometryColumn == "" || strings.EqualFold(column.Name, geometryColumn) {
			if column.GeometryType != "" {
				summary["geometry_type"] = column.GeometryType
			}
			if column.SRID != nil {
				summary["srid"] = *column.SRID
			}
			break
		}
	}
	if _, ok := summary["srid"]; !ok && info.SRID != nil {
		summary["srid"] = *info.SRID
	}
	if len(summary) == 0 {
		return nil
	}
	return summary
}
