package dbbridge

import (
	"context"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"gorm.io/gorm"

	// 导入所有数据库插件，触发 init() 注册
	_ "github.com/addp/common/engine/plugins/clickhouse"
	_ "github.com/addp/common/engine/plugins/doris"
	_ "github.com/addp/common/engine/plugins/math_workflow"
	_ "github.com/addp/common/engine/plugins/minio"
	_ "github.com/addp/common/engine/plugins/mongodb"
	_ "github.com/addp/common/engine/plugins/mysql"
	_ "github.com/addp/common/engine/plugins/postgresql"
	_ "github.com/addp/common/engine/plugins/python_workflow"
	_ "github.com/addp/common/engine/plugins/s3"
	_ "github.com/addp/common/engine/plugins/spark_sql"
	_ "github.com/addp/common/engine/plugins/spark_workflow"
)

// BuildConnectionString 使用插件系统构建连接字符串
func BuildConnectionString(engine *models.Engine) (string, error) {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:   engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}

	return plugin.BuildConnectionString(pluginEngine)
}

// TestConnection 使用插件系统测试连接
func TestConnection(ctx context.Context, engine *models.Engine) error {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:   engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}

	return plugin.TestConnection(ctx, pluginEngine)
}

// GenerateCapabilities 使用插件系统生成能力描述
func GenerateCapabilities(engineType string) (string, error) {
	return plugin.GenerateCapabilities(engineType)
}

// GetSensitiveFields 获取敏感字段列表
func GetSensitiveFields(engineType string) ([]string, error) {
	return plugin.GetSensitiveFields(engineType)
}

// GetRequiredFields 获取必填字段列表
func GetRequiredFields(engineType string) ([]string, error) {
	return plugin.GetRequiredFields(engineType)
}

// GetDefaultPort 获取默认端口
func GetDefaultPort(engineType string) (int, error) {
	return plugin.GetDefaultPort(engineType)
}

// ListAllTypes 列出所有已注册的数据库类型
func ListAllTypes() []string {
	return plugin.List()
}

// GetAllPlugins 获取所有插件信息（用于前端API）
func GetAllPlugins() map[string]PluginInfo {
	plugins := plugin.GetAll()
	result := make(map[string]PluginInfo)

	for dbType, p := range plugins {
		result[dbType] = PluginInfo{
			Type:             p.Type(),
			DisplayName:      p.DisplayName(),
			Category:         p.EngineCategory(),
			DefaultPort:      p.DefaultPort(),
			RequiredFields:   p.RequiredFields(),
			SensitiveFields:  p.SensitiveFields(),
		}
	}

	return result
}

// PluginInfo 插件信息（用于API响应）
type PluginInfo struct {
	Type            string   `json:"type"`
	DisplayName     string   `json:"display_name"`
	Category        string   `json:"category"`
	DefaultPort     int      `json:"default_port"`
	RequiredFields  []string `json:"required_fields"`
	SensitiveFields []string `json:"sensitive_fields"`
}

// === 连接池管理方法（供Develop模块使用）===

// GetOrCreatePool 获取或创建连接池
// 这是推荐的获取连接池的方式，会自动管理连接池的生命周期
func GetOrCreatePool(engine *models.Engine, config *plugin.PoolConfig) (*gorm.DB, error) {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:   engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}
	return plugin.GetOrCreatePoolFromFactory(pluginEngine, config)
}

// DefaultPoolConfig 返回默认连接池配置
func DefaultPoolConfig() *plugin.PoolConfig {
	return plugin.DefaultPoolConfig()
}

// ClosePool 关闭指定引擎的连接池
// 通常在引擎被删除或更新时调用
func ClosePool(engineID uint) error {
	return plugin.ClosePool(engineID)
}

// CloseAllPools 关闭所有连接池
// 在应用关闭时调用，确保优雅关闭
func CloseAllPools() {
	plugin.CloseAllPools()
}

// GetPoolStats 获取所有连接池的统计信息
func GetPoolStats() map[uint]plugin.PoolStats {
	return plugin.GetPoolStats()
}

// === 元数据查询方法（供Meta模块使用）===

// ListSchemas 列出所有Schema/Database
func ListSchemas(ctx context.Context, engine *models.Engine, db *gorm.DB) ([]plugin.SchemaInfo, error) {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:   engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}
	return plugin.ListSchemas(ctx, pluginEngine, db)
}

// ListTables 列出指定Schema下的所有表
func ListTables(ctx context.Context, engine *models.Engine, db *gorm.DB, schema string) ([]plugin.TableInfo, error) {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:   engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}
	return plugin.ListTables(ctx, pluginEngine, db, schema)
}

// ListColumns 列出指定表的所有列
func ListColumns(ctx context.Context, engine *models.Engine, db *gorm.DB, schema, table string) ([]plugin.ColumnInfo, error) {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:   engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}
	return plugin.ListColumns(ctx, pluginEngine, db, schema, table)
}

// GetTableRowCount 获取表的行数
func GetTableRowCount(ctx context.Context, engine *models.Engine, db *gorm.DB, schema, table string) (int64, error) {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:   engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}
	return plugin.GetTableRowCount(ctx, pluginEngine, db, schema, table)
}

// === 辅助方法 ===

// SupportsConnectionPool 检查指定类型是否支持连接池
func SupportsConnectionPool(engineType string) bool {
	return plugin.SupportsConnectionPool(engineType)
}

// SupportsMetadataQuery 检查指定类型是否支持元数据查询
func SupportsMetadataQuery(engineType string) bool {
	return plugin.SupportsMetadataQuery(engineType)
}
