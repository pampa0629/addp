package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	commonJSON "github.com/addp/common/jsonmap"
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

type MetaScanOptions struct {
	EngineID uint
	NodeID   uint
	ItemID   uint
	Targets  []string

	Namespaces  []string
	ObjectPaths []string
	ScanDepth   string
	Force       bool
	TriggerType string
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

// WithTenantID 返回一个带有租户 ID 的新 MetaClient（复用 httpClient，线程安全）
func (c *MetaClient) WithTenantID(tenantID uint) *MetaClient {
	return &MetaClient{
		baseURL:     c.baseURL,
		httpClient:  c.httpClient,
		authToken:   c.authToken,
		internalKey: c.internalKey,
		tenantID:    &tenantID,
	}
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
	url := fmt.Sprintf("%s/api/v1/meta/engines/%d/tree", c.baseURL, engineID)

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

	var result models.MetadataTree
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetNodeByPath 按路径查询节点（支持 Schema/Bucket/Prefix）
func (c *MetaClient) GetNodeByPath(engineID uint, nodePath string) (*models.MetaNode, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/nodes/by-path?engine_id=%d&path=%s",
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

	var result models.MetaNode
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetItemByPath 按路径查询项目（对象存储）
func (c *MetaClient) GetItemByPath(engineID uint, bucketName, objectPath string) (*models.MetaItem, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/items/by-path?engine_id=%d&bucket=%s&path=%s",
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

	var result models.MetaItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetNodeChildren 获取节点的子节点
func (c *MetaClient) GetNodeChildren(nodeID uint) ([]models.MetaNode, error) {
	url := fmt.Sprintf("%s/api/v1/meta/nodes/%d/children", c.baseURL, nodeID)

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

	var result []models.MetaNode
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// GetNodeItems 获取节点下的项目
func (c *MetaClient) GetNodeItems(nodeID uint) ([]models.MetaItem, error) {
	url := fmt.Sprintf("%s/api/v1/meta/nodes/%d/items", c.baseURL, nodeID)

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

	var result []models.MetaItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// GetMetaNode 获取单个节点详情
func (c *MetaClient) GetMetaNode(nodeID uint) (*models.MetaNode, error) {
	url := fmt.Sprintf("%s/api/v1/meta/nodes/%d", c.baseURL, nodeID)

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

	var result models.MetaNode
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetMetaItemByID 获取单个 MetaItem 详情
func (c *MetaClient) GetMetaItemByID(itemID uint) (*models.MetaItem, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/items/%d", c.baseURL, itemID)

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

	var result models.MetaItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (c *MetaClient) TriggerScan(opts MetaScanOptions) error {
	urlStr := fmt.Sprintf("%s/api/v1/meta/scan/engine", c.baseURL)

	scanReq := map[string]interface{}{
		"engine_id":    opts.EngineID,
		"node_id":      opts.NodeID,
		"item_id":      opts.ItemID,
		"targets":      opts.Targets,
		"scan_depth":   opts.ScanDepth,
		"force":        opts.Force,
		"trigger_type": opts.TriggerType,
	}
	if scanReq["scan_depth"] == "" {
		scanReq["scan_depth"] = "basic"
	}
	if scanReq["trigger_type"] == "" {
		scanReq["trigger_type"] = "manual"
	}
	if len(opts.Namespaces) > 0 {
		scanReq["namespaces"] = opts.Namespaces
	}
	if len(opts.ObjectPaths) > 0 {
		scanReq["object_paths"] = opts.ObjectPaths
	}

	body, err := json.Marshal(scanReq)
	if err != nil {
		return fmt.Errorf("failed to marshal scan request: %w", err)
	}

	req, err := http.NewRequest("POST", urlStr, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// TriggerScanEngine 触发引擎元数据扫描。
func (c *MetaClient) TriggerScanEngine(engineID uint, namespaces []string) error {
	return c.TriggerScan(MetaScanOptions{
		EngineID:    engineID,
		Namespaces:  namespaces,
		ScanDepth:   "basic",
		Force:       false,
		TriggerType: "manual",
	})
}

func (c *MetaClient) EnsureItemDeepScanned(itemID uint) error {
	return c.TriggerScan(MetaScanOptions{
		ItemID:      itemID,
		ScanDepth:   "deep",
		Force:       false,
		TriggerType: "manual",
	})
}

func (c *MetaClient) ForceRefreshItem(itemID uint) error {
	return c.TriggerScan(MetaScanOptions{
		ItemID:      itemID,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: "manual",
	})
}

func (c *MetaClient) ForceRefreshNode(nodeID uint) error {
	return c.TriggerScan(MetaScanOptions{
		NodeID:      nodeID,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: "manual",
	})
}

// ListItems 获取引擎的已扫描数据项列表，支持按命名空间过滤。
func (c *MetaClient) ListItems(engineID uint, namespace string) ([]models.MetaItem, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/engines/%d/items", c.baseURL, engineID)
	if namespace != "" {
		urlStr += "?namespace=" + url.QueryEscape(namespace)
	}

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
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result []models.MetaItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// GetItemFields 获取数据项字段列表。
func (c *MetaClient) GetItemFields(engineID uint, namespace, itemName string, includeDetails bool) ([]models.FieldInfo, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/engines/%d/items/fields?name=%s",
		c.baseURL, engineID, url.QueryEscape(itemName))
	if namespace != "" {
		urlStr += "&namespace=" + url.QueryEscape(namespace)
	}
	if includeDetails {
		urlStr += "&include_details=true"
	}

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
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result []models.FieldInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// GetItemSpatialMetadata 获取数据项空间元数据。
func (c *MetaClient) GetItemSpatialMetadata(engineID uint, namespace, itemName string) (*models.SpatialMetadata, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/engines/%d/items/spatial?name=%s",
		c.baseURL, engineID, url.QueryEscape(itemName))
	if namespace != "" {
		urlStr += "&namespace=" + url.QueryEscape(namespace)
	}

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
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result models.SpatialMetadata
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ObjectMetadataRequest 描述按需对象元数据提取请求。
type ObjectMetadataRequest struct {
	EngineID   uint
	ObjectKey  string
	ObjectData io.Reader
}

// ExtractObjectMetadata 按需调用 Meta 提取对象深度元数据。
func (c *MetaClient) ExtractObjectMetadata(req *ObjectMetadataRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("object metadata request is required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/meta/metadata/extract?engine_id=%d&object_key=%s",
		c.baseURL, req.EngineID, url.QueryEscape(req.ObjectKey))

	httpReq, err := http.NewRequest("POST", endpoint, req.ObjectData)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		return data, nil
	}
	return result, nil
}

// BuildObjectContentIndex 按需调用 Meta 建立对象内容索引。
func (c *MetaClient) BuildObjectContentIndex(req *ObjectMetadataRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("object metadata request is required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/meta/metadata/content-index?engine_id=%d&object_key=%s",
		c.baseURL, req.EngineID, url.QueryEscape(req.ObjectKey))

	httpReq, err := http.NewRequest("POST", endpoint, req.ObjectData)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Attributes map[string]interface{} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Data.Attributes, nil
}

// GetObjectMetadata 获取已存储的对象提取元数据。
func (c *MetaClient) GetObjectMetadata(engineID uint, objectKey string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/api/v1/meta/metadata/object?engine_id=%d&object_key=%s",
		c.baseURL, engineID, url.QueryEscape(objectKey))

	req, err := http.NewRequest("GET", endpoint, nil)
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

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if extracted, ok := commonJSON.Value(result, "capabilities.extraction", "extracted_metadata").(map[string]interface{}); ok {
		return extracted, nil
	}
	return nil, nil
}

// TryExtractMetadata 先读取已有对象元数据，缺失时再按需提取。
func (c *MetaClient) TryExtractMetadata(engineID uint, objectKey string, objectDataProvider func() (io.Reader, error)) (map[string]interface{}, error) {
	existing, err := c.GetObjectMetadata(engineID, objectKey)
	if err == nil && existing != nil {
		return existing, nil
	}
	if objectDataProvider == nil {
		return nil, fmt.Errorf("no object data provider for extraction")
	}
	objectData, err := objectDataProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to get object data: %w", err)
	}
	return c.ExtractObjectMetadata(&ObjectMetadataRequest{
		EngineID:   engineID,
		ObjectKey:  objectKey,
		ObjectData: objectData,
	})
}
