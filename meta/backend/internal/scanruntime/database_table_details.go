package scanruntime

import (
	"context"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
)

// scanTableDetails 扫描表的详细信息（字段、空间事实等）。
func (s *DatabaseRuntime) scanTableDetails(
	ctx context.Context,
	resource *commonModels.Engine,
	scanCatalog databaseScanCatalog,
	schemaName string,
	tableInfo datatype.TableInfo,
	existingItem *models.MetaItem,
	isDeepScan bool,
) ([]datatype.FieldInfo, models.JSONMap, error) {
	var fields []datatype.FieldInfo
	var attrs models.JSONMap

	if isDeepScan {
		describedFacts, err := s.describeTableFacts(ctx, resource, scanCatalog, schemaName, tableInfo.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("字段扫描失败: %w", err)
		}
		describedTable := datatype.TableInfo{Name: tableInfo.Name}
		if factsTable := plugin.CatalogFactsTableInfo(describedFacts); factsTable != nil {
			describedTable = *factsTable
		}
		tableInfo = mergeDatabaseTableInfo(tableInfo, describedTable)
		fields = append([]datatype.FieldInfo(nil), tableInfo.Fields...)

		s.log.Info("字段扫描成功",
			"table", tableInfo.Name,
			"field_count", len(fields),
		)
		primaryKeyColumns := []string{}
		for _, field := range fields {
			if field.PrimaryKey {
				primaryKeyColumns = append(primaryKeyColumns, field.Name)
			}
		}

		tableInfo.Kind = normalizedTableKind(tableInfo)
		tableInfo.Fields = fields
		tableInfo.PrimaryKey = primaryKeyColumns
		attrs = tableItemAttributes(schemaName, tableInfo)

		if spatialInfo := plugin.CatalogFactsSpatialInfo(describedFacts); spatialInfo != nil {
			metaattr.MergeStandardAttributes(attrs, metaattr.TableDescribeAttributes(metaattr.TableDescribeAttributesInput{
				Spatial: spatialInfo,
			}))
			s.log.Info("空间事实读取成功",
				"table", tableInfo.Name,
				"geometry_column", spatialInfo.PrimaryGeometryName(),
			)
		}
	} else {
		if existingItem != nil && existingItem.Attributes != nil {
			attrs = existingItem.Attributes
			metaattr.SetStorage(attrs, "schema_name", schemaName)
			tableInfo.Kind = normalizedTableKind(tableInfo)
			metaattr.ApplyTableItemAttributes(attrs, &tableInfo)
		} else {
			tableInfo.Kind = normalizedTableKind(tableInfo)
			attrs = tableItemAttributes(schemaName, tableInfo)
		}
	}
	metaattr.ApplyTableItemAttributes(attrs, &tableInfo)

	return fields, attrs, nil
}

func tableItemAttributes(schemaName string, tableInfo datatype.TableInfo) models.JSONMap {
	attrs := models.JSONMap{}
	metaattr.SetStorage(attrs, "schema_name", schemaName)
	metaattr.ApplyTableItemAttributes(attrs, &tableInfo)
	return attrs
}

func derefInt64Ptr(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func normalizedTableKind(table datatype.TableInfo) string {
	if table.Kind != "" {
		return table.Kind
	}
	return "table"
}
