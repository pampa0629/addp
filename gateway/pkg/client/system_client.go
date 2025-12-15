package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SystemClient 用于调用 System 模块的内部 API
type SystemClient struct {
	baseURL    string
	httpClient *http.Client
	internalKey string // INTERNAL_API_KEY
}

// APIKeyValidationResponse System 返回的 API Key 验证响应
type APIKeyValidationResponse struct {
	Valid              bool      `json:"valid"`
	AppID              uint      `json:"app_id"`
	AppName            string    `json:"app_name"`
	AllowedServices    []string  `json:"allowed_services"`
	RateLimitPerMinute int       `json:"rate_limit_per_minute"`
	ExpiresAt          *time.Time `json:"expires_at"`
}

// NewSystemClient 创建 System 客户端
func NewSystemClient(baseURL, internalKey string) *SystemClient {
	return &SystemClient{
		baseURL:    baseURL,
		internalKey: internalKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Second, // 5 秒超时
		},
	}
}

// ValidateAPIKey 验证 API Key
// keyHash: API Key 的 SHA256 hash（hex 编码）
func (c *SystemClient) ValidateAPIKey(keyHash string) (*APIKeyValidationResponse, error) {
	url := fmt.Sprintf("%s/internal/api-keys/validate?key_hash=%s", c.baseURL, keyHash)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	// 添加内部 API Key 认证
	req.Header.Set("X-Internal-API-Key", c.internalKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result APIKeyValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}

	return &result, nil
}

// BulkGetAPIKeys 批量获取所有有效的 API Keys（用于预加载缓存）
// TODO: Phase 2 实现
func (c *SystemClient) BulkGetAPIKeys() ([]APIKeyValidationResponse, error) {
	url := fmt.Sprintf("%s/internal/api-keys/bulk", c.baseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("X-Internal-API-Key", c.internalKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result []APIKeyValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}

	return result, nil
}
