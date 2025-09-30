package connector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/addp/manager/internal/models"
)

type SystemClient struct {
	BaseURL string
	client  *http.Client
}

func NewSystemClient(baseURL string) *SystemClient {
	return &SystemClient{
		BaseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetResources 从 System 模块获取所有存储引擎资源
func (c *SystemClient) GetResources(token string) ([]models.SystemResource, error) {
	url := fmt.Sprintf("%s/api/resources?page=1&page_size=100", c.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call system API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system API error: %s, body: %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var resources []models.SystemResource
	if err := json.Unmarshal(body, &resources); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resources, nil
}

// GetResourceByID 从 System 模块获取单个资源
func (c *SystemClient) GetResourceByID(id uint, token string) (*models.SystemResource, error) {
	url := fmt.Sprintf("%s/api/resources/%d", c.BaseURL, id)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call system API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("system API error: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var resource models.SystemResource
	if err := json.Unmarshal(body, &resource); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resource, nil
}