package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SystemClient 系统服务客户端
type SystemClient struct {
	baseURL    string
	httpClient *http.Client
	authToken  string
}

// Resource 资源信息
type Resource struct {
	ID             uint                   `json:"id"`
	TenantID       uint                   `json:"tenant_id"`
	ResourceName   string                 `json:"name"`
	ResourceType   string                 `json:"resource_type"`
	ConnectionInfo map[string]interface{} `json:"connection_info"`
	Status         string                 `json:"status"`
	Description    string                 `json:"description"`
}

// NewSystemClient 创建系统客户端
func NewSystemClient(baseURL, authToken string) *SystemClient {
	return &SystemClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		authToken: authToken,
	}
}

// GetResource 获取资源详情
func (c *SystemClient) GetResource(resourceID uint) (*Resource, error) {
	url := fmt.Sprintf("%s/api/resources/%d", c.baseURL, resourceID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.authToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var resource Resource
	if err := json.NewDecoder(resp.Body).Decode(&resource); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resource, nil
}

// ListResources 获取资源列表
func (c *SystemClient) ListResources(resourceType string) ([]Resource, error) {
	url := fmt.Sprintf("%s/api/resources", c.baseURL)
	if resourceType != "" {
		url += "?resource_type=" + resourceType
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.authToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var resources []Resource
	if err := json.NewDecoder(resp.Body).Decode(&resources); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return resources, nil
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
