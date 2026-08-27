package scanruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanchange"
)

// scanTables 扫描Schema下的所有表。
func (s *DatabaseRuntime) scanTables(
	ctx context.Context,
	resource *commonModels.Engine,
	scanCatalog databaseScanCatalog,
	tenantID, engineID uint,
	schemaNode *models.MetaNode,
	schemaName string,
	scanDepth string,
	force bool,
) (int, int, error) {
	isDeepScan := strings.EqualFold(scanDepth, "deep")

	existingTableMap := s.repo.GetItemsByNodeAndTypeMap(tenantID, engineID, schemaNode.ID, scanCatalog.itemTerm)

	s.log.Info("开始扫描 Schema",
		"tenant_id", tenantID,
		"engine_id", engineID,
		"schema", schemaName,
		"scan_depth", scanDepth,
		"existing_tables", len(existingTableMap),
	)

	pluginTables, err := s.listTables(ctx, resource, scanCatalog, schemaName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list tables: %w", err)
	}

	s.log.Info("扫描到的表",
		"schema", schemaName,
		"tables_count", len(pluginTables),
	)

	totalTables := 0
	totalFields := 0
	scannedTables := make(map[string]bool)

	for i, tableInfo := range pluginTables {
		if err := ctx.Err(); err != nil {
			return totalTables, totalFields, err
		}
		s.log.Info(fmt.Sprintf("处理第 %d/%d 张表", i+1, len(pluginTables)),
			"table_name", tableInfo.Name,
			"row_count", tableInfo.RowCount,
			"estimated_row_count", tableInfo.EstimatedRowCount,
		)

		scannedTables[tableInfo.Name] = true
		if isDeepScan {
			tableInfo.RowCount = s.resolveTableRowCount(ctx, resource, scanCatalog, schemaName, tableInfo)
		}

		existingItem := existingTableMap[tableInfo.Name]
		needsUpdate := force || scanchange.ShouldUpdateTable(existingItem, tableInfo) || existingItem == nil
		if isDeepScan && existingItem != nil && existingItem.ScannedDepth != models.ScannedDepthDeep {
			needsUpdate = true
		}

		if existingItem != nil && !needsUpdate {
			s.log.Debug("表未变化，跳过更新",
				"schema", schemaName,
				"table", tableInfo.Name,
			)
			totalTables++
			continue
		}

		fields, attrs, err := s.scanTableDetails(ctx, resource, scanCatalog, schemaName, tableInfo, existingItem, isDeepScan)
		if err != nil {
			s.log.Warn("表扫描失败，跳过",
				"schema", schemaName,
				"table", tableInfo.Name,
				"error", err,
			)
			continue
		}

		fullName := metapath.ComposeNodeFullName(tableInfo.Name, schemaNode, ".")
		rowCount := tableInfo.RowCount
		if rowCount == nil && !isDeepScan && existingItem != nil {
			rowCount = existingItem.RowCount
		}
		sizeBytes := derefInt64Ptr(tableInfo.SizeBytes)

		item, err := s.repo.UpsertItemWithDepth(tenantID, engineID, schemaNode, scanCatalog.itemTerm, tableInfo.Name, fullName, attrs, rowCount, &sizeBytes, tableInfo.UpdatedAt, scanDepth)
		if err != nil {
			s.log.Error("表元数据持久化失败",
				"schema", schemaName,
				"table", tableInfo.Name,
				"error", err,
			)
			continue
		}

		s.log.Info("表元数据写入成功",
			"table", tableInfo.Name,
			"item_id", item.ID,
		)

		if isDeepScan && s.tableIndexer != nil {
			s.tableIndexer.IndexTableContent(ctx, resource, tenantID, schemaName, tableInfo, fields, item)
		}

		totalTables++
		totalFields += len(fields)
	}

	s.deleteRemovedTables(tenantID, engineID, schemaName, existingTableMap, scannedTables)

	s.log.Info("Schema 扫描完成",
		"schema", schemaName,
		"tables", totalTables,
		"fields", totalFields,
	)

	return totalTables, totalFields, nil
}

func (s *DatabaseRuntime) listTables(
	ctx context.Context,
	resource *commonModels.Engine,
	scanCatalog databaseScanCatalog,
	schemaName string,
) ([]datatype.TableInfo, error) {
	parentPath := plugin.TabularNamespacePath(resource.ID, scanCatalog.namespaceTerm, schemaName)
	nodes, err := scanCatalog.catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), parentPath, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	tables := make([]datatype.TableInfo, 0, len(nodes))
	for _, node := range nodes {
		if node.Role != plugin.EngineCatalogRoleLeaf {
			continue
		}
		if node.Table != nil {
			tables = append(tables, *node.Table.Clone())
			continue
		}
		tables = append(tables, datatype.TableInfo{
			Name: node.Name,
			Kind: node.Kind,
		})
	}
	return tables, nil
}

func (s *DatabaseRuntime) resolveTableRowCount(ctx context.Context, resource *commonModels.Engine, scanCatalog databaseScanCatalog, schemaName string, tableInfo datatype.TableInfo) *int64 {
	if tableInfo.RowCount != nil && *tableInfo.RowCount >= 0 {
		return tableInfo.RowCount
	}
	path := plugin.TabularItemPath(resource.ID, scanCatalog.namespaceTerm, schemaName, tableInfo.Name)
	facts, err := scanCatalog.factsProvider.DescribeEngineCatalogFacts(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true})
	if err != nil || facts == nil {
		s.log.Debug("表行数精确查询失败，保留 catalog 统计值",
			"engine_id", resource.ID,
			"schema", schemaName,
			"table", tableInfo.Name,
			"error", err,
		)
		return tableInfo.RowCount
	}
	described := plugin.EngineCatalogFactsTableInfo(facts)
	if described == nil || described.RowCount == nil || *described.RowCount < 0 {
		return tableInfo.RowCount
	}
	return described.RowCount
}

// deleteRemovedTables 软删除已移除的表。
func (s *DatabaseRuntime) deleteRemovedTables(
	tenantID, engineID uint,
	schemaName string,
	existingTableMap map[string]*models.MetaItem,
	scannedTables map[string]bool,
) {
	for tableName, item := range existingTableMap {
		if !scannedTables[tableName] {
			s.log.Info("表已不存在，标记删除",
				"schema", schemaName,
				"table", tableName,
			)
			if err := s.db.Delete(item).Error; err != nil {
				s.log.Warn("软删除表元数据失败",
					"schema", schemaName,
					"table", tableName,
					"error", err,
				)
			}
			if s.tableIndexer != nil {
				s.tableIndexer.DeleteTablesFromIndex(tenantID, engineID, schemaName)
			}
		}
	}
}
