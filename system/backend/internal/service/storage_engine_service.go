package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/dbbridge"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
)

type StorageEngineService struct{}

func NewStorageEngineService() *StorageEngineService {
	return &StorageEngineService{}
}

// TestConnection 测试引擎连接
// 根据 engine_category 区分测试方式：
// - standard: 使用数据库插件测试（dbbridge）
// - extension: 使用 HTTP 健康检查（/health 端点）
func (s *StorageEngineService) TestConnection(resource *models.Engine) error {
	// 根据引擎分类选择测试方式
	if resource.EngineCategory == "extension" {
		return s.testExtensionEngineConnection(resource)
	}

	// 默认使用数据库插件测试（standard 引擎）
	return dbbridge.TestConnection(context.Background(), resource)
}

// testExtensionEngineConnection 测试扩展引擎连接（HTTP 健康检查）
func (s *StorageEngineService) testExtensionEngineConnection(resource *models.Engine) error {
	// 构建 base URL
	baseURL, err := commonmodels.BuildBaseURL(resource.ConnectionInfo)
	if err != nil {
		return fmt.Errorf("构建引擎 URL 失败: %w", err)
	}

	// 获取健康检查配置
	healthCheckEndpoint := "/health"
	timeout := 5 * time.Second

	// 如果引擎类型有标准配置，使用标准配置
	if standard, ok := commonmodels.WorkflowStandards[resource.EngineType]; ok {
		healthCheckEndpoint = standard.HealthCheck.Endpoint
		timeout = time.Duration(standard.HealthCheck.Timeout) * time.Second
	}

	// 构建完整的健康检查 URL
	healthURL := strings.TrimRight(baseURL, "/") + healthCheckEndpoint

	// 创建 HTTP 客户端
	client := &http.Client{
		Timeout: timeout,
	}

	// 发送 GET 请求
	resp, err := client.Get(healthURL)
	if err != nil {
		return fmt.Errorf("健康检查失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("健康检查失败: HTTP %d", resp.StatusCode)
	}

	return nil
}

// GetConnectionInfo 获取存储引擎连接信息（用于前端展示，隐藏敏感信息）
func (s *StorageEngineService) GetConnectionInfo(resource *models.Engine) map[string]interface{} {
	result := make(map[string]interface{})
	result["type"] = resource.EngineType

	// 从插件系统获取敏感字段列表
	sensitiveFields, err := dbbridge.GetSensitiveFields(resource.EngineType)
	if err != nil {
		// 降级：使用默认敏感字段列表
		sensitiveFields = []string{"password", "secret_key", "access_key", "token"}
	}

	// 创建敏感字段的快速查找map
	sensitiveMap := make(map[string]bool)
	for _, field := range sensitiveFields {
		sensitiveMap[field] = true
	}

	// 遍历所有连接信息字段
	for key, value := range resource.ConnectionInfo {
		if sensitiveMap[key] {
			// 敏感字段：access_key部分隐藏，其他完全隐藏
			if key == "access_key" {
				result[key] = maskString(value)
			} else {
				result[key] = "******"
			}
		} else {
			// 非敏感字段：直接返回
			result[key] = value
		}
	}

	return result
}

// maskString 部分隐藏字符串（仅用于access_key等需要部分显示的字段）
func maskString(value interface{}) string {
	if str, ok := value.(string); ok {
		if len(str) <= 8 {
			return "****"
		}
		return str[:4] + "****" + str[len(str)-4:]
	}
	return "****"
}

// NamespaceInfo 表示 catalog 第一层命名空间。
type NamespaceInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CatalogItemInfo 表示 catalog 叶子数据项。
type CatalogItemInfo struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

// ListNamespaces 列出指定资源的 catalog 命名空间。
func (s *StorageEngineService) ListNamespaces(resource *models.Engine) ([]NamespaceInfo, error) {
	nodes, err := dbbridge.ListNamespaces(context.Background(), resource)
	if err != nil {
		return nil, err
	}
	namespaces := make([]NamespaceInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsContainer {
			continue
		}
		namespaces = append(namespaces, NamespaceInfo{
			Name:        node.Name,
			Description: stringAttribute(node.Attributes, "description"),
		})
	}
	return namespaces, nil
}

// ListCatalogItems 列出指定命名空间下的 catalog 叶子数据项。
func (s *StorageEngineService) ListCatalogItems(resource *models.Engine, namespace string) ([]CatalogItemInfo, error) {
	nodes, err := dbbridge.ListItems(context.Background(), resource, namespace)
	if err != nil {
		return nil, err
	}
	items := make([]CatalogItemInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsItem {
			continue
		}
		items = append(items, CatalogItemInfo{
			Name:        node.Name,
			Namespace:   namespace,
			Type:        node.Kind,
			Description: stringAttribute(node.Attributes, "description"),
		})
	}
	return items, nil
}

func stringAttribute(attributes map[string]interface{}, key string) string {
	if attributes == nil {
		return ""
	}
	if value, ok := attributes[key].(string); ok {
		return value
	}
	return ""
}
