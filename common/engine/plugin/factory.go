package plugin

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// TestConnection 统一入口：测试数据库连接
// 自动查找对应类型的插件并调用其 TestConnection 方法
func TestConnection(ctx context.Context, engine *Engine) error {
	if engine == nil {
		return fmt.Errorf("engine cannot be nil")
	}

	plugin, err := Get(engine.EngineType)
	if err != nil {
		return err
	}

	return plugin.TestConnection(ctx, engine.ConnectionInfo)
}

// BuildConnectionString 统一入口：构建连接字符串
// 自动查找对应类型的插件并调用其 BuildConnectionString 方法
func BuildConnectionString(engine *Engine) (string, error) {
	if engine == nil {
		return "", fmt.Errorf("engine cannot be nil")
	}

	plugin, err := Get(engine.EngineType)
	if err != nil {
		return "", err
	}

	return plugin.BuildConnectionString(engine.ConnectionInfo)
}

// ValidateConnectionInfo 统一入口：验证连接信息
// 自动查找对应类型的插件并调用其 ValidateConnectionInfo 方法
func ValidateConnectionInfo(engineType string, connInfo ConnectionInfo) error {
	plugin, err := Get(engineType)
	if err != nil {
		return err
	}

	return plugin.ValidateConnectionInfo(connInfo)
}

// GenerateCapabilities 统一入口：生成结构化引擎能力声明 JSON
// 自动查找对应类型的插件并序列化其 Capabilities 结构
func GenerateCapabilities(engineType string) (string, error) {
	plugin, err := Get(engineType)
	if err != nil {
		return "", err
	}

	return MarshalEngineCapabilities(plugin.Capabilities())
}

// GetRequiredFields 获取指定类型的必填字段列表
func GetRequiredFields(engineType string) ([]string, error) {
	plugin, err := Get(engineType)
	if err != nil {
		return nil, err
	}

	return plugin.RequiredFields(), nil
}

// GetSensitiveFields 获取指定类型的敏感字段列表
func GetSensitiveFields(engineType string) ([]string, error) {
	plugin, err := Get(engineType)
	if err != nil {
		return nil, err
	}

	return plugin.SensitiveFields(), nil
}

// GetDefaultPort 获取指定类型的默认端口
func GetDefaultPort(engineType string) (int, error) {
	plugin, err := Get(engineType)
	if err != nil {
		return 0, err
	}

	return plugin.DefaultPort(), nil
}

// GetDisplayName 获取指定类型的显示名称
func GetDisplayName(engineType string) (string, error) {
	plugin, err := Get(engineType)
	if err != nil {
		return "", err
	}

	return plugin.DisplayName(), nil
}

// GetEngineCategory 获取指定类型的引擎分类
func GetEngineCategory(engineType string) (string, error) {
	plugin, err := Get(engineType)
	if err != nil {
		return "", err
	}

	return plugin.EngineCategory(), nil
}

// === 连接池管理相关方法 ===

// CreateConnectionPoolDirect 直接创建连接池（不缓存）
// 供 PoolManager 内部使用，一般用户应使用 GetOrCreatePoolFromFactory
func CreateConnectionPoolDirect(engine *Engine, config *PoolConfig) (*gorm.DB, error) {
	if engine == nil {
		return nil, fmt.Errorf("engine cannot be nil")
	}
	if config == nil {
		config = DefaultPoolConfig()
	}

	plugin, err := Get(engine.EngineType)
	if err != nil {
		return nil, err
	}

	// 类型断言：检查是否支持连接池
	poolPlugin, ok := plugin.(ConnectionPoolPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not support connection pool", engine.EngineType)
	}

	return poolPlugin.CreateConnectionPool(engine.ConnectionInfo, config)
}

// GetOrCreatePoolFromFactory 获取或创建连接池（推荐使用）
// 会自动管理连接池的生命周期，复用已有连接池
func GetOrCreatePoolFromFactory(engine *Engine, config *PoolConfig) (*gorm.DB, error) {
	if engine == nil {
		return nil, fmt.Errorf("engine cannot be nil")
	}
	if config == nil {
		config = DefaultPoolConfig()
	}

	result, err := GetOrCreatePool(engine, config)
	if err != nil {
		return nil, err
	}

	// 类型断言为 *gorm.DB
	db, ok := result.(*gorm.DB)
	if !ok {
		return nil, fmt.Errorf("unexpected connection pool type")
	}

	return db, nil
}

// === 元数据查询相关方法 ===

// ListSchemas 列出所有第一层命名空间（Schema/Database）。
// db 参数保留用于旧调用方兼容；实际查询统一走 CatalogProvider。
func ListSchemas(ctx context.Context, resource *Engine, db *gorm.DB) ([]SchemaInfo, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource cannot be nil")
	}

	enginePlugin, err := Get(resource.EngineType)
	if err != nil {
		return nil, err
	}

	catalogProvider, ok := enginePlugin.(CatalogProvider)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not implement CatalogProvider", resource.EngineType)
	}

	nodes, err := catalogProvider.ListChildren(ctx, resource.ConnectionInfo, CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: resource.ID,
	}, ListOptions{})
	if err != nil {
		return nil, err
	}

	schemas := make([]SchemaInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsContainer {
			continue
		}
		tableCount := 0
		if count, ok := int64Stat(node.Stats, "table_count"); ok {
			tableCount = int(count)
		}
		schemas = append(schemas, SchemaInfo{
			Name:       node.Name,
			TableCount: tableCount,
		})
	}
	return schemas, nil
}

