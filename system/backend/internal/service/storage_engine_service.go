package service

import (
	"context"

	"github.com/addp/common/database/plugin"
	"github.com/addp/common/dbbridge"
	"github.com/addp/system/internal/models"
)

type StorageEngineService struct{}

func NewStorageEngineService() *StorageEngineService {
	return &StorageEngineService{}
}

// TestConnection 测试存储引擎连接
// 使用插件系统统一处理所有数据库类型的连接测试
func (s *StorageEngineService) TestConnection(resource *models.Resource) error {
	return dbbridge.TestConnection(context.Background(), resource)
}

// GetConnectionInfo 获取存储引擎连接信息（用于前端展示，隐藏敏感信息）
func (s *StorageEngineService) GetConnectionInfo(resource *models.Resource) map[string]interface{} {
	result := make(map[string]interface{})
	result["type"] = resource.ResourceType

	// 从插件系统获取敏感字段列表
	sensitiveFields, err := dbbridge.GetSensitiveFields(resource.ResourceType)
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
func (s *StorageEngineService) ListSchemas(resource *models.Resource) ([]plugin.SchemaInfo, error) {
	// 获取或创建连接池
	db, err := dbbridge.GetOrCreatePool(resource, nil)
	if err != nil {
		return nil, err
	}

	// 使用 dbbridge 列出 schemas
	return dbbridge.ListSchemas(context.Background(), resource, db)
}

// ListTables 列出指定资源和Schema下的所有表
func (s *StorageEngineService) ListTables(resource *models.Resource, schema string) ([]plugin.TableInfo, error) {
	// 获取或创建连接池
	db, err := dbbridge.GetOrCreatePool(resource, nil)
	if err != nil {
		return nil, err
	}

	// 使用 dbbridge 列出表
	return dbbridge.ListTables(context.Background(), resource, db, schema)
}
