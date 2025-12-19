package plugin

import (
	"context"
	"fmt"
)

// TestConnection 统一入口：测试数据库连接
// 自动查找对应类型的插件并调用其 TestConnection 方法
func TestConnection(ctx context.Context, resource *Resource) error {
	if resource == nil {
		return fmt.Errorf("resource cannot be nil")
	}

	plugin, err := Get(resource.ResourceType)
	if err != nil {
		return err
	}

	return plugin.TestConnection(ctx, resource.ConnectionInfo)
}

// BuildConnectionString 统一入口：构建连接字符串
// 自动查找对应类型的插件并调用其 BuildConnectionString 方法
func BuildConnectionString(resource *Resource) (string, error) {
	if resource == nil {
		return "", fmt.Errorf("resource cannot be nil")
	}

	plugin, err := Get(resource.ResourceType)
	if err != nil {
		return "", err
	}

	return plugin.BuildConnectionString(resource.ConnectionInfo)
}

// ValidateConnectionInfo 统一入口：验证连接信息
// 自动查找对应类型的插件并调用其 ValidateConnectionInfo 方法
func ValidateConnectionInfo(resourceType string, connInfo ConnectionInfo) error {
	plugin, err := Get(resourceType)
	if err != nil {
		return err
	}

	return plugin.ValidateConnectionInfo(connInfo)
}

// GenerateCapabilities 统一入口：生成资源能力描述
// 自动查找对应类型的插件并调用其 GenerateCapabilities 方法
func GenerateCapabilities(resourceType string) (string, error) {
	plugin, err := Get(resourceType)
	if err != nil {
		return "", err
	}

	return plugin.GenerateCapabilities(), nil
}

// GetRequiredFields 获取指定类型的必填字段列表
func GetRequiredFields(resourceType string) ([]string, error) {
	plugin, err := Get(resourceType)
	if err != nil {
		return nil, err
	}

	return plugin.RequiredFields(), nil
}

// GetSensitiveFields 获取指定类型的敏感字段列表
func GetSensitiveFields(resourceType string) ([]string, error) {
	plugin, err := Get(resourceType)
	if err != nil {
		return nil, err
	}

	return plugin.SensitiveFields(), nil
}

// GetDefaultPort 获取指定类型的默认端口
func GetDefaultPort(resourceType string) (int, error) {
	plugin, err := Get(resourceType)
	if err != nil {
		return 0, err
	}

	return plugin.DefaultPort(), nil
}

// GetDisplayName 获取指定类型的显示名称
func GetDisplayName(resourceType string) (string, error) {
	plugin, err := Get(resourceType)
	if err != nil {
		return "", err
	}

	return plugin.DisplayName(), nil
}

// GetConnectionCategory 获取指定类型的连接类别
func GetConnectionCategory(resourceType string) (string, error) {
	plugin, err := Get(resourceType)
	if err != nil {
		return "", err
	}

	return plugin.ConnectionCategory(), nil
}
