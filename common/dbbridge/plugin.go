package dbbridge

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"gorm.io/gorm"

	// 导入所有内置引擎插件，触发 init() 注册
	_ "github.com/addp/common/engine/plugins/builtin/all"
)

// BuildDSN 使用插件系统构建连接字符串
func BuildDSN(engine *models.Engine) (string, error) {
	return plugin.BuildDSN(toPluginEngine(engine))
}

// TestConnection 使用插件系统测试连接
func TestConnection(ctx context.Context, engine *models.Engine) error {
	if engine == nil {
		return fmt.Errorf("engine cannot be nil")
	}
	if _, err := plugin.Get(engine.EngineType); err == nil {
		return plugin.TestConnection(ctx, toPluginEngine(engine))
	}
	if supportsADDPWorkflowRuntime(engine) {
		workflowProvider, err := WorkflowRuntimeProviderForEngine(engine)
		if err != nil {
			return err
		}
		return workflowProvider.TestConnection(ctx, plugin.ConnectionInfo(engine.ConnectionInfo))
	}
	return plugin.TestConnection(ctx, toPluginEngine(engine))
}

// ProbeWorkflowRuntimeContract validates the addp.workflow/v1 control-plane
// contract exposed by a workflow runtime before it is saved or used.
func ProbeWorkflowRuntimeContract(ctx context.Context, engine *models.Engine) (int, error) {
	workflowProvider, err := WorkflowRuntimeProviderForEngine(engine)
	if err != nil {
		return 0, err
	}
	if err := workflowProvider.TestConnection(ctx, plugin.ConnectionInfo(engine.ConnectionInfo)); err != nil {
		return 0, err
	}
	operators, err := ListWorkflowOperators(ctx, engine)
	if err != nil {
		return 0, err
	}
	return len(operators), nil
}

// GenerateCapabilities 使用插件系统生成结构化能力声明 JSON
func GenerateCapabilities(engineType string) (string, error) {
	return plugin.GenerateCapabilities(engineType)
}

// GenerateResolvedCapabilities 使用插件系统生成具体引擎实例的结构化能力声明 JSON。
func GenerateResolvedCapabilities(ctx context.Context, engine *models.Engine) (string, error) {
	if engine == nil {
		return "", fmt.Errorf("engine cannot be nil")
	}
	return plugin.GenerateResolvedCapabilities(ctx, toPluginEngine(engine))
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

// CatalogModel 获取引擎插件声明的 catalog model。
func CatalogModel(engineType string) (plugin.CatalogModelSpec, error) {
	p, err := plugin.Get(engineType)
	if err != nil {
		return plugin.CatalogModelSpec{}, err
	}
	modelProvider, ok := p.(plugin.CatalogModelProvider)
	if !ok {
		return plugin.CatalogModelSpec{}, fmt.Errorf("plugin %s does not implement CatalogModelProvider", engineType)
	}
	return modelProvider.CatalogModel(), nil
}

// GetAllPlugins 获取所有插件信息（用于前端API）
func GetAllPlugins() map[string]PluginInfo {
	plugins := plugin.GetAll()
	result := make(map[string]PluginInfo)

	for dbType, p := range plugins {
		result[dbType] = PluginInfo{
			Type:            p.Type(),
			DisplayName:     p.DisplayName(),
			Origin:          p.EngineOrigin(),
			DefaultPort:     p.DefaultPort(),
			RequiredFields:  p.RequiredFields(),
			SensitiveFields: p.SensitiveFields(),
		}
	}

	return result
}

// PluginInfo 插件信息（用于API响应）
type PluginInfo struct {
	Type            string   `json:"type"`
	DisplayName     string   `json:"display_name"`
	Origin          string   `json:"origin"`
	DefaultPort     int      `json:"default_port"`
	RequiredFields  []string `json:"required_fields"`
	SensitiveFields []string `json:"sensitive_fields"`
}

// === 连接池管理方法（供Develop模块使用）===

// GetOrCreatePool 获取或创建连接池
// 这是推荐的获取连接池的方式，会自动管理连接池的生命周期
func GetOrCreatePool(engine *models.Engine, config *plugin.PoolConfig) (*gorm.DB, error) {
	return plugin.GetOrCreatePoolFromFactory(toPluginEngine(engine), config)
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

// === Catalog / facts 查询方法 ===

func toPluginEngine(engine *models.Engine) *plugin.Engine {
	pluginEngine := &plugin.Engine{
		ID:             engine.ID,
		EngineType:     engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}
	return pluginEngine
}
