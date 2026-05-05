package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
	"gorm.io/gorm"
)

// DatabaseScanService 数据库扫描服务
// 职责：扫描关系型数据库（PostgreSQL、MySQL等）的Schema、Table、Field
type DatabaseScanService struct {
	db             *gorm.DB
	log            *slog.Logger
	indexer        *search.Indexer
	repo           *ScanRepository         // 数据访问层
	spatialService *SpatialMetadataService // 空间元数据扫描服务
	indexerService *IndexerService         // 索引服务
}

// NewDatabaseScanService 创建数据库扫描服务
func NewDatabaseScanService(db *gorm.DB, log *slog.Logger, indexer *search.Indexer, repo *ScanRepository, spatialService *SpatialMetadataService, indexerService *IndexerService) *DatabaseScanService {
	return &DatabaseScanService{
		db:             db,
		log:            log,
		indexer:        indexer,
		repo:           repo,
		spatialService: spatialService,
		indexerService: indexerService,
	}
}

// ScanSchema 扫描数据库Schema及其所有表
//
// 职责划分：
// 1. Schema节点管理：创建/更新Schema节点，管理扫描状态
// 2. 表迭代处理：扫描所有表，判断是否需要更新
// 3. 字段扫描：深度扫描时获取表字段信息
// 4. 空间元数据：提取PostGIS等空间类型的元数据
// 5. 搜索索引：将表资产信息同步到Meilisearch
// 6. 软删除处理：清理已删除的表
//
// 参数：
//   - ctx: 上下文
//   - resource: 数据源引擎配置
//   - tenantID: 租户ID
//   - engineID: 引擎ID
//   - schemaName: Schema名称
//   - scanDepth: 扫描深度 ("quick"快速扫描 | "deep"深度扫描)
//
// 返回：(schema数量, 表数量, 字段数量, error)
func (s *DatabaseScanService) ScanSchema(ctx context.Context, resource *commonModels.Engine, tenantID, engineID uint, schemaName string, scanDepth string) (int, int, int, error) {
	// 1. 获取插件
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	metadataProvider, ok := p.(plugin.ItemMetadataProvider)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement ItemMetadataProvider", resource.EngineType)
	}

	db := s.tryOpenConnectionPool(resource)

	// 2. 创建/更新 Schema/Database 节点
	schemaNode, err := s.repo.UpsertNode(tenantID, engineID, nil, namespaceTermForPlugin(p), schemaName, nil, nil)
	if err != nil {
		return 0, 0, 0, err
	}

	if err := s.repo.ResetNodeState(schemaNode, "running"); err != nil {
		return 0, 0, 0, err
	}

	// 3. 扫描表
	tables, fields, err := s.scanTables(ctx, resource, catalogProvider, metadataProvider, db, tenantID, engineID, schemaNode, schemaName, scanDepth)
	if err != nil {
		s.repo.FinalizeNodeState(schemaNode, "pending", 0, 0, err.Error())
		return 0, 0, 0, err
	}

	// 6. 完成扫描
	var totalSize int64
	for _, item := range getTableItems(s.db, tenantID, engineID, schemaNode.ID) {
		if item.SizeBytes != nil {
			totalSize += *item.SizeBytes
		}
	}

	if err := s.repo.FinalizeNodeState(schemaNode, "completed", tables, totalSize, ""); err != nil {
		return 0, tables, fields, err
	}

	return 1, tables, fields, nil
}

