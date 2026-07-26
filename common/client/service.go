package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ServiceClient 数据服务模块客户端
// 供其他模块（如 portal）调用 service API 使用
type ServiceClient struct {
	baseURL     string
	httpClient  *http.Client
	internalKey string
}

// ServiceEndpointInfo 统一服务端点信息
type ServiceEndpointInfo struct {
	ServiceType string            `json:"service_type"` // query / registered / tile
	Title       string            `json:"title"`
	Endpoints   map[string]string `json:"endpoints"`
}

func NewServiceClientWithInternalKey(baseURL, internalKey string) *ServiceClient {
	return &ServiceClient{
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		internalKey: internalKey,
	}
}

func (c *ServiceClient) addAuth(req *http.Request) {
	if c.internalKey != "" {
		req.Header.Set("X-Internal-API-Key", c.internalKey)
	}
}

// GetEndpointsByRef 根据 source_reference（如 "query:123"）获取服务端点信息
func (c *ServiceClient) GetEndpointsByRef(tenantID int64, sourceRef string) (*ServiceEndpointInfo, error) {
	url := fmt.Sprintf("%s/api/v1/service/internal/endpoints?ref=%s", c.baseURL, sourceRef)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuth(req)
	req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", tenantID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("service api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result ServiceEndpointInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}
