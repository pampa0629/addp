package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/database/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
	"github.com/addp/meta/plugins"
	"gorm.io/gorm"
)

// DatabaseScanService 数据库扫描服务
// 职责：扫描关系型数据库（PostgreSQL、MySQL等）的Schema、Table、Field
type DatabaseScanService struct {
	db               *gorm.DB
	log              *slog.Logger
	indexer          *search.Indexer
	repo             *ScanRepository // 数据访问层
	spatialService   *SpatialMetadataService // 空间元数据扫描服务
	indexerService   *IndexerService         // 索引服务
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

	// 2. 类型断言为 RelationalDBPlugin
	relPlugin, ok := p.(plugin.RelationalDBPlugin)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement RelationalDBPlugin", resource.EngineType)
	}

	// 3. 获取连接池
	db, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
		ID:             resource.ID,
		EngineType:     resource.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
	}, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// 4. 创建/更新 Schema 节点
	schemaNode, err := s.repo.UpsertNode(tenantID, engineID, nil, "schema", schemaName, "", nil)
	if err != nil {
		return 0, 0, 0, err
	}

	if err := s.repo.ResetNodeState(schemaNode, "扫描中"); err != nil {
		return 0, 0, 0, err
	}

	// 5. 扫描表
	tables, fields, err := s.scanTables(ctx, resource, relPlugin, db, tenantID, engineID, schemaNode, schemaName, scanDepth)
	if err != nil {
		s.repo.FinalizeNodeState(schemaNode, "未扫描", 0, 0, err.Error())
		return 0, 0, 0, err
	}

	// 6. 完成扫描
	var totalSize int64
	for _, item := range getTableItems(s.db, tenantID, engineID, schemaNode.ID) {
		if item.SizeBytes != nil {
			totalSize += *item.SizeBytes
		}
	}

	if err := s.repo.FinalizeNodeState(schemaNode, "已扫描", tables, totalSize, ""); err != nil {
		return 0, tables, fields, err
	}

	return 1, tables, fields, nil
}

// scanTables 扫描Schema下的所有表
func (s *DatabaseScanService) scanTables(
	ctx context.Context,
	resource *commonModels.Engine,
	relPlugin plugin.RelationalDBPlugin,
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

	// 扫描表列表（直接调用 RelationalDBPlugin）
	pluginTables, err := relPlugin.ListTables(ctx, db, schemaName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list tables: %w", err)
	}

	// 转换为 meta 内部的 TableInfo 格式
	tables := make([]plugins.TableInfo, len(pluginTables))
	for i, t := range pluginTables {
		tables[i] = plugins.TableInfo{
			Name:      t.TableName,
			Type:      "BASE TABLE", // plugin.TableInfo 没有 Type 字段，默认为 BASE TABLE
			Comment:   "",           // plugin.TableInfo 没有 Comment 字段
			RowCount:  t.RowCount,
			SizeBytes: t.SizeBytes,
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
		fields, attrs, err := s.scanTableDetails(ctx, resource, relPlugin, db, schemaName, tableInfo, existingItem, isDeepScan)
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

		item, err := s.repo.UpsertItem(tenantID, engineID, schemaNode, "table", tableInfo.Name, fullName, attrs, &rowCount, &sizeBytes, nil)
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

// scanTableDetails 扫描表的详细信息（字段、空间元数据等）
func (s *DatabaseScanService) scanTableDetails(
	ctx context.Context,
	resource *commonModels.Engine,
	relPlugin plugin.RelationalDBPlugin,
	db *gorm.DB,
	schemaName string,
	tableInfo plugins.TableInfo,
	existingItem *models.MetaItem,
	isDeepScan bool,
) ([]plugins.FieldInfo, models.JSONMap, error) {
	var fields []plugins.FieldInfo
	var attrs models.JSONMap

	if isDeepScan {
		// 扫描字段（直接调用 RelationalDBPlugin）
		pluginColumns, err := relPlugin.ListColumns(ctx, db, schemaName, tableInfo.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("字段扫描失败: %w", err)
		}

		// 转换为 meta 内部的 FieldInfo 格式
		fields = make([]plugins.FieldInfo, len(pluginColumns))
		for i, col := range pluginColumns {
			fields[i] = plugins.FieldInfo{
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

		attrs = models.JSONMap{
			"schema":        schemaName,
			"table_type":    tableInfo.Type,
			"table_comment": tableInfo.Comment,
			"fields":        buildFieldAttributes(fields),
		}

		// 扫描空间元数据（仅 PostgreSQL）
		if resource.EngineType == "postgresql" {
			spatialMeta := s.scanSpatialMetadata(ctx, db, schemaName, tableInfo.Name)
			if spatialMeta != nil {
				attrs["spatial_metadata"] = spatialMeta
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
			attrs["schema"] = schemaName
			attrs["table_type"] = tableInfo.Type
			attrs["table_comment"] = tableInfo.Comment
		} else {
			attrs = models.JSONMap{
				"schema":        schemaName,
				"table_type":    tableInfo.Type,
				"table_comment": tableInfo.Comment,
			}
		}
	}

	return fields, attrs, nil
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

func shouldUpdateTable(existingItem *models.MetaItem, tableInfo plugins.TableInfo) bool {
	if existingItem == nil {
		return true
	}

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