// scanTables 扫描Schema下的所有表
func (s *DatabaseScanService) scanTables(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	metadataProvider plugin.ItemMetadataProvider,
	db *gorm.DB,
	tenantID, engineID uint,
	schemaNode *models.MetaNode,
	schemaName string,
	scanDepth string,
) (int, int, error) {
	isDeepScan := strings.EqualFold(scanDepth, "deep")

	// 查询已存在的表
	existingTableMap := getExistingTableMap(s.db, tenantID, engineID, schemaNode.ID)

	s.log.Info("开始扫描 Schema",
		"tenant_id", tenantID,
		"engine_id", engineID,
		"schema", schemaName,
		"scan_depth", scanDepth,
		"existing_tables", len(existingTableMap),
	)

	pluginTables, err := s.listTables(ctx, resource, catalogProvider, schemaName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list tables: %w", err)
	}

	// ScannerTableInfo 是旧 Scanner 适配层结构，数据库扫描在此处做一次边界转换。
	// 新的 item 语义（data_family、format 等）写入 attrs，不再扩展 ScannerTableInfo。
	tables := make([]format.ScannerTableInfo, len(pluginTables))
	for i, t := range pluginTables {
		tables[i] = format.ScannerTableInfo{
			Name:         t.TableName,
			Type:         "BASE TABLE", // plugin.TableInfo 没有 Type 字段，默认为 BASE TABLE
			Comment:      "",           // plugin.TableInfo 没有 Comment 字段
			RowCount:     t.RowCount,
			SizeBytes:    t.SizeBytes,
			LastModified: t.LastModified, // 表的最后修改时间（用于增量扫描判断）
		}
	}

	s.log.Info("扫描到的表",
		"schema", schemaName,
		"tables_count", len(tables),
	)

	totalTables := 0
	totalFields := 0
	scannedTables := make(map[string]bool)

	// 处理每张表
	for i, tableInfo := range tables {
		s.log.Info(fmt.Sprintf("处理第 %d/%d 张表", i+1, len(tables)),
			"table_name", tableInfo.Name,
			"row_count", tableInfo.RowCount,
		)

		scannedTables[tableInfo.Name] = true

		// 检查是否需要更新
		existingItem := existingTableMap[tableInfo.Name]
		needsUpdate := shouldUpdateTable(existingItem, tableInfo)

		// 浅度扫描且表未变化，跳过
		if !isDeepScan && existingItem != nil && !needsUpdate {
			s.log.Debug("表未变化，跳过更新",
				"schema", schemaName,
				"table", tableInfo.Name,
			)
			totalTables++
			continue
		}

		// 扫描表字段和元数据
		fields, attrs, err := s.scanTableDetails(ctx, resource, metadataProvider, db, schemaName, tableInfo, existingItem, isDeepScan)
		if err != nil {
			s.log.Warn("表扫描失败，跳过",
				"schema", schemaName,
				"table", tableInfo.Name,
				"error", err,
			)
			continue
		}

		// 持久化表元数据
		fullName := composeNodeFullName(tableInfo.Name, schemaNode, ".")
		rowCount := tableInfo.RowCount
		sizeBytes := tableInfo.SizeBytes

		item, err := s.repo.UpsertItem(tenantID, engineID, schemaNode, "table", tableInfo.Name, fullName, attrs, &rowCount, &sizeBytes, tableInfo.LastModified)
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

		// 深度扫描时索引表资产
		if isDeepScan && s.indexerService != nil {
			s.indexerService.IndexTableAsset(resource, tenantID, schemaName, tableInfo, fields, item)
		}

		totalTables++
		totalFields += len(fields)
	}

	// 软删除不存在的表
	s.deleteRemovedTables(tenantID, engineID, schemaName, existingTableMap, scannedTables)

	s.log.Info("Schema 扫描完成",
		"schema", schemaName,
		"tables", totalTables,
		"fields", totalFields,
	)

	return totalTables, totalFields, nil
}

func (s *DatabaseScanService) listTables(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	schemaName string,
) ([]plugin.TableInfo, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
		Segments: []plugin.CatalogSegment{{
			Term: namespaceTermForPlugin(catalogProvider),
			Kind: plugin.CatalogKindNamespace,
			Name: schemaName,
		}},
	}, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	tables := make([]plugin.TableInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsItem {
			continue
		}
		rowCount, _ := int64Stat(node.Stats, "row_count")
		sizeBytes, _ := int64Stat(node.Stats, "size_bytes")
		tables = append(tables, plugin.TableInfo{
			Schema:    schemaName,
			TableName: node.Name,
			RowCount:  rowCount,
			SizeBytes: sizeBytes,
		})
	}
	return tables, nil
}

func (s *DatabaseScanService) tryOpenConnectionPool(resource *commonModels.Engine) *gorm.DB {
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil
	}
	if _, ok := p.(plugin.ConnectionPoolPlugin); !ok {
		return nil
	}
	db, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
		ID:             resource.ID,
		EngineType:     resource.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
	}, nil)
	if err != nil {
		s.log.Warn("创建连接池失败，跳过连接池增强元数据",
			"engine_id", resource.ID,
			"engine_type", resource.EngineType,
			"error", err,
		)
		return nil
	}
	return db
}

