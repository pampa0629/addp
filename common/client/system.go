package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	commonconfiguration "github.com/addp/common/configuration"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	commonutils "github.com/addp/common/utils"
)

// SystemClient 系统服务客户端
type SystemClient struct {
	baseURL     string
	httpClient  *http.Client
	authToken   string // 用户 Access Token
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
		// 用户调用使用 Access Token
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

func withQuery(endpoint string, values neturl.Values) string {
	if encoded := values.Encode(); encoded != "" {
		return endpoint + "?" + encoded
	}
	return endpoint
}

func valuesFromFilters(filters map[string]string) neturl.Values {
	values := neturl.Values{}
	for key, value := range filters {
		values.Set(key, value)
	}
	return values
}

// GetEngine 获取引擎详情
func (c *SystemClient) GetEngine(engineID uint) (*models.Engine, error) {
	var url string
	// 如果使用内部 API Key，调用内部 API
	if c.internalKey != "" {
		url = fmt.Sprintf("%s/api/v1/internal/engines/%d", c.baseURL, engineID)
	} else {
		url = fmt.Sprintf("%s/api/v1/system/engines/%d", c.baseURL, engineID)
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

	var engine models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engine); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &engine, nil
}

// ListEngines 获取资源列表
func (c *SystemClient) ListEngines(engineType string, tenantID uint) ([]models.Engine, error) {
	var url string
	// 如果使用内部 API Key，调用内部 API
	if c.internalKey != "" {
		url = fmt.Sprintf("%s/api/v1/internal/engines", c.baseURL)
	} else {
		url = fmt.Sprintf("%s/api/v1/system/engines", c.baseURL)
	}

	values := neturl.Values{}
	if engineType != "" {
		values.Set("engine_type", engineType)
	}
	if tenantID > 0 {
		values.Set("tenant_id", fmt.Sprintf("%d", tenantID))
	}
	url = withQuery(url, values)

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

	var engines []models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode engines response: %w", err)
	}
	return engines, nil
}

// CreateEngine 创建资源（支持内部或用户接口）
func (c *SystemClient) CreateEngine(payload map[string]interface{}) (*models.Engine, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	var url string
	if c.internalKey != "" {
		url = fmt.Sprintf("%s/api/v1/internal/engines", c.baseURL)
	} else {
		url = fmt.Sprintf("%s/api/v1/system/engines", c.baseURL)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
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

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engine models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engine); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &engine, nil
}

