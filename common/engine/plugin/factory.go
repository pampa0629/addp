package plugin

import (
	"context"
	"fmt"
	"strings"

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

// === Catalog / metadata 查询相关方法 ===

// ListNamespaces 列出引擎 catalog 的第一层命名空间。
func ListNamespaces(ctx context.Context, resource *Engine) ([]CatalogNode, error) {
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

	return catalogProvider.ListChildren(ctx, resource.ConnectionInfo, CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: resource.ID,
	}, ListOptions{})
}

// ListItems 列出指定命名空间下的叶子数据项。
func ListItems(ctx context.Context, resource *Engine, namespace string) ([]CatalogNode, error) {
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

	return catalogProvider.ListChildren(ctx, resource.ConnectionInfo, CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: resource.ID,
		Segments: []CatalogSegment{{
			Term: namespaceTermForPlugin(enginePlugin),
			Kind: CatalogKindNamespace,
			Name: namespace,
		}},
	}, ListOptions{})
}

// DescribeItem 描述 catalog 叶子数据项。
func DescribeItem(ctx context.Context, resource *Engine, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
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

	if path.Version == "" {
		path.Version = CatalogPathVersion
	}
	if path.EngineID == 0 {
		path.EngineID = resource.ID
	}

	return metadataProvider.DescribeItem(ctx, resource.ConnectionInfo, path, opts)
}

// DescribeNamedItem 描述指定命名空间下的具名 tabular 数据项。
func DescribeNamedItem(ctx context.Context, resource *Engine, namespace, item string, opts MetadataOptions) (*ItemMetadata, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource cannot be nil")
	}

	enginePlugin, err := Get(resource.EngineType)
	if err != nil {
		return nil, err
	}

	return DescribeItem(ctx, resource, CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: resource.ID,
		Segments: []CatalogSegment{
			{Term: namespaceTermForPlugin(enginePlugin), Kind: CatalogKindNamespace, Name: namespace},
			{Term: CatalogTermTable, Kind: CatalogKindTable, Name: item},
		},
	}, opts)
}

// CountItemRows 获取 tabular 数据项行数。
func CountItemRows(ctx context.Context, resource *Engine, namespace, item string) (int64, error) {
	if resource == nil {
		return 0, fmt.Errorf("resource cannot be nil")
	}

	enginePlugin, err := Get(resource.EngineType)
	if err != nil {
		return 0, err
	}

	if _, ok := enginePlugin.(ItemMetadataProvider); ok {
		metadata, err := DescribeNamedItem(ctx, resource, namespace, item, MetadataOptions{IncludeStatistics: true})
		if err == nil && metadata != nil {
			if rowCount, ok := int64Stat(metadata.Stats, "row_count"); ok {
				return rowCount, nil
			}
		}
	}

	sqlRuntime, ok := enginePlugin.(SQLQueryRuntimeProvider)
	if !ok {
		return 0, fmt.Errorf("plugin %s does not implement SQLQueryRuntimeProvider", resource.EngineType)
	}

	result, err := sqlRuntime.ExecuteSQL(ctx, resource.ConnectionInfo, countSQLForEngine(resource.EngineType, namespace, item), QueryOptions{
		EngineID:   resource.ID,
		EngineType: resource.EngineType,
		ReadOnly:   true,
		Limit:      1,
	})
	if err != nil {
		return 0, err
	}
	if len(result.Rows) == 0 {
		return 0, nil
	}
	for _, value := range result.Rows[0] {
		if rowCount, ok := int64Value(value); ok {
			return rowCount, nil
		}
	}

	return 0, fmt.Errorf("row count query returned non-numeric result")
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
	if modelProvider, ok := p.(CatalogModelProvider); ok {
		model := modelProvider.CatalogModel()
		if len(model.Levels) > 0 && model.Levels[0].Term != "" {
			return model.Levels[0].Term
		}
	}
	return CatalogTermDatabase
}

func countSQLForEngine(engineType, schema, table string) string {
	switch strings.ToLower(engineType) {
	case "mysql", "doris", "clickhouse", "spark_sql", "spark":
		return fmt.Sprintf("SELECT COUNT(*) AS total FROM `%s`.`%s`", escapeBacktickIdentifier(schema), escapeBacktickIdentifier(table))
	default:
		return fmt.Sprintf("SELECT COUNT(*) AS total FROM \"%s\".\"%s\"", escapeDoubleQuoteIdentifier(schema), escapeDoubleQuoteIdentifier(table))
	}
}

func escapeBacktickIdentifier(identifier string) string {
	return strings.ReplaceAll(identifier, "`", "``")
}

func escapeDoubleQuoteIdentifier(identifier string) string {
	return strings.ReplaceAll(identifier, `"`, `""`)
}

func int64Stat(stats map[string]interface{}, key string) (int64, bool) {
	if stats == nil {
		return 0, false
	}
	return int64Value(stats[key])
}

func int64Value(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int16:
		return int64(v), true
	case int8:
		return int64(v), true
	case uint:
		return int64(v), true
	case uint64:
		return int64(v), true
	case uint32:
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	case []byte:
		var parsed int64
		if _, err := fmt.Sscan(string(v), &parsed); err == nil {
			return parsed, true
		}
	case string:
		var parsed int64
		if _, err := fmt.Sscan(v, &parsed); err == nil {
			return parsed, true
		}
	default:
		return 0, false
	}
	return 0, false
}
