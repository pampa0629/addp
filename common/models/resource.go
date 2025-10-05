package models

import (
	"fmt"
)

// Resource 资源信息
type Resource struct {
	ID             uint                   `json:"id"`
	TenantID       uint                   `json:"tenant_id"`
	ResourceName   string                 `json:"name"`
	ResourceType   string                 `json:"resource_type"`
	ConnectionInfo map[string]interface{} `json:"connection_info"`
	Status         string                 `json:"status"`
	Description    string                 `json:"description"`
	IsActive       bool                   `json:"is_active"`
}

// BuildConnectionString 根据资源信息构建连接字符串
func BuildConnectionString(resource *Resource) (string, error) {
	connInfo := resource.ConnectionInfo

	// 辅助函数:从interface{}转换为字符串
	getString := func(key string) string {
		if v, ok := connInfo[key]; ok {
			switch val := v.(type) {
			case string:
				return val
			case float64:
				return fmt.Sprintf("%.0f", val)
			case int:
				return fmt.Sprintf("%d", val)
			default:
				return fmt.Sprintf("%v", val)
			}
		}
		return ""
	}

	switch resource.ResourceType {
	case "postgresql", "PostgreSQL":
		host := getString("host")
		port := getString("port")
		user := getString("user")
		password := getString("password")
		dbname := getString("database")

		if host == "" || port == "" || user == "" || password == "" {
			return "", fmt.Errorf("missing required PostgreSQL connection info")
		}

		return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			host, port, user, password, dbname), nil

	case "mysql", "MySQL":
		host := getString("host")
		port := getString("port")
		user := getString("user")
		password := getString("password")
		dbname := getString("database")

		if host == "" || port == "" || user == "" || password == "" {
			return "", fmt.Errorf("missing required MySQL connection info")
		}

		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
			user, password, host, port, dbname), nil

	default:
		return "", fmt.Errorf("unsupported resource type: %s", resource.ResourceType)
	}
}
