package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/models"
)

// SystemClient 系统服务客户端
type SystemClient struct {
	baseURL     string
	httpClient  *http.Client
	authToken   string // JWT Token (用于用户认证的 API)
	internalKey string // Internal API Key (用于服务间调用)
}

// NewSystemClient 创建系统客户端（用户认证方式）
func NewSystemClient(baseURL, authToken string) *SystemClient {
	return &SystemClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		authToken: authToken,
	}
}

// NewSystemClientWithInternalKey 创建系统客户端（服务间调用方式）
func NewSystemClientWithInternalKey(baseURL, internalKey string) *SystemClient {
	return &SystemClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		internalKey: internalKey,
	}
}

// addAuth 添加认证头（根据客户端类型选择 JWT 或 Internal Key）
func (c *SystemClient) addAuth(req *http.Request) {
	if c.internalKey != "" {
		// 服务间调用使用 Internal API Key
		req.Header.Set("X-Internal-API-Key", c.internalKey)
	} else if c.authToken != "" {
		// 用户调用使用 JWT Token
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

// GetResource 获取资源详情
func (c *SystemClient) GetResource(resourceID uint) (*models.Resource, error) {
	var url string
	// 如果使用内部 API Key，调用内部 API
	if c.internalKey != "" {
		url = fmt.Sprintf("%s/internal/resources/%d", c.baseURL, resourceID)
	} else {
		url = fmt.Sprintf("%s/api/resources/%d", c.baseURL, resourceID)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
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

	var resource models.Resource
	if err := json.NewDecoder(resp.Body).Decode(&resource); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resource, nil
}

// ListResources 获取资源列表
func (c *SystemClient) ListResources(resourceType string, tenantID uint) ([]models.Resource, error) {
	var url string
	// 如果使用内部 API Key，调用内部 API
	if c.internalKey != "" {
		url = fmt.Sprintf("%s/internal/resources", c.baseURL)
	} else {
		url = fmt.Sprintf("%s/api/resources", c.baseURL)
	}

	queryAdded := false
	if resourceType != "" {
		url += "?resource_type=" + resourceType
		queryAdded = true
	}
	if tenantID > 0 {
		prefix := "?"
		if queryAdded || strings.Contains(url, "?") {
			prefix = "&"
		}
		url += fmt.Sprintf("%stenant_id=%d", prefix, tenantID)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
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

	var resources []models.Resource
	if err := json.NewDecoder(resp.Body).Decode(&resources); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return resources, nil
}
