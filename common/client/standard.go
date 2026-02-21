package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type StandardClient struct {
	baseURL     string
	httpClient  *http.Client
	authToken   string
	internalKey string
}

func NewStandardClient(baseURL string) *StandardClient {
	return &StandardClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func NewStandardClientWithInternalKey(baseURL, internalKey string) *StandardClient {
	return &StandardClient{
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		internalKey: internalKey,
	}
}

func (c *StandardClient) addAuth(req *http.Request) {
	if c.internalKey != "" {
		req.Header.Set("X-Internal-API-Key", c.internalKey)
	} else if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

type ElementResponse struct {
	ID           int64                  `json:"id"`
	TenantID     int64                  `json:"tenant_id"`
	Name         string                 `json:"name"`
	Code         string                 `json:"code"`
	DataType     string                 `json:"data_type"`
	CodeSetID    *int64                 `json:"code_set_id"`
	QualityRules map[string]interface{} `json:"quality_rules"`
}

// ValidateElement 验证数据元是否存在（用于跨模块引用验证）
func (c *StandardClient) ValidateElement(elementID int64, tenantID int64) error {
	url := fmt.Sprintf("%s/api/standard/elements/%d", c.baseURL, elementID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("element_id %d not found", elementID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("standard api returned status %d: %s", resp.StatusCode, string(body))
	}

	var element ElementResponse
	if err := json.NewDecoder(resp.Body).Decode(&element); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if element.TenantID != tenantID {
		return fmt.Errorf("element_id %d belongs to tenant %d, not %d", elementID, element.TenantID, tenantID)
	}

	return nil
}

// GetElement 获取数据元详情
func (c *StandardClient) GetElement(elementID int64, tenantID int64) (*ElementResponse, error) {
	url := fmt.Sprintf("%s/api/standard/elements/%d", c.baseURL, elementID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("standard api returned status %d: %s", resp.StatusCode, string(body))
	}

	var element ElementResponse
	if err := json.NewDecoder(resp.Body).Decode(&element); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &element, nil
}
