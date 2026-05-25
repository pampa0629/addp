package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanchange"
	"github.com/addp/meta/internal/search"
	"gorm.io/gorm"
)

// DatabaseScanService 数据库扫描服务
// 职责：扫描关系型数据库（PostgreSQL、MySQL等）的Schema、Table、Field
type DatabaseScanService struct {
	db             *gorm.DB
	log            *slog.Logger
	indexer        *search.Indexer
	repo           *metaRepo.ScanRepository // 数据访问层
	spatialService *SpatialMetadataService  // 空间元数据扫描服务
	indexerService *IndexerService          // 索引服务
}

// NewDatabaseScanService 创建数据库扫描服务
func NewDatabaseScanService(db *gorm.DB, log *slog.Logger, indexer *search.Indexer, repo *metaRepo.ScanRepository, spatialService *SpatialMetadataService, indexerService *IndexerService) *DatabaseScanService {
	return &DatabaseScanService{
		db:             db,
		log:            log,
		indexer:        indexer,
		repo:           repo,
		spatialService: spatialService,
		indexerService: indexerService,
	}
}

// ScanNamespace 扫描数据库命名空间及其所有表
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
//   - namespaceName: 命名空间名称
//   - scanDepth: 扫描深度 ("quick"快速扫描 | "deep"深度扫描)
//
// 返回：(schema数量, 表数量, 字段数量, error)
func (s *DatabaseScanService) ScanNamespace(ctx context.Context, resource *commonModels.Engine, tenantID, engineID uint, namespaceName string, scanDepth string, force bool) (int, int, int, error) {
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
	itemTerm := catalogItemTermForPlugin(p, plugin.CatalogTermTable)

	db := s.tryOpenConnectionPool(resource)

	// 2. 创建/更新 Schema/Database 节点
	schemaNode, err := s.repo.UpsertNode(tenantID, engineID, nil, namespaceTermForPlugin(p), namespaceName, nil, nil)
	if err != nil {
		return 0, 0, 0, err
	}

	if err := s.repo.ResetNodeState(schemaNode, "running"); err != nil {
		return 0, 0, 0, err
	}

	// 3. 扫描表
	tables, fields, err := s.scanTables(ctx, resource, catalogProvider, metadataProvider, db, tenantID, engineID, schemaNode, namespaceName, scanDepth, force, itemTerm)
	if err != nil {
		s.repo.FinalizeNodeState(schemaNode, "pending", 0, 0, err.Error())
		return 0, 0, 0, err
	}

	// 6. 完成扫描
	var totalSize int64
	tableItems, err := s.repo.GetItemsByNodeAndType(tenantID, engineID, schemaNode.ID, itemTerm)
	if err != nil {
		return 0, tables, fields, err
	}
	for _, item := range tableItems {
		if item.SizeBytes != nil {
			totalSize += *item.SizeBytes
		}
	}

	if err := s.repo.FinalizeNodeStateWithDepth(schemaNode, "completed", tables, totalSize, "", scanDepth); err != nil {
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
	force bool,
	itemTerm string,
) (int, int, error) {
	isDeepScan := strings.EqualFold(scanDepth, "deep")

	// 查询已存在的表
	existingTableMap := s.repo.GetItemsByNodeAndTypeMap(tenantID, engineID, schemaNode.ID, itemTerm)

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

	s.log.Info("扫描到的表",
		"schema", schemaName,
		"tables_count", len(pluginTables),
	)

	totalTables := 0
	totalFields := 0
	scannedTables := make(map[string]bool)

	// 处理每张表
	for i, tableInfo := range pluginTables {
		s.log.Info(fmt.Sprintf("处理第 %d/%d 张表", i+1, len(pluginTables)),
			"table_name", tableInfo.Name,
			"row_count", tableInfo.RowCount,
		)

		scannedTables[tableInfo.Name] = true
		tableInfo.RowCount = s.resolveTableRowCount(ctx, resource, schemaName, tableInfo)

		// 检查是否需要更新
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
		fullName := metapath.ComposeNodeFullName(tableInfo.Name, schemaNode, ".")
		rowCount := derefInt64Ptr(tableInfo.RowCount)
		sizeBytes := derefInt64Ptr(tableInfo.SizeBytes)

		item, err := s.repo.UpsertItemWithDepth(tenantID, engineID, schemaNode, itemTerm, tableInfo.Name, fullName, attrs, &rowCount, &sizeBytes, tableInfo.UpdatedAt, scanDepth)
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
) ([]datatype.TableInfo, error) {
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
	tables := make([]datatype.TableInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsItem {
			continue
		}
		rowCount := int64StatPtr(node.Stats, "row_count")
		sizeBytes := int64StatPtr(node.Stats, "size_bytes")
		tables = append(tables, datatype.TableInfo{
			Name:      node.Name,
			Kind:      node.Kind,
			Comment:   stringAttr(node.Attributes, "comment"),
			RowCount:  rowCount,
			SizeBytes: sizeBytes,
			UpdatedAt: timeAttr(node.Attributes, "updated_at"),
			Native:    mapAttr(node.Attributes, "native"),
		})
	}
	return tables, nil
}

func (s *DatabaseScanService) resolveTableRowCount(ctx context.Context, resource *commonModels.Engine, schemaName string, tableInfo datatype.TableInfo) *int64 {
	if tableInfo.RowCount != nil && *tableInfo.RowCount > 0 {
		return tableInfo.RowCount
	}
	count, err := plugin.CountItemRows(ctx, &plugin.Engine{
		ID:             resource.ID,
		EngineType:     resource.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
	}, schemaName, tableInfo.Name)
	if err != nil {
		s.log.Debug("表行数精确查询失败，保留 catalog 统计值",
			"engine_id", resource.ID,
			"schema", schemaName,
			"table", tableInfo.Name,
			"error", err,
		)
		return tableInfo.RowCount
	}
	return &count
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
	tableInfo datatype.TableInfo,
	existingItem *models.MetaItem,
	isDeepScan bool,
) ([]datatype.FieldInfo, models.JSONMap, error) {
	var fields []datatype.FieldInfo
	var attrs models.JSONMap

	if isDeepScan {
		pluginColumns, err := s.listColumns(ctx, resource, metadataProvider, schemaName, tableInfo.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("字段扫描失败: %w", err)
		}

		fields = databaseDatatypeFieldInfo(pluginColumns)

		s.log.Info("字段扫描成功",
			"table", tableInfo.Name,
			"field_count", len(fields),
		)
		// 提取主键信息
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

		// 扫描空间元数据由引擎能力声明控制，具体实现通过连接池增强元数据。
		if engineSupportsSpatialMetadata(resource.EngineType) && db != nil {
			spatialMeta := s.scanSpatialMetadata(ctx, db, schemaName, tableInfo.Name)
			if spatialMeta != nil {
				metaattr.UpsertNested(attrs, "capabilities", "spatial", metaattr.SpatialInfoAttributes(spatialInfoFromMetadata(spatialMeta)))
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

func int64StatPtr(stats map[string]interface{}, key string) *int64 {
	value, ok := int64Stat(stats, key)
	if !ok {
		return nil
	}
	return &value
}

func derefInt64Ptr(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func stringAttr(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	if value, ok := attrs[key].(string); ok {
		return value
	}
	return ""
}

func timeAttr(attrs map[string]interface{}, key string) *time.Time {
	if attrs == nil {
		return nil
	}
	switch value := attrs[key].(type) {
	case *time.Time:
		return value
	case time.Time:
		return &value
	default:
		return nil
	}
}

func mapAttr(attrs map[string]interface{}, key string) map[string]interface{} {
	if attrs == nil {
		return nil
	}
	values, ok := attrs[key].(map[string]interface{})
	if !ok || len(values) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(values))
	for k, v := range values {
		cloned[k] = v
	}
	return cloned
}

func engineSupportsSpatialMetadata(engineType string) bool {
	p, err := plugin.Get(engineType)
	if err != nil {
		return false
	}
	capabilities := p.Capabilities()
	return capabilities.Storage != nil &&
		capabilities.Storage.Metadata != nil &&
		capabilities.Storage.Metadata.SpatialMetadata
}

func databaseDatatypeFieldInfo(input []datatype.FieldInfo) []datatype.FieldInfo {
	fields := make([]datatype.FieldInfo, 0, len(input))
	for _, field := range input {
		nativeType := field.NativeType
		if nativeType == "" {
			nativeType = string(field.Type)
		}
		field.NativeType = nativeType
		field.Type = datatype.FieldType(metaquery.StandardizeFieldType(nativeType, string(field.Type)))
		fields = append(fields, field)
	}
	return fields
}

func normalizedTableKind(table datatype.TableInfo) string {
	if table.Kind != "" {
		return table.Kind
	}
	return "table"
}

func (s *DatabaseScanService) listColumns(
	ctx context.Context,
	resource *commonModels.Engine,
	metadataProvider plugin.ItemMetadataProvider,
	schemaName string,
	tableName string,
) ([]datatype.FieldInfo, error) {
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
	return item.Fields, nil
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

func spatialInfoFromMetadata(spatialMeta *models.SpatialMetadata) *datatype.SpatialInfo {
	if spatialMeta == nil || spatialMeta.GeometryColumn == "" {
		return nil
	}
	info := &datatype.SpatialInfo{
		GeometryColumns: []datatype.GeometryColumnInfo{{
			Name:         spatialMeta.GeometryColumn,
			GeometryType: metaattr.NormalizeGeometryType(firstString(spatialMeta.GeometryTypes)),
		}},
		PrimaryGeometryColumn: spatialMeta.GeometryColumn,
	}
	if spatialMeta.SRID > 0 {
		srid := spatialMeta.SRID
		info.GeometryColumns[0].SRID = &srid
	}
	if len(spatialMeta.Extent) == 4 {
		extent := datatype.NewBoundingBox(spatialMeta.Extent[0], spatialMeta.Extent[1], spatialMeta.Extent[2], spatialMeta.Extent[3])
		info.Extent = &extent
	}
	hasSpatialIndex := spatialMeta.HasSpatialIndex
	info.HasSpatialIndex = &hasSpatialIndex
	info.IndexName = spatialMeta.IndexName
	return info
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
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
