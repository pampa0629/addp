package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/addp/common/models"
)

// SystemClient 系统服务客户端
type SystemClient struct {
	baseURL    string
	httpClient *http.Client
	authToken  string
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
func (c *SystemClient) GetResource(resourceID uint) (*models.Resource, error) {
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

	var resource models.Resource
	if err := json.NewDecoder(resp.Body).Decode(&resource); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resource, nil
}

// ListResources 获取资源列表
func (c *SystemClient) ListResources(resourceType string) ([]models.Resource, error) {
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

	var resources []models.Resource
	if err := json.NewDecoder(resp.Body).Decode(&resources); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return resources, nil
}
