package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
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

	CatalogPaths []string
	RefGroups    []MetaScanRefGroup
	ScanDepth    string
	Force        bool
	TriggerType  string
	Source       string
}

type MetaScanRefGroup struct {
	Primary string        `json:"primary"`
	Refs    []MetaScanRef `json:"refs"`
}

type MetaScanRef struct {
	Path     string `json:"path"`
	Role     string `json:"role"`
	Required bool   `json:"required"`
}

// MetaScanResponse 元数据扫描响应
type MetaScanResponse struct {
	Status              string                   `json:"status"`
	Message             string                   `json:"message"`
	CatalogNodesScanned int                      `json:"catalog_nodes_scanned"`
	ItemsScanned        int                      `json:"items_scanned"`
	FieldsScanned       int                      `json:"fields_scanned"`
	DurationMs          int64                    `json:"duration_ms"`
	StartedAt           string                   `json:"started_at"`
	Extraction          *MetaExtractionScanStats `json:"extraction,omitempty"`
}

type MetaAutoScanRunsResponse struct {
	Runs      []commonExecution.TaskExecution `json:"runs"`
	Submitted int                             `json:"submitted"`
}

type MetaExtractionScanStats struct {
	Documents   int `json:"documents"`
	Extracted   int `json:"extracted"`
	Unsupported int `json:"unsupported"`
	Failed      int `json:"failed"`
	Indexed     int `json:"indexed"`
	IndexFailed int `json:"index_failed"`
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

func (c *MetaClient) WithAuthToken(authToken string) *MetaClient {
	return &MetaClient{
		baseURL:    c.baseURL,
		httpClient: c.httpClient,
		authToken:  authToken,
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

// GetNodeByCatalogPath 按 catalog path 查询节点。
func (c *MetaClient) GetNodeByCatalogPath(engineID uint, catalogPath string) (*models.MetaNode, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/nodes/by-catalog-path?engine_id=%d&catalog_path=%s",
		c.baseURL, engineID, url.QueryEscape(catalogPath))

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

// GetItemByCatalogPath 按 catalog path 查询数据项。
func (c *MetaClient) GetItemByCatalogPath(engineID uint, catalogPath string) (*models.MetaItem, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/items/by-catalog-path?engine_id=%d&catalog_path=%s",
		c.baseURL, engineID, url.QueryEscape(catalogPath))

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

func (c *MetaClient) CreateManualScanRun(opts MetaScanOptions) (*commonExecution.TaskExecution, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/scan/run/manual", c.baseURL)

	scanReq := map[string]interface{}{}
	if opts.EngineID > 0 {
		scanReq["engine_id"] = opts.EngineID
	}
	if opts.NodeID > 0 {
		scanReq["node_id"] = opts.NodeID
	}
	if opts.ItemID > 0 {
		scanReq["item_id"] = opts.ItemID
	}
	if len(opts.Targets) > 0 {
		scanReq["targets"] = opts.Targets
	}
	if len(opts.CatalogPaths) > 0 {
		scanReq["catalog_paths"] = opts.CatalogPaths
	}
	if len(opts.RefGroups) > 0 {
		scanReq["ref_groups"] = opts.RefGroups
	}
	if depth := strings.TrimSpace(opts.ScanDepth); depth != "" {
		scanReq["scan_depth"] = depth
	} else {
		scanReq["scan_depth"] = "deep"
	}
	if opts.Force {
		scanReq["force"] = true
	}
	if triggerType := strings.TrimSpace(opts.TriggerType); triggerType != "" {
		scanReq["trigger_type"] = triggerType
	} else {
		scanReq["trigger_type"] = "manual"
	}
	if source := strings.TrimSpace(opts.Source); source != "" {
		scanReq["source"] = source
	}

	body, err := json.Marshal(scanReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal scan run request: %w", err)
	}

	req, err := http.NewRequest("POST", urlStr, bytes.NewReader(body))
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result commonExecution.TaskExecution
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return &result, nil
}

func (c *MetaClient) RefreshItem(itemID uint, opts MetaScanOptions) (*MetaScanResponse, error) {
	if itemID == 0 {
		return nil, fmt.Errorf("item_id is required")
	}
	urlStr := fmt.Sprintf("%s/api/v1/meta/items/%d/refresh", c.baseURL, itemID)
	reqPayload := map[string]interface{}{}
	if opts.EngineID > 0 {
		reqPayload["engine_id"] = opts.EngineID
	}
	if opts.Force {
		reqPayload["force"] = true
	}
	if depth := strings.TrimSpace(opts.ScanDepth); depth != "" {
		reqPayload["scan_depth"] = depth
	}
	if triggerType := strings.TrimSpace(opts.TriggerType); triggerType != "" {
		reqPayload["trigger_type"] = triggerType
	}
	if source := strings.TrimSpace(opts.Source); source != "" {
		reqPayload["source"] = source
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
	}
	req, err := http.NewRequest("POST", urlStr, bytes.NewReader(body))
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result MetaScanResponse
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return &result, nil
}

func (c *MetaClient) ForceRefreshItem(itemID uint) error {
	_, err := c.RefreshItem(itemID, MetaScanOptions{Force: true})
	return err
}

// ListItems 获取引擎的已扫描数据项列表，支持按 catalog 第一层业务分支过滤。
func (c *MetaClient) ListItems(engineID uint, branch string) ([]models.MetaItem, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/engines/%d/items", c.baseURL, engineID)
	if branch != "" {
		urlStr += "?branch=" + url.QueryEscape(branch)
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

// GetItemFieldsByCatalogPath 获取指定 catalog path 数据项的字段列表。
func (c *MetaClient) GetItemFieldsByCatalogPath(engineID uint, catalogPath string, includeDetails bool) ([]datatype.FieldInfo, error) {
	item, err := c.GetItemByCatalogPath(engineID, catalogPath)
	if err != nil {
		return nil, err
	}
	return c.GetItemFieldsByID(item.ID, includeDetails)
}

// GetItemFieldsByID 获取指定 item_id 数据项的字段列表。
func (c *MetaClient) GetItemFieldsByID(itemID uint, includeDetails bool) ([]datatype.FieldInfo, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/items/%d/fields", c.baseURL, itemID)
	if includeDetails {
		urlStr += "?include_details=true"
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

	var result []datatype.FieldInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// GetItemSpatialMetadataByCatalogPath 获取指定 catalog path 数据项的空间元数据。
func (c *MetaClient) GetItemSpatialMetadataByCatalogPath(engineID uint, catalogPath string) (*models.SpatialMetadata, error) {
	item, err := c.GetItemByCatalogPath(engineID, catalogPath)
	if err != nil {
		return nil, err
	}
	return c.GetItemSpatialMetadataByID(item.ID)
}

// GetItemSpatialMetadataByID 获取指定 item_id 数据项的空间元数据。
func (c *MetaClient) GetItemSpatialMetadataByID(itemID uint) (*models.SpatialMetadata, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/items/%d/spatial", c.baseURL, itemID)

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

// BuildObjectAccessIndex 按需调用 Meta 建立对象访问索引。
func (c *MetaClient) BuildObjectAccessIndex(req *ObjectMetadataRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("object metadata request is required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/meta/metadata/access-index?engine_id=%d&object_key=%s",
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

// GetObjectMetadata 获取已存储的对象标准 attributes。
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
	if attrs, ok := result["attributes"].(map[string]interface{}); ok {
		return attrs, nil
	}
	return nil, nil
}