// RegisterCapability 注册能力（服务间调用，无需 JWT）
func (c *SystemClient) RegisterCapability(req *models.CapabilityRegistrationRequest) error {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/internal/registry/capabilities", c.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ListCapabilities 查询能力列表（支持过滤）
func (c *SystemClient) ListCapabilities(filters map[string]string) ([]*models.Engine, error) {
	url := fmt.Sprintf("%s/api/v1/internal/registry/capabilities", c.baseURL)
	url = withQuery(url, valuesFromFilters(filters))

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engines []*models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return engines, nil
}

// ListComputeEngines 查询所有具有计算能力的引擎
func (c *SystemClient) ListComputeEngines(filters map[string]string) ([]*models.Engine, error) {
	url := fmt.Sprintf("%s/api/v1/internal/registry/compute-engines", c.baseURL)
	url = withQuery(url, valuesFromFilters(filters))

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engines []*models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return engines, nil
}

// ================ 能力过滤相关方法（新增） ================

// ListEnginesByCapability 通用方法：按能力过滤资源
func (c *SystemClient) ListEnginesByCapability(tenantID uint, storageTypes []string) ([]models.Engine, error) {
	// 只能使用内部 API
	if c.internalKey == "" {
		return nil, fmt.Errorf("capability filtering requires internal API key")
	}

	url := fmt.Sprintf("%s/api/v1/internal/engines", c.baseURL)

	values := neturl.Values{}
	if tenantID > 0 {
		values.Set("tenant_id", fmt.Sprintf("%d", tenantID))
	}
	if len(storageTypes) > 0 {
		values.Set("storage_type", strings.Join(storageTypes, ","))
	}
	url = withQuery(url, values)

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

	var engines []models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return engines, nil
}

// ListRelationalDatabases 快捷方法：获取所有关系型数据库
func (c *SystemClient) ListRelationalDatabases(tenantID uint) ([]models.Engine, error) {
	return c.ListEnginesByCapability(tenantID, []string{"tabular"})
}

// ListObjectStorages 快捷方法：获取所有对象存储
func (c *SystemClient) ListObjectStorages(tenantID uint) ([]models.Engine, error) {
	return c.ListEnginesByCapability(tenantID, []string{"object"})
}

// ListScannableResources 快捷方法：获取所有可扫描的资源（元数据模块使用）
func (c *SystemClient) ListScannableResources(tenantID uint) ([]models.Engine, error) {
	return c.ListEnginesByCapability(tenantID, []string{"tabular", "dynamic_schema", "graph", "object", "file"})
}

// ListSQLQueryEngines 快捷方法：获取所有支持查询开发的引擎（开发模块使用）
// 包含表格型、动态 schema 型、图数据库
func (c *SystemClient) ListSQLQueryEngines(tenantID uint) ([]models.Engine, error) {
	// 获取所有支持查询的存储引擎类型
	allEngines, err := c.ListEnginesByCapability(tenantID, []string{"tabular", "dynamic_schema", "graph"})
	if err != nil {
		return nil, err
	}

	// 过滤出支持 query 计算入口的引擎
	queryEngines := commonutils.FilterEnginesByComputeEntrypoint(allEngines, "query")

	return queryEngines, nil
}

// ================ TaskProvider 相关方法（新增） ================

// ListTaskProviders 查询所有启用的任务提供者
func (c *SystemClient) ListTaskProviders() ([]*models.TaskProvider, error) {
	url := fmt.Sprintf("%s/api/v1/internal/task-providers", c.baseURL)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var providers []*models.TaskProvider
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return providers, nil
}

// GetTaskProvider 根据 module_name 查询任务提供者
func (c *SystemClient) GetTaskProvider(moduleName string) (*models.TaskProvider, error) {
	url := fmt.Sprintf("%s/api/v1/internal/task-providers/%s", c.baseURL, moduleName)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var provider models.TaskProvider
	if err := json.NewDecoder(resp.Body).Decode(&provider); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &provider, nil
}

// ================ Catalog 相关方法 ================

// EngineCatalogListChildrenRequest 表示实时 catalog 子节点浏览请求。
type EngineCatalogListChildrenRequest struct {
	Path    EngineCatalogPath        `json:"path"`
	Options EngineCatalogListOptions `json:"options,omitempty"`
}

// EngineCatalogListChildrenResponse 表示实时 catalog 子节点浏览响应。
type EngineCatalogListChildrenResponse struct {
	Nodes []EngineCatalogEntry `json:"nodes"`
}

// EngineCatalogPath 表示跨引擎结构化 catalog 路径。
type EngineCatalogPath struct {
	Version  string                 `json:"version,omitempty"`
	EngineID uint                   `json:"engine_id,omitempty"`
	Segments []EngineCatalogSegment `json:"segments"`
}

// EngineCatalogSegment 表示 catalog 路径中的一层。
type EngineCatalogSegment struct {
	Term string `json:"term"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// EngineCatalogEntry 表示实时 catalog 浏览返回的中性条目。
type EngineCatalogEntry struct {
	Name      string                      `json:"name"`
	Path      EngineCatalogPath           `json:"path"`
	Term      string                      `json:"term"`
	Kind      string                      `json:"kind"`
	Role      string                      `json:"role"`
	Table     *datatype.TableInfo         `json:"table,omitempty"`
	Storage   *plugin.CatalogStorageFacts `json:"storage,omitempty"`
	LeafCount *int                        `json:"leaf_count,omitempty"`
	UpdatedAt *time.Time                  `json:"updated_at,omitempty"`
}

// EngineCatalogListOptions 表示实时 catalog 列表选项。
type EngineCatalogListOptions struct {
	Recursive bool `json:"recursive,omitempty"`
	Limit     int  `json:"limit,omitempty"`
	Offset    int  `json:"offset,omitempty"`
}

// ListCatalogChildren 列出指定引擎的实时 catalog 子节点。
func (c *SystemClient) ListCatalogChildren(engineID uint, req EngineCatalogListChildrenRequest) ([]EngineCatalogEntry, error) {
	return c.ListCatalogChildrenWithToken(engineID, req, "")
}

// ListCatalogChildrenWithToken 使用指定用户 JWT 列出实时 catalog 子节点。
// token 为空时使用客户端自身认证配置。
func (c *SystemClient) ListCatalogChildrenWithToken(engineID uint, req EngineCatalogListChildrenRequest, token string) ([]EngineCatalogEntry, error) {
	endpoint := fmt.Sprintf("%s/api/v1/system/engines/%d/catalog/children", c.baseURL, engineID)
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	} else {
		c.addAuth(httpReq)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result EngineCatalogListChildrenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Nodes, nil
}

// ListSparkRuntimes 获取所有 Apache Spark 通用引擎资源
func (c *SystemClient) ListSparkRuntimes(tenantID uint) ([]models.Engine, error) {
	values := neturl.Values{}
	values.Set("engine_type", "spark")
	if tenantID > 0 {
		values.Set("tenant_id", fmt.Sprintf("%d", tenantID))
	}
	endpoint := withQuery(fmt.Sprintf("%s/api/v1/internal/engines", c.baseURL), values)

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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("system api returned status %d: %s", resp.StatusCode, string(body))
	}

	var engines []models.Engine
	if err := json.NewDecoder(resp.Body).Decode(&engines); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 过滤出活跃的 Spark 通用引擎资源
	filtered := make([]models.Engine, 0)
	for _, r := range engines {
		if r.LifecycleState == models.EngineLifecycleActive {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// GetEngineByID 根据 ID 获取资源详情（简化别名）
func (c *SystemClient) GetEngineByID(engineID uint) (*models.Engine, error) {
	return c.GetEngine(engineID)
}

// DoRequest 发送通用HTTP请求（支持GET, POST, PUT, DELETE等）
// 用于调用各种System内部API
func (c *SystemClient) DoRequest(method, url string, payload interface{}, result interface{}) error {
	var bodyReader io.Reader
	if payload != nil {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, url, bodyReader)
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request returned status %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// CreateAuditLog 创建审计日志（跨模块调用）
func (c *SystemClient) CreateAuditLog(log *models.AuditLogCreateRequest) error {
	url := fmt.Sprintf("%s/api/v1/internal/audit-logs", c.baseURL)

	bodyBytes, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("failed to marshal audit log: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create audit log failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ==================== 模块注册与发现 API ====================

// ModuleRegistrationRequest 模块注册请求
type ModuleRegistrationRequest struct {
	ModuleName              string                                     `json:"module_name"`
	ModuleURL               string                                     `json:"module_url"`
	RoutePrefix             string                                     `json:"route_prefix"`
	HealthCheckURL          string                                     `json:"health_check_url,omitempty"`
	Metadata                map[string]interface{}                     `json:"metadata,omitempty"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management,omitempty"`
}

// ModuleInfo 模块信息
type ModuleInfo struct {
	ID                      uint                                       `json:"id"`
	ModuleName              string                                     `json:"module_name"`
	ModuleURL               string                                     `json:"module_url"`
	RoutePrefix             string                                     `json:"route_prefix"`
	HealthCheckURL          string                                     `json:"health_check_url"`
	Status                  string                                     `json:"status"`
	LastHeartbeat           time.Time                                  `json:"last_heartbeat"`
	Metadata                map[string]interface{}                     `json:"metadata"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management,omitempty"`
	CreatedAt               time.Time                                  `json:"created_at"`
	UpdatedAt               time.Time                                  `json:"updated_at"`
}

// RegisterModule 注册模块
func (c *SystemClient) RegisterModule(req *ModuleRegistrationRequest) error {
	url := fmt.Sprintf("%s/api/v1/internal/modules/register", c.baseURL)

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register module failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// SendHeartbeat 发送心跳
func (c *SystemClient) SendHeartbeat(moduleName string) error {
	url := fmt.Sprintf("%s/api/v1/internal/modules/heartbeat", c.baseURL)

	reqBody := map[string]string{"module_name": moduleName}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heartbeat failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// RegisterAndHeartbeat 启动后台 goroutine 完成模块注册+心跳+失败自动重连。
// 初始注册最多重试 3 次；心跳每 10s 一次，连续失败 3 次自动重新注册。
func (c *SystemClient) RegisterAndHeartbeat(moduleName, moduleURL, routePrefix string) {
	c.RegisterAndHeartbeatWithMetadata(moduleName, moduleURL, routePrefix, map[string]interface{}{"module": moduleName})
}

// RegisterAndHeartbeatWithMetadata 启动后台 goroutine 完成模块注册、能力元数据上报、心跳和失败自动重连。
func (c *SystemClient) RegisterAndHeartbeatWithMetadata(moduleName, moduleURL, routePrefix string, metadata map[string]interface{}) {
	go func() {
		time.Sleep(2 * time.Second)
		registrationMetadata := map[string]interface{}{"module": moduleName}
		for key, value := range metadata {
			registrationMetadata[key] = value
		}

		req := &ModuleRegistrationRequest{
			ModuleName:     moduleName,
			ModuleURL:      moduleURL,
			RoutePrefix:    routePrefix,
			HealthCheckURL: moduleURL + "/health",
			Metadata:       registrationMetadata,
		}

		tryRegister := func() bool {
			if err := c.RegisterModule(req); err != nil {
				log.Printf("⚠️  %s 模块注册失败: %v", moduleName, err)
				return false
			}
			log.Printf("✅ %s 模块注册成功: %s", moduleName, moduleURL)
			return true
		}

		// 初始注册，最多重试 3 次
		registered := false
		for attempt := 1; attempt <= 3; attempt++ {
			if tryRegister() {
				registered = true
				break
			}
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}

		// 心跳循环
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		consecutiveFailures := 0
		for range ticker.C {
			if err := c.SendHeartbeat(moduleName); err != nil {
				consecutiveFailures++
				log.Printf("⚠️  %s 心跳失败: %v", moduleName, err)
			} else {
				if !registered {
					log.Printf("✅ %s 心跳恢复正常", moduleName)
					registered = true
				}
				consecutiveFailures = 0
			}

			if consecutiveFailures >= 3 {
				log.Printf("⚠️  %s 心跳连续失败 %d 次，尝试重新注册...", moduleName, consecutiveFailures)
				if tryRegister() {
					registered = true
					consecutiveFailures = 0
				} else {
					time.Sleep(20 * time.Second)
				}
			}
		}
	}()
}

// GetModules 获取模块列表
func (c *SystemClient) GetModules() ([]*ModuleInfo, error) {
	url := fmt.Sprintf("%s/api/v1/internal/modules", c.baseURL)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get modules failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Modules []*ModuleInfo `json:"modules"`
		Count   int           `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Modules, nil
}

// GetActiveModules 获取活跃模块列表
func (c *SystemClient) GetActiveModules() ([]*ModuleInfo, error) {
	url := fmt.Sprintf("%s/api/v1/internal/modules?status=up", c.baseURL)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get active modules failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Modules []*ModuleInfo `json:"modules"`
		Count   int           `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Modules, nil
}

// GetModule 获取单个模块信息
func (c *SystemClient) GetModule(moduleName string) (*ModuleInfo, error) {
	url := fmt.Sprintf("%s/api/v1/internal/modules/%s", c.baseURL, moduleName)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get module failed with status %d: %s", resp.StatusCode, string(body))
	}

	var module ModuleInfo
	if err := json.NewDecoder(resp.Body).Decode(&module); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &module, nil
}
