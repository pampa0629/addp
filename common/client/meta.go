package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	baseURL            string
	httpClient         *http.Client
	serviceTokenSource ServiceTokenProvider
	tenantID           *uint
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

const (
	MetaScanDepthBasic = "basic"
	MetaScanDepthDeep  = "deep"
)

type MetaInspectRequest struct {
	Locator   string             `json:"locator"`
	RefGroups []MetaScanRefGroup `json:"ref_groups,omitempty"`
	ScanDepth string             `json:"scan_depth,omitempty"`
}

type MetaInspectResult struct {
	Attributes map[string]interface{} `json:"attributes"`
	FullName   string                 `json:"full_name,omitempty"`
	Name       string                 `json:"name,omitempty"`
	DataType   string                 `json:"data_type,omitempty"`
	Format     string                 `json:"format,omitempty"`
	Layout     string                 `json:"layout,omitempty"`
}

type MetaScanRefGroup struct {
	Primary string        `json:"primary"`
	Refs    []MetaScanRef `json:"refs"`
}

type MetaScanRef struct {
	Path     string `json:"path"`
	Role     string `json:"role"`
	Required bool   `json:"required"`
	Primary  bool   `json:"primary,omitempty"`
}

type MetaLineageServiceDependency struct {
	SourceItemID     uint                   `json:"source_item_id"`
	DependencyKind   string                 `json:"dependency_kind"`
	Granularity      string                 `json:"granularity,omitempty"`
	DependencyFields map[string]interface{} `json:"dependency_fields,omitempty"`
}

type MetaLineageServicePublication struct {
	ServiceID         uint                           `json:"service_id"`
	PublishedRevision string                         `json:"published_revision"`
	DependencyHash    string                         `json:"dependency_hash,omitempty"`
	Dependencies      []MetaLineageServiceDependency `json:"dependencies"`
}

func normalizeManualMetaTriggerType(triggerType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(triggerType))
	if normalized == "" || normalized == commonExecution.TriggerTypeManual {
		return commonExecution.TriggerTypeManual, nil
	}
	return "", fmt.Errorf("unsupported trigger_type %q: use manual", triggerType)
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

type MetaExtractionScanStats struct {
	Documents   int `json:"documents"`
	Extracted   int `json:"extracted"`
	Unsupported int `json:"unsupported"`
	Failed      int `json:"failed"`
	Indexed     int `json:"indexed"`
	IndexFailed int `json:"index_failed"`
}

func NewMetaClient(baseURL string, tokenSource ServiceTokenProvider) *MetaClient {
	return &MetaClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Meta 查询可能较慢，使用 60 秒超时
		},
		serviceTokenSource: tokenSource,
	}
}

func (c *MetaClient) WithTenantID(tenantID uint) *MetaClient {
	return &MetaClient{
		baseURL: c.baseURL, httpClient: c.httpClient,
		serviceTokenSource: c.serviceTokenSource, tenantID: &tenantID,
	}
}

