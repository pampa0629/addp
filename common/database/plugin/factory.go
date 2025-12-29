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

// GenerateCapabilities 统一入口：生成引擎能力描述
// 自动查找对应类型的插件并调用其 GenerateCapabilities 方法
func GenerateCapabilities(engineType string) (string, error) {
	plugin, err := Get(engineType)
	if err != nil {
		return "", err
	}

	return plugin.GenerateCapabilities(), nil
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

// GetConnectionCategory 获取指定类型的连接类别
func GetConnectionCategory(engineType string) (string, error) {
	plugin, err := Get(engineType)
	if err != nil {
		return "", err
	}

	return plugin.ConnectionCategory(), nil
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

// ListSchemas 列出所有Schema（供Meta模块使用）
func ListSchemas(ctx context.Context, resource *Engine, db *gorm.DB) ([]SchemaInfo, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource cannot be nil")
	}
	if db == nil {
		return nil, fmt.Errorf("database connection cannot be nil")
	}

	plugin, err := Get(resource.EngineType)
	if err != nil {
		return nil, err
	}

	metaPlugin, ok := plugin.(MetadataPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not support metadata query", resource.EngineType)
	}

	return metaPlugin.ListSchemas(ctx, db)
}

// ListTables 列出指定Schema下的所有表（供Meta模块使用）
func ListTables(ctx context.Context, resource *Engine, db *gorm.DB, schema string) ([]TableInfo, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource cannot be nil")
	}
	if db == nil {
		return nil, fmt.Errorf("database connection cannot be nil")
	}

	plugin, err := Get(resource.EngineType)
	if err != nil {
		return nil, err
	}

	metaPlugin, ok := plugin.(MetadataPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not support metadata query", resource.EngineType)
	}

	return metaPlugin.ListTables(ctx, db, schema)
}

// ListColumns 列出指定表的所有列（供Meta模块使用）
func ListColumns(ctx context.Context, resource *Engine, db *gorm.DB, schema, table string) ([]ColumnInfo, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource cannot be nil")
	}
	if db == nil {
		return nil, fmt.Errorf("database connection cannot be nil")
	}

	plugin, err := Get(resource.EngineType)
	if err != nil {
		return nil, err
	}

	metaPlugin, ok := plugin.(MetadataPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not support metadata query", resource.EngineType)
	}

	return metaPlugin.ListColumns(ctx, db, schema, table)
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

	metaPlugin, ok := plugin.(MetadataPlugin)
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

	_, ok := plugin.(MetadataPlugin)
	return ok
}
