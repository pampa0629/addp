package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/database/plugin"
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

// ListSchemas 列出指定资源的所有Schema/Database
func (s *StorageEngineService) ListSchemas(resource *models.Engine) ([]plugin.SchemaInfo, error) {
	// 获取或创建连接池
	db, err := dbbridge.GetOrCreatePool(resource, nil)
	if err != nil {
		return nil, err
	}

	// 使用 dbbridge 列出 schemas
	return dbbridge.ListSchemas(context.Background(), resource, db)
}

// ListTables 列出指定资源和Schema下的所有表
func (s *StorageEngineService) ListTables(resource *models.Engine, schema string) ([]plugin.TableInfo, error) {
	// 获取或创建连接池
	db, err := dbbridge.GetOrCreatePool(resource, nil)
	if err != nil {
		return nil, err
	}

	// 使用 dbbridge 列出表
	return dbbridge.ListTables(context.Background(), resource, db, schema)
}