// RecordServicePublication records a published Service revision in Meta lineage.
func (c *MetaClient) RecordServicePublication(ctx context.Context, publication MetaLineageServicePublication) error {
	if c == nil || c.baseURL == "" || c.tenantID == nil || *c.tenantID == 0 {
		return errors.New("Meta lineage publication requires a tenant context")
	}
	body, err := json.Marshal(publication)
	if err != nil {
		return fmt.Errorf("marshal Meta lineage publication: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/meta/lineage/services", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Meta lineage publication request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.do(request)
	if err != nil {
		return fmt.Errorf("send Meta lineage publication request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return fmt.Errorf("Meta lineage publication returned status %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	return nil
}

func (c *MetaClient) GetItemByIDForTenant(tenantID, itemID uint) (*models.MetaItem, error) {
	return c.WithTenantID(tenantID).GetItemByID(itemID)
}

func (c *MetaClient) addAuth(req *http.Request) error {
	if c == nil || c.serviceTokenSource == nil {
		return errors.New("Meta request has no service token provider")
	}
	if c.tenantID == nil || *c.tenantID == 0 {
		return errors.New("Meta service request requires a tenant context")
	}
	token, err := c.serviceTokenSource.Token(req.Context(), *c.tenantID)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func (c *MetaClient) do(req *http.Request) (*http.Response, error) {
	if c == nil || c.httpClient == nil || req == nil {
		return nil, errors.New("Meta request is required")
	}
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		if err := c.addAuth(req); err != nil {
			return nil, err
		}
		token = strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	}
	response, err := c.httpClient.Do(req)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}
	invalidator, ok := c.serviceTokenSource.(ServiceTokenInvalidator)
	if !ok || c.tenantID == nil || *c.tenantID == 0 || token == "" || (req.Body != nil && req.GetBody == nil) {
		return response, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
	invalidator.InvalidateToken(*c.tenantID, token)
	newToken, err := c.serviceTokenSource.Token(req.Context(), *c.tenantID)
	if err != nil {
		return nil, err
	}
	if req.GetBody != nil {
		req.Body, err = req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("recreate Meta request body: %w", err)
		}
	}
	req.Header.Set("Authorization", "Bearer "+newToken)
	return c.httpClient.Do(req)
}

// GetMetadataTree 获取引擎的完整元数据树
func (c *MetaClient) GetMetadataTree(engineID uint) (*models.MetadataTree, error) {
	url := fmt.Sprintf("%s/api/v1/meta/engines/%d/tree", c.baseURL, engineID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

// GetNodeByID 获取单个节点详情。
func (c *MetaClient) GetNodeByID(nodeID uint) (*models.MetaNode, error) {
	url := fmt.Sprintf("%s/api/v1/meta/nodes/%d", c.baseURL, nodeID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

// GetNodeAncestors 获取 root 到目标 node 的祖先链，包含目标 node 自身。
func (c *MetaClient) GetNodeAncestors(nodeID uint) ([]models.MetaNode, error) {
	url := fmt.Sprintf("%s/api/v1/meta/nodes/%d/ancestors", c.baseURL, nodeID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

// GetItemByID 获取单个数据项详情。
func (c *MetaClient) GetItemByID(itemID uint) (*models.MetaItem, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/items/%d", c.baseURL, itemID)

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

// GetItemAncestors 获取 item 及 root 到 item 父节点的祖先链。
func (c *MetaClient) GetItemAncestors(itemID uint) (*models.MetaItemAncestors, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/items/%d/ancestors", c.baseURL, itemID)

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("meta api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result models.MetaItemAncestors
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
	triggerType, err := normalizeManualMetaTriggerType(opts.TriggerType)
	if err != nil {
		return nil, err
	}
	scanReq["trigger_type"] = triggerType
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

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

func (c *MetaClient) CreateManualScanRunForTenant(tenantID uint, opts MetaScanOptions) (*commonExecution.TaskExecution, error) {
	return c.WithTenantID(tenantID).CreateManualScanRun(opts)
}

func (c *MetaClient) InspectAttributes(req MetaInspectRequest) (*MetaInspectResult, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/inspect", c.baseURL)
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inspect request: %w", err)
	}
	httpReq, err := http.NewRequest("POST", urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create inspect request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send inspect request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("meta inspect api returned status %d: %s", resp.StatusCode, string(respBody))
	}
	var result MetaInspectResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode inspect response: %w", err)
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
	triggerType, err := normalizeManualMetaTriggerType(opts.TriggerType)
	if err != nil {
		return nil, err
	}
	reqPayload["trigger_type"] = triggerType
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
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

// ListEngineItems 获取引擎的已扫描数据项列表，支持按 catalog 第一层业务分支过滤。
func (c *MetaClient) ListEngineItems(engineID uint, branch string) ([]models.MetaItem, error) {
	urlStr := fmt.Sprintf("%s/api/v1/meta/engines/%d/items", c.baseURL, engineID)
	if branch != "" {
		urlStr += "?branch=" + url.QueryEscape(branch)
	}

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
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