// scanTableDetails 扫描表的详细信息（字段、空间元数据等）
func (s *DatabaseScanService) scanTableDetails(
	ctx context.Context,
	resource *commonModels.Engine,
	metadataProvider plugin.ItemMetadataProvider,
	db *gorm.DB,
	schemaName string,
	tableInfo format.ScannerTableInfo,
	existingItem *models.MetaItem,
	isDeepScan bool,
) ([]format.ScannerFieldInfo, models.JSONMap, error) {
	var fields []format.ScannerFieldInfo
	var attrs models.JSONMap

	if isDeepScan {
		pluginColumns, err := s.listColumns(ctx, resource, metadataProvider, schemaName, tableInfo.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("字段扫描失败: %w", err)
		}

		// ScannerFieldInfo 是旧 Scanner 适配层结构，字段级标准语义后续应收口到 format.FieldInfo。
		fields = make([]format.ScannerFieldInfo, len(pluginColumns))
		for i, col := range pluginColumns {
			fields[i] = format.ScannerFieldInfo{
				Name:         col.ColumnName,
				DataType:     col.DataType,
				IsNullable:   col.IsNullable,
				IsPrimaryKey: col.IsPrimaryKey,
				Comment:      col.Comment,
			}
		}

		s.log.Info("字段扫描成功",
			"table", tableInfo.Name,
			"field_count", len(fields),
		)
		// 提取主键信息
		primaryKeyColumns := []string{}
		for _, field := range fields {
			if field.IsPrimaryKey {
				primaryKeyColumns = append(primaryKeyColumns, field.Name)
			}
		}

		// 如果有主键，查询主键约束名
		var primaryKeyName string
		if len(primaryKeyColumns) > 0 && db != nil {
			primaryKeyName, _ = s.queryPrimaryKeyName(ctx, db, schemaName, tableInfo.Name)
		}

		fieldsAttr := buildFieldAttributes(fields)
		tableMetadata := map[string]interface{}{
			"primary_key":      primaryKeyColumns,
			"primary_key_name": primaryKeyName,
			"has_primary_key":  len(primaryKeyColumns) > 0,
		}
		attrs = models.JSONMap{
			"schema_name":    schemaName,
			"table_type":     tableInfo.Type,
			"table_comment":  tableInfo.Comment,
			"fields":         fieldsAttr,
			"table_metadata": tableMetadata,
			"schema": map[string]interface{}{
				"fields":         fieldsAttr,
				"table_metadata": tableMetadata,
				"table_type":     tableInfo.Type,
				"table_comment":  tableInfo.Comment,
			},
		}

		// 扫描空间元数据（仅 PostgreSQL）
		if resource.EngineType == "postgresql" && db != nil {
			spatialMeta := s.scanSpatialMetadata(ctx, db, schemaName, tableInfo.Name)
			if spatialMeta != nil {
				setExtensionAttribute(attrs, "spatial", "spatial_metadata", spatialMeta)
				s.log.Info("空间元数据扫描成功",
					"table", tableInfo.Name,
					"geometry_column", spatialMeta.GeometryColumn,
				)
			}
		}
	} else {
		// 浅度扫描：保留已有字段信息
		if existingItem != nil && existingItem.Attributes != nil {
			attrs = existingItem.Attributes
			attrs["schema_name"] = schemaName
			attrs["table_type"] = tableInfo.Type
			attrs["table_comment"] = tableInfo.Comment
			upsertSection(attrs, "schema", map[string]interface{}{
				"table_type":    tableInfo.Type,
				"table_comment": tableInfo.Comment,
			})
		} else {
			attrs = models.JSONMap{
				"schema_name":   schemaName,
				"table_type":    tableInfo.Type,
				"table_comment": tableInfo.Comment,
				"schema": map[string]interface{}{
					"table_type":    tableInfo.Type,
					"table_comment": tableInfo.Comment,
				},
			}
		}
	}
	setItemAttribute(attrs, "data_family", string(dataitem.DataFamilyTabular))
	setItemAttribute(attrs, "format", resource.EngineType)

	return fields, attrs, nil
}

