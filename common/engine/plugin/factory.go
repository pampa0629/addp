package plugin

import (
	"context"
	"fmt"

	commonquery "github.com/addp/common/query"
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

// BuildDSN 统一入口：为需要 driver DSN 的引擎构建 DSN。
// DSN 是可选 provider 能力，不是所有引擎的基础接口能力。
func BuildDSN(engine *Engine) (string, error) {
	if engine == nil {
		return "", fmt.Errorf("engine cannot be nil")
	}

	enginePlugin, err := Get(engine.EngineType)
	if err != nil {
		return "", err
	}

	dsnProvider, ok := enginePlugin.(DSNProvider)
	if !ok {
		return "", fmt.Errorf("engine type %s does not support DSN", engine.EngineType)
	}

	return dsnProvider.BuildDSN(engine.ConnectionInfo)
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

// GenerateResolvedCapabilities 统一入口：基于插件静态能力模板和具体连接信息生成实例能力声明 JSON。
func GenerateResolvedCapabilities(ctx context.Context, engine *Engine) (string, error) {
	if engine == nil {
		return "", fmt.Errorf("engine cannot be nil")
	}
	enginePlugin, err := Get(engine.EngineType)
	if err != nil {
		return "", err
	}

	capabilities := enginePlugin.Capabilities()
	if resolver, ok := enginePlugin.(InstanceCapabilitiesResolver); ok {
		capabilities, err = resolver.ResolveCapabilities(ctx, engine.ConnectionInfo, capabilities)
		if err != nil {
			return "", err
		}
	}
	return MarshalEngineCapabilities(capabilities)
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

// === Catalog facts 查询相关方法 ===

// DescribeEngineCatalogFacts 描述 catalog leaf 的 engine-native facts。
func DescribeEngineCatalogFacts(ctx context.Context, resource *Engine, path EngineCatalogPath, opts EngineCatalogFactsOptions) (*EngineCatalogFacts, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource cannot be nil")
	}

	enginePlugin, err := Get(resource.EngineType)
	if err != nil {
		return nil, err
	}

	factsProvider, ok := enginePlugin.(EngineCatalogFactsProvider)
	if !ok {
		return nil, fmt.Errorf("plugin %s does not implement EngineCatalogFactsProvider", resource.EngineType)
	}

	if path.Version == "" {
		path.Version = EngineCatalogPathVersion
	}
	if path.EngineID == 0 {
		path.EngineID = resource.ID
	}

	return factsProvider.DescribeEngineCatalogFacts(ctx, resource.ConnectionInfo, path, opts)
}

// CountEngineCatalogItemRows 获取 tabular catalog leaf 的行数。
func CountEngineCatalogItemRows(ctx context.Context, resource *Engine, path EngineCatalogPath) (int64, error) {
	if resource == nil {
		return 0, fmt.Errorf("resource cannot be nil")
	}

	enginePlugin, err := Get(resource.EngineType)
	if err != nil {
		return 0, err
	}

	if _, ok := enginePlugin.(EngineCatalogFactsProvider); ok {
		facts, err := DescribeEngineCatalogFacts(ctx, resource, path, EngineCatalogFactsOptions{IncludeStatistics: true})
		if err == nil && facts != nil {
			if tableInfo := EngineCatalogFactsTableInfo(facts); tableInfo != nil && tableInfo.RowCount != nil && *tableInfo.RowCount >= 0 {
				return *tableInfo.RowCount, nil
			}
		}
	}

	sqlRuntime, ok := enginePlugin.(SQLQueryRuntimeProvider)
	if !ok {
		return 0, fmt.Errorf("plugin %s does not implement SQLQueryRuntimeProvider", resource.EngineType)
	}

	segments := EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) < 2 {
		return 0, fmt.Errorf("catalog row count path requires namespace and item segments")
	}
	namespace := segments[0].Name
	item := segments[len(segments)-1].Name
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

func countSQLForEngine(engineType, schema, table string) string {
	return commonquery.ForEngine(engineType).CountTableSQL(schema, table, "")
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
