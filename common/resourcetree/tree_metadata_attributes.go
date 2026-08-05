package resourcetree

import (
	"strings"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
)

// 本文件只从 Meta attributes 中派生 TreeNode.Metadata 的展示摘要。
// 它不是通用 attributes 规范 API，不承担 attributes 兼容读取或持久化构造。

func tableInfoForTreeMetadata(attrs map[string]interface{}, fallbackName string) *datatype.TableInfo {
	return datatype.TableInfoFromPayload(commonJSON.Section(attrs, "type_info.table"), fallbackName)
}

func spatialInfoForTreeMetadata(attrs map[string]interface{}) *datatype.SpatialInfo {
	return datatype.SpatialInfoFromPayload(commonJSON.Section(attrs, "capabilities.spatial"))
}

func itemLayoutForTreeMetadata(attrs map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(commonJSON.StringFromSections(attrs, "layout", "item")))
}

func dataTypeForTreeMetadata(attrs map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(commonJSON.StringFromSections(attrs, "data_type", "item")))
}

func formatNameForTreeMetadata(attrs map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(commonJSON.StringFromSections(attrs, "format", "item")))
}

func physicalPathForTreeMetadata(attrs map[string]interface{}) string {
	return strings.TrimSpace(commonJSON.String(attrs, "storage", "physical_path"))
}

func objectCountForTreeMetadata(attrs map[string]interface{}) int64 {
	return commonJSON.Int64(attrs, "storage", "object_count")
}

func tableFieldCountForTreeMetadata(attrs map[string]interface{}) int {
	tableInfo := tableInfoForTreeMetadata(attrs, "")
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

func tableRowCountForTreeMetadata(attrs map[string]interface{}) int64 {
	tableInfo := tableInfoForTreeMetadata(attrs, "")
	if tableInfo == nil || tableInfo.RowCount == nil {
		return 0
	}
	return *tableInfo.RowCount
}

func spatialSummaryForTreeMetadata(attrs map[string]interface{}) map[string]interface{} {
	info := spatialInfoForTreeMetadata(attrs)
	if info == nil {
		return nil
	}
	geometryColumn := info.PrimaryGeometryName()
	summary := map[string]interface{}{}
	geometryColumns := make([]string, 0, len(info.GeometryColumns))
	seenGeometryColumns := map[string]bool{}
	for _, column := range info.GeometryColumns {
		name := strings.TrimSpace(column.Name)
		if name == "" || seenGeometryColumns[name] {
			continue
		}
		seenGeometryColumns[name] = true
		geometryColumns = append(geometryColumns, name)
	}
	if len(geometryColumns) > 0 {
		summary["geometry_columns"] = geometryColumns
	}
	if geometryColumn != "" {
		summary["primary_geometry_column"] = geometryColumn
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