func (s *DatabaseScanService) listColumns(
	ctx context.Context,
	resource *commonModels.Engine,
	metadataProvider plugin.ItemMetadataProvider,
	schemaName string,
	tableName string,
) ([]plugin.ColumnInfo, error) {
	item, err := metadataProvider.DescribeItem(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
		Segments: []plugin.CatalogSegment{
			{Term: namespaceTermForPlugin(metadataProvider), Kind: plugin.CatalogKindNamespace, Name: schemaName},
			{Term: plugin.CatalogTermTable, Kind: plugin.CatalogKindTable, Name: tableName},
		},
	}, plugin.MetadataOptions{})
	if err != nil {
		return nil, err
	}
	columns := make([]plugin.ColumnInfo, 0, len(item.Fields))
	for _, field := range item.Fields {
		dataType := field.NativeType
		if dataType == "" {
			dataType = field.Type
		}
		columns = append(columns, plugin.ColumnInfo{
			ColumnName:   field.Name,
			DataType:     dataType,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		})
	}
	return columns, nil
}

// scanSpatialMetadata 扫描PostGIS空间元数据
func (s *DatabaseScanService) scanSpatialMetadata(ctx context.Context, db *gorm.DB, schemaName, tableName string) *models.SpatialMetadata {
	// 获取底层的 *sql.DB
	sqlDB, err := db.DB()
	if err != nil {
		s.log.Warn("获取 sql.DB 失败",
			"schema", schemaName,
			"table", tableName,
			"error", err,
		)
		return nil
	}

	spatialMeta, err := s.spatialService.ScanTableSpatialMetadata(ctx, sqlDB, schemaName, tableName)
	if err != nil {
		s.log.Warn("空间元数据扫描失败",
			"schema", schemaName,
			"table", tableName,
			"error", err,
		)
		return nil
	}

	return spatialMeta
}

// deleteRemovedTables 软删除已移除的表
func (s *DatabaseScanService) deleteRemovedTables(
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
			// 从索引中删除
			if s.indexerService != nil {
				s.indexerService.DeleteTablesFromIndex(tenantID, engineID, schemaName)
			}
		}
	}
}

// 辅助函数

func getExistingTableMap(db *gorm.DB, tenantID, engineID, nodeID uint) map[string]*models.MetaItem {
	var existingItems []models.MetaItem
	if err := db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ?",
		tenantID, engineID, nodeID, "table").Find(&existingItems).Error; err != nil {
		return make(map[string]*models.MetaItem)
	}

	existingTableMap := make(map[string]*models.MetaItem)
	for i := range existingItems {
		existingTableMap[existingItems[i].Name] = &existingItems[i]
	}
	return existingTableMap
}

func getTableItems(db *gorm.DB, tenantID, engineID, nodeID uint) []models.MetaItem {
	var items []models.MetaItem
	db.Where("tenant_id = ? AND engine_id = ? AND node_id = ?",
		tenantID, engineID, nodeID).Find(&items)
	return items
}

func shouldUpdateTable(existingItem *models.MetaItem, tableInfo format.ScannerTableInfo) bool {
	if existingItem == nil {
		return true
	}

	// 表的修改时间变化（优先判断）
	// 如果表有 LastModified 且数据库中有 DataUpdatedAt，则比较时间戳
	if tableInfo.LastModified != nil && existingItem.DataUpdatedAt != nil {
		if tableInfo.LastModified.After(*existingItem.DataUpdatedAt) {
			return true // 表有更新，需要重新扫描
		}
		// 表的 LastModified 早于或等于上次扫描时间，且有时间戳记录，说明表未变化
		// 即使行数/大小有差异也可能是统计信息不准确，跳过扫描
		return false
	}

	// 如果没有时间戳信息，降级到行数/大小比较（传统逻辑）

	// 行数变化
	if existingItem.RowCount != nil && *existingItem.RowCount != tableInfo.RowCount {
		return true
	}

	// 大小变化
	if existingItem.SizeBytes != nil && *existingItem.SizeBytes != tableInfo.SizeBytes {
		return true
	}

	// 之前未记录行数或大小
	if (existingItem.RowCount == nil && tableInfo.RowCount != 0) ||
		(existingItem.SizeBytes == nil && tableInfo.SizeBytes != 0) {
		return true
	}

	return false
}

// queryPrimaryKeyName 查询主键约束名称
func (s *DatabaseScanService) queryPrimaryKeyName(
	ctx context.Context,
	db *gorm.DB,
	schema, table string,
) (string, error) {
	query := `
		SELECT constraint_name
		FROM information_schema.table_constraints
		WHERE table_schema = $1 AND table_name = $2
		  AND constraint_type = 'PRIMARY KEY'
		LIMIT 1
	`

	var constraintName string
	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&constraintName).Error
	if err != nil {
		return "", err
	}

	return constraintName, nil
}
