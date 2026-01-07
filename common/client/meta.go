package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/addp/common/models"
)

// MetaClient Meta 服务客户端
type MetaClient struct {
	baseURL     string
	httpClient  *http.Client
	authToken   string // JWT Token (用于用户认证的 API)
	internalKey string // Internal API Key (用于服务间调用)
	tenantID    *uint  // Tenant ID (用于服务间调用时指定租户)
}

// NewMetaClient 创建 Meta 客户端（用户认证方式）
func NewMetaClient(baseURL, authToken string) *MetaClient {
	return &MetaClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Meta 查询可能较慢，使用 60 秒超时
		},
		authToken: authToken,
	}
}

// NewMetaClientWithInternalKey 创建 Meta 客户端（服务间调用方式）
func NewMetaClientWithInternalKey(baseURL, internalKey string) *MetaClient {
	return &MetaClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		internalKey: internalKey,
	}
}

// SetTenantID 设置租户 ID（用于服务间调用）
func (c *MetaClient) SetTenantID(tenantID *uint) {
	c.tenantID = tenantID
}

// addAuth 添加认证头（根据客户端类型选择 JWT 或 Internal Key）
func (c *MetaClient) addAuth(req *http.Request) {
	if c.internalKey != "" {
		// 服务间调用使用 Internal API Key
		req.Header.Set("X-Internal-API-Key", c.internalKey)
		// 如果设置了 tenantID，添加到请求头
		if c.tenantID != nil {
			req.Header.Set("X-Tenant-ID", fmt.Sprintf("%d", *c.tenantID))
		}
	} else if c.authToken != "" {
		// 用户调用使用 JWT Token
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

// GetMetadataTree 获取引擎的完整元数据树
func (c *MetaClient) GetMetadataTree(engineID uint) (*models.MetadataTree, error) {
	url := fmt.Sprintf("%s/api/meta/engines/%d/tree", c.baseURL, engineID)

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
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data *models.MetadataTree `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetNodeByPath 按路径查询节点（支持 Schema/Bucket/Prefix）
func (c *MetaClient) GetNodeByPath(engineID uint, nodePath string) (*models.MetaNode, error) {
	urlStr := fmt.Sprintf("%s/api/meta/nodes/by-path?engine_id=%d&path=%s",
		c.baseURL, engineID, url.QueryEscape(nodePath))

	req, err := http.NewRequest("GET", urlStr, nil)
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
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data *models.MetaNode `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetItemByPath 按路径查询项目（对象存储）
func (c *MetaClient) GetItemByPath(engineID uint, bucketName, objectPath string) (*models.MetaItem, error) {
	urlStr := fmt.Sprintf("%s/api/meta/items/by-path?engine_id=%d&bucket=%s&path=%s",
		c.baseURL, engineID, url.QueryEscape(bucketName), url.QueryEscape(objectPath))

	req, err := http.NewRequest("GET", urlStr, nil)
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
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data *models.MetaItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetNodeChildren 获取节点的子节点
func (c *MetaClient) GetNodeChildren(nodeID uint) ([]models.MetaNode, error) {
	url := fmt.Sprintf("%s/api/meta/nodes/%d/children", c.baseURL, nodeID)

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
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []models.MetaNode `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetNodeItems 获取节点下的项目
func (c *MetaClient) GetNodeItems(nodeID uint) ([]models.MetaItem, error) {
	url := fmt.Sprintf("%s/api/meta/nodes/%d/items", c.baseURL, nodeID)

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
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []models.MetaItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetTableSpatialMetadata 获取表的空间元数据（MVT专用）
func (c *MetaClient) GetTableSpatialMetadata(engineID uint, schema, table string) (*models.SpatialMetadata, error) {
	urlStr := fmt.Sprintf("%s/api/meta/metadata/tables/spatial?engine_id=%d&schema=%s&table=%s",
		c.baseURL, engineID, url.QueryEscape(schema), url.QueryEscape(table))

	req, err := http.NewRequest("GET", urlStr, nil)
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
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data *models.SpatialMetadata `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}

// GetMetaNode 获取单个节点详情
func (c *MetaClient) GetMetaNode(nodeID uint) (*models.MetaNode, error) {
	url := fmt.Sprintf("%s/api/meta/nodes/%d", c.baseURL, nodeID)

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
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data *models.MetaNode `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Data, nil
}
