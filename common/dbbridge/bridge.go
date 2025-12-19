package dbbridge

import (
	"context"

	"github.com/addp/common/database/plugin"
	"github.com/addp/common/models"

	// 导入所有数据库插件，触发 init() 注册
	_ "github.com/addp/common/database/plugins/doris"
	_ "github.com/addp/common/database/plugins/minio"
	_ "github.com/addp/common/database/plugins/mysql"
	_ "github.com/addp/common/database/plugins/postgresql"
	_ "github.com/addp/common/database/plugins/s3"
	_ "github.com/addp/common/database/plugins/spark_sql"
)

// BuildConnectionString 使用插件系统构建连接字符串
func BuildConnectionString(resource *models.Resource) (string, error) {
	pluginResource := &plugin.Resource{
		ID:             resource.ID,
		ResourceType:   resource.ResourceType,
		ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
	}

	return plugin.BuildConnectionString(pluginResource)
}

// TestConnection 使用插件系统测试连接
func TestConnection(ctx context.Context, resource *models.Resource) error {
	pluginResource := &plugin.Resource{
		ID:             resource.ID,
		ResourceType:   resource.ResourceType,
		ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
	}

	return plugin.TestConnection(ctx, pluginResource)
}

// GenerateCapabilities 使用插件系统生成能力描述
func GenerateCapabilities(resourceType string) (string, error) {
	return plugin.GenerateCapabilities(resourceType)
}

// GetSensitiveFields 获取敏感字段列表
func GetSensitiveFields(resourceType string) ([]string, error) {
	return plugin.GetSensitiveFields(resourceType)
}

// GetRequiredFields 获取必填字段列表
func GetRequiredFields(resourceType string) ([]string, error) {
	return plugin.GetRequiredFields(resourceType)
}

// GetDefaultPort 获取默认端口
func GetDefaultPort(resourceType string) (int, error) {
	return plugin.GetDefaultPort(resourceType)
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
			Category:         p.ConnectionCategory(),
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