// ListTables 列出指定命名空间下的表。
// db 参数保留用于旧调用方兼容；实际查询统一走 CatalogProvider。
func ListTables(ctx context.Context, resource *Engine, db *gorm.DB, schema string) ([]TableInfo, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource cannot be nil")
	}

	enginePlugin, err := Get(resource.EngineType)
	if err != nil {
		return nil, err
	}

	catalogProvider, ok := enginePlugin.(CatalogProvider)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not implement CatalogProvider", resource.EngineType)
	}

	nodes, err := catalogProvider.ListChildren(ctx, resource.ConnectionInfo, CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: resource.ID,
		Segments: []CatalogSegment{{
			Term: namespaceTermForPlugin(enginePlugin),
			Kind: CatalogKindNamespace,
			Name: schema,
		}},
	}, ListOptions{})
	if err != nil {
		return nil, err
	}

	tables := make([]TableInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsItem {
			continue
		}
		rowCount, _ := int64Stat(node.Stats, "row_count")
		sizeBytes, _ := int64Stat(node.Stats, "size_bytes")
		tables = append(tables, TableInfo{
			Schema:    schema,
			TableName: node.Name,
			RowCount:  rowCount,
			SizeBytes: sizeBytes,
		})
	}
	return tables, nil
}

// ListColumns 列出指定表的所有列。
// db 参数保留用于旧调用方兼容；实际查询统一走 ItemMetadataProvider。
func ListColumns(ctx context.Context, resource *Engine, db *gorm.DB, schema, table string) ([]ColumnInfo, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource cannot be nil")
	}

	enginePlugin, err := Get(resource.EngineType)
	if err != nil {
		return nil, err
	}

	metadataProvider, ok := enginePlugin.(ItemMetadataProvider)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not implement ItemMetadataProvider", resource.EngineType)
	}

	item, err := metadataProvider.DescribeItem(ctx, resource.ConnectionInfo, CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: resource.ID,
		Segments: []CatalogSegment{
			{Term: namespaceTermForPlugin(enginePlugin), Kind: CatalogKindNamespace, Name: schema},
			{Term: CatalogTermTable, Kind: CatalogKindTable, Name: table},
		},
	}, MetadataOptions{})
	if err != nil {
		return nil, err
	}

	columns := make([]ColumnInfo, 0, len(item.Fields))
	for _, field := range item.Fields {
		dataType := field.NativeType
		if dataType == "" {
			dataType = field.Type
		}
		columns = append(columns, ColumnInfo{
			ColumnName:   field.Name,
			DataType:     dataType,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		})
	}
	return columns, nil
}

// GetTableRowCount 获取表的行数（供Meta模块使用）
func GetTableRowCount(ctx context.Context, resource *Engine, db *gorm.DB, schema, table string) (int64, error) {
	if resource == nil {
		return 0, fmt.Errorf("resource cannot be nil")
	}
	if db == nil {
		return 0, fmt.Errorf("database connection cannot be nil")
	}

	plugin, err := Get(resource.EngineType)
	if err != nil {
		return 0, err
	}

	metaPlugin, ok := plugin.(RelationalDBPlugin)
	if !ok {
		return 0, fmt.Errorf("plugin %s does not support metadata query", resource.EngineType)
	}

	return metaPlugin.GetTableRowCount(ctx, db, schema, table)
}

// === 辅助方法 ===

// SupportsConnectionPool 检查指定类型是否支持连接池
func SupportsConnectionPool(engineType string) bool {
	plugin, err := Get(engineType)
	if err != nil {
		return false
	}

	_, ok := plugin.(ConnectionPoolPlugin)
	return ok
}

// SupportsMetadataQuery 检查指定类型是否支持元数据查询
func SupportsMetadataQuery(engineType string) bool {
	plugin, err := Get(engineType)
	if err != nil {
		return false
	}

	capabilities := plugin.Capabilities()
	return capabilities.Storage != nil &&
		capabilities.Storage.Metadata != nil &&
		capabilities.Storage.Metadata.Supported
}

func namespaceTermForPlugin(p EnginePlugin) string {
	if relPlugin, ok := p.(RelationalDBPlugin); ok {
		return relPlugin.SchemaNodeType()
	}
	if modelProvider, ok := p.(CatalogModelProvider); ok {
		model := modelProvider.CatalogModel()
		if len(model.Levels) > 0 && model.Levels[0].Term != "" {
			return model.Levels[0].Term
		}
	}
	return CatalogTermDatabase
}

func int64Stat(stats map[string]interface{}, key string) (int64, bool) {
	if stats == nil {
		return 0, false
	}
	switch v := stats[key].(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}
